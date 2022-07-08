// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/insomniacslk/xjson"

	"github.com/linuxboot/contest/pkg/cerrors"
	"github.com/linuxboot/contest/pkg/config"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// TestRunner is the state associated with a test run.
// Here's how a test run works:
//  * Each target gets a targetState and a "target handler" - a goroutine that takes that particular
//    target through each step of the pipeline in sequence. It injects the target, waits for the result,
//    then moves on to the next step.
//  * Each step of the pipeline gets a stepState and:
//    - A "step runner" - a goroutine that is responsible for running the step's Run() method
//    - A "step reader" - a goroutine that processes results and sends them on to target handlers that await them.
//  * After starting all of the above, the main goroutine goes into "monitor" mode
//    that checks on the pipeline's progress and is responsible for closing step input channels
//    when all the targets have been injected.
//  * Monitor loop finishes when all the targets have been injected into the last step
//    or if a step has encountered an error.
//  * We then wait for all the step runners and readers to shut down.
//  * Once all the activity has died down, resulting state is examined and an error is returned, if any.
type TestRunner struct {
	shutdownTimeout time.Duration // Time to wait for steps runners to finish a the end of the run

	steps   []*stepState            // The pipeline, in order of execution
	targets map[string]*targetState // Target state lookup map

	stepOutputs *testStepsVariables // contains emitted steps variables

	// One mutex to rule them all, used to serialize access to all the state above.
	// Could probably be split into several if necessary.
	mu          sync.Mutex
	monitorCond *sync.Cond // Used to notify the monitor about changes
}

// targetStepPhase denotes progression of a target through a step
type targetStepPhase int

const (
	targetStepPhaseInvalid  targetStepPhase = iota
	targetStepPhaseInit                     // (1) Created
	targetStepPhaseBegin                    // (2) Picked up for execution.
	targetStepPhaseRun                      // (3) Injected into step.
	targetStepPhaseObsolete                 // (4) Former result posted to the handler. [Obsolete]
	targetStepPhaseEnd                      // (5) Finished running a step.
)

// stepVariables represents the emitted variables of the steps
type stepVariables map[string]json.RawMessage

// targetState contains state associated with one target progressing through the pipeline.
type targetState struct {
	tgt *target.Target

	// This part of state gets serialized into JSON for resumption.
	CurStep        int                      `json:"S,omitempty"` // Current step number.
	CurPhase       targetStepPhase          `json:"P,omitempty"` // Current phase of step execution.
	Res            *xjson.Error             `json:"R,omitempty"` // Final result, if reached the end state.
	StepsVariables map[string]stepVariables `json:"V,omitempty"` // maps steps onto emitted variables of each

	handlerRunning bool
}

// resumeStateStruct is used to serialize runner state to be resumed in the future.
type resumeStateStruct struct {
	Version         int                     `json:"V"`
	Targets         map[string]*targetState `json:"T"`
	StepResumeState []json.RawMessage       `json:"SRS,omitempty"`
}

// Resume state version we are compatible with.
// When imcompatible changes are made to the state format, bump this.
// Restoring incompatible state will abort the job.
const resumeStateStructVersion = 2

type TestStepEventsEmitterFactory interface {
	New(testStepLabel string) testevent.Emitter
}

// Run is the main enty point of the runner.
func (tr *TestRunner) Run(
	ctx xcontext.Context,
	t *test.Test, targets []*target.Target,
	emitterFactory TestStepEventsEmitterFactory,
	resumeState json.RawMessage,
) (json.RawMessage, map[string]error, error) {

	// Peel off contexts used for steps and target handlers.
	runCtx, runCancel := xcontext.WithCancel(ctx)
	defer runCancel()

	// If we have state to resume, parse it.
	var rs resumeStateStruct
	if len(resumeState) > 0 {
		ctx.Debugf("Attempting to resume from state: %s", string(resumeState))
		if err := json.Unmarshal(resumeState, &rs); err != nil {
			return nil, nil, fmt.Errorf("invalid resume state: %w", err)
		}
		if rs.Version != resumeStateStructVersion {
			return nil, nil, fmt.Errorf("incompatible resume state version %d (want %d)",
				rs.Version, resumeStateStructVersion)
		}
		tr.targets = rs.Targets
	}

	// Set up the targets
	if tr.targets == nil {
		tr.targets = make(map[string]*targetState)
	}

	// Initialize remaining fields of the target structures,
	// build the map and kick off target processing.
	minStep := len(tr.steps)
	for _, tgt := range targets {
		tgs := tr.targets[tgt.ID]
		if tgs == nil {
			tgs = &targetState{
				CurPhase: targetStepPhaseInit,
			}
		}
		tgs.tgt = tgt
		tr.targets[tgt.ID] = tgs

		if tgs.CurStep < minStep {
			minStep = tgs.CurStep
		}
	}

	var err error
	tr.stepOutputs, err = newTestStepsVariables(t.TestStepsBundles)
	if err != nil {
		ctx.Errorf("Failed to initialise test steps variables: %v", err)
		return nil, nil, err
	}

	for targetID, targetState := range tr.targets {
		if err := tr.stepOutputs.initTargetStepsVariables(targetID, targetState.StepsVariables); err != nil {
			ctx.Errorf("Failed to initialise test steps variables for target: %s: %v", targetID, err)
			return nil, nil, err
		}
	}

	// Set up the pipeline
	for i, sb := range t.TestStepsBundles {
		var srs json.RawMessage
		if i < len(rs.StepResumeState) && string(rs.StepResumeState[i]) != "null" {
			srs = rs.StepResumeState[i]
		}

		// Collect "processed" targets in resume state for a StepRunner
		var resumeStateTargets []target.Target
		for _, tgt := range tr.targets {
			if tgt.CurStep == i && tgt.CurPhase == targetStepPhaseRun {
				resumeStateTargets = append(resumeStateTargets, *tgt.tgt)
			}
		}

		// Step handlers will be started from target handlers as targets reach them.
		tr.steps = append(tr.steps, newStepState(i, sb, emitterFactory, srs, resumeStateTargets, func(err error) {
			tr.monitorCond.Signal()
		}))
	}

	for _, tgs := range tr.targets {
		tgs.handlerRunning = true

		go func(state *targetState) {
			tr.targetHandler(runCtx, state)
		}(tgs)
	}

	// Run until no more progress can be made.
	runErr := tr.runMonitor(ctx, minStep)
	if runErr != nil {
		ctx.Errorf("monitor returned error: %q, canceling", runErr)
		for _, ss := range tr.steps {
			ss.ForceStop()
		}
	}

	// Wait for step runners and readers to exit.
	stepResumeStates, err := tr.waitSteps(ctx)
	if err != nil && runErr == nil {
		runErr = err
	}

	// There will be no more results cancel everything
	ctx.Debugf("cancel target handlers")
	runCancel()

	// Has the run been canceled? If so, ignore whatever happened, it doesn't matter.
	select {
	case <-ctx.Done():
		runErr = xcontext.ErrCanceled
	default:
	}

	// Examine the resulting state.
	ctx.Debugf("leaving, err %v, target states:", runErr)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	resumeOk := runErr == nil
	numInFlightTargets := 0
	for i, tgt := range targets {
		tgs := tr.targets[tgt.ID]
		tgs.StepsVariables, err = tr.stepOutputs.getTargetStepsVariables(tgt.ID)
		if err != nil {
			ctx.Errorf("Failed to get steps variables: %v", err)
			return nil, nil, err
		}
		stepErr := tr.steps[tgs.CurStep].runErr
		if tgs.CurPhase == targetStepPhaseRun {
			numInFlightTargets++
			if stepErr != xcontext.ErrPaused {
				resumeOk = false
			}
		}
		if stepErr != nil && stepErr != xcontext.ErrPaused {
			resumeOk = false
		}
		ctx.Debugf("  %d target: '%s' step err: '%v', resume ok: '%t'", i, tgs, stepErr, resumeOk)
	}
	ctx.Debugf("- %d in flight, ok to resume? %t", numInFlightTargets, resumeOk)
	ctx.Debugf("step states:")
	for i, ss := range tr.steps {
		ctx.Debugf("  %d %s %t %t %s", i, ss, ss.stepRunner.Started(), ss.GetError(), stepResumeStates[i])
	}

	// Is there a useful error to report?
	if runErr != nil {
		return nil, nil, runErr
	}

	// Have we been asked to pause? If yes, is it safe to do so?
	select {
	case <-ctx.Until(xcontext.ErrPaused):
		if !resumeOk {
			ctx.Warnf("paused but not ok to resume")
			break
		}
		rs := &resumeStateStruct{
			Version:         resumeStateStructVersion,
			Targets:         tr.targets,
			StepResumeState: stepResumeStates,
		}
		resumeState, runErr = json.Marshal(rs)
		if runErr != nil {
			ctx.Errorf("unable to serialize the state: %s", runErr)
			return nil, nil, runErr
		}
		ctx.Debugf("resume state: %s", resumeState)
		runErr = xcontext.ErrPaused
	default:
	}

	targetsResults := make(map[string]error)
	for id, state := range tr.targets {
		if state.Res != nil {
			targetsResults[id] = state.Res.Unwrap()
		} else if state.CurStep == len(tr.steps)-1 && state.CurPhase == targetStepPhaseEnd {
			targetsResults[id] = nil
		}
	}
	return resumeState, targetsResults, runErr
}

func (tr *TestRunner) waitSteps(ctx xcontext.Context) ([]json.RawMessage, error) {
	ctx.Debugf("waiting for step runners to finish")

	shutdownCtx, cancel := context.WithTimeout(ctx, tr.shutdownTimeout)
	defer cancel()

	var stepsNeverReturned []string
	var resumeStates []json.RawMessage
	var resultErr error
	for _, ss := range tr.steps {
		if !ss.Started() {
			resumeStates = append(resumeStates, ss.GetInitResumeState())
			continue
		}
		result, err := ss.WaitResults(shutdownCtx)
		if err != nil {
			stepsNeverReturned = append(stepsNeverReturned, ss.GetTestStepLabel())
			ss.SetError(ctx, &cerrors.ErrTestStepsNeverReturned{StepNames: []string{ss.GetTestStepLabel()}})
			// Stop step context, this will help release the reader.
			ss.ForceStop()
		} else if resultErr == nil && result.Err != nil && result.Err != xcontext.ErrPaused {
			resultErr = result.Err
		}
		resumeStates = append(resumeStates, result.ResumeState)
	}

	if len(stepsNeverReturned) > 0 && resultErr == nil {
		resultErr = &cerrors.ErrTestStepsNeverReturned{StepNames: stepsNeverReturned}
	}
	return resumeStates, resultErr
}

// targetHandler takes a single target through each step of the pipeline in sequence.
// It injects the target, waits for the result, then moves on to the next step.
func (tr *TestRunner) targetHandler(ctx xcontext.Context, tgs *targetState) {
	defer func() {
		tr.mu.Lock()
		tgs.handlerRunning = false
		tr.mu.Unlock()
		tr.monitorCond.Signal()
		ctx.Debugf("%s: target handler finished", tgs)
	}()

	ctx = ctx.WithField("target", tgs.tgt.ID)
	ctx.Debugf("%s: target handler active", tgs)
	// NB: CurStep may be non-zero on entry if resumed
loop:
	for i := tgs.CurStep; i < len(tr.steps); {
		// Early check for pause or cancellation.
		select {
		case <-ctx.Until(xcontext.ErrPaused):
			ctx.Debugf("%s: paused 0", tgs)
			break loop
		case <-ctx.Done():
			ctx.Debugf("%s: canceled 0", tgs)
			break loop
		default:
		}
		tr.mu.Lock()
		ss := tr.steps[i]
		switch tgs.CurPhase {
		case targetStepPhaseInit:
			// Normal case, inject and wait for result.
			tgs.CurPhase = targetStepPhaseBegin
		case targetStepPhaseBegin:
			// Paused before injection.
		case targetStepPhaseRun:
			// Resumed in running state, skip injection.
		case targetStepPhaseEnd:
			// Resumed in terminal state, we are done.
			tr.mu.Unlock()
			break loop
		default:
			ctx.Errorf("%s: invalid phase %s", tgs, tgs.CurPhase)
			break loop
		}
		tr.mu.Unlock()
		// Make sure we have a step runner active. If not, start one.
		err := ss.Run(ctx)

		var targetNotifier ChanNotifier
		if err == nil {
			// Inject the target.
			ctx.Debugf("%s: injecting into %s", tgs, ss)
			targetNotifier, err = ss.InjectTarget(ctx, tgs.tgt)
		}
		if err == nil {
			tr.mu.Lock()
			// By the time we get here the target could have been processed and result posted already, hence the check.
			if tgs.CurPhase == targetStepPhaseBegin {
				tgs.CurPhase = targetStepPhaseRun
			}
			tr.mu.Unlock()
			tr.monitorCond.Signal()
		}
		// Await result. It will be communicated to us by the step runner
		// and returned in tgs.res.
		if err == nil {
			select {
			case res := <-targetNotifier.NotifyCh():
				ctx.Debugf("Got target result: '%v'", err)
				tr.mu.Lock()
				if res != nil {
					tgs.Res = xjson.NewError(res)
				}
				tgs.CurPhase = targetStepPhaseEnd
				tr.mu.Unlock()
				tr.monitorCond.Signal()
				err = nil
			case <-ss.NotifyStopped():
				err = ss.GetError()
				ctx.Debugf("step runner stopped: '%v'", err)
			case <-ctx.Done():
				ctx.Debugf("Canceled target context during waiting for target result")
				err = ctx.Err()
			}
		}
		if err != nil {
			ctx.Errorf("Target handler failed: %v", err)
			switch err {
			case xcontext.ErrPaused:
				ctx.Debugf("%s: paused 1", tgs)
			case xcontext.ErrCanceled:
				ctx.Debugf("%s: canceled 1", tgs)
			default:
				// TODO: this is a logical error. The step might not have failed.
				// targetHandler should return an error instead that should be tracked in runMonitor
				ss.SetError(ctx, err)
			}
			break
		}

		tr.mu.Lock()
		if tgs.Res != nil {
			tr.mu.Unlock()
			break
		}
		i++
		if i < len(tr.steps) {
			tgs.CurStep = i
			tgs.CurPhase = targetStepPhaseInit
		}
		tr.mu.Unlock()
	}
}

// checkStepRunnersFailed checks if any step runner has encountered an error.
func (tr *TestRunner) checkStepRunnersFailed() error {
	for _, ss := range tr.steps {
		runErr := ss.GetError()
		switch runErr {
		case nil, xcontext.ErrPaused:
		default:
			return runErr
		}
	}
	return nil
}

// runMonitor monitors progress of targets through the pipeline
// and closes input channels of the steps to indicate that no more are expected.
// It also monitors steps for critical errors and cancels the whole run.
// Note: input channels remain open when cancellation is requested,
// plugins are expected to handle it explicitly.
func (tr *TestRunner) runMonitor(ctx xcontext.Context, minStep int) error {
	ctx.Debugf("monitor: active")
	if minStep < len(tr.steps) {
		ctx.Debugf("monitor: starting at step %s", tr.steps[minStep])
	}

	// Run the main loop.
	runErr := func() error {
		tr.mu.Lock()
		defer tr.mu.Unlock()

		pass := 1
		var runErr error
	stepLoop:
		for step := minStep; step < len(tr.steps); pass++ {
			ss := tr.steps[step]
			ctx.Debugf("monitor pass %d: current step %s", pass, ss)
			// Check if all the targets have either made it past the injection phase or terminated.
			ok := true
			for _, tgs := range tr.targets {
				ctx.Debugf("monitor pass %d: %s: %s", pass, ss, tgs)
				if !tgs.handlerRunning { // Not running anymore
					continue
				}
				if tgs.CurStep < step || tgs.CurPhase < targetStepPhaseRun {
					ctx.Debugf("monitor pass %d: %s: not all targets injected yet (%s)", pass, ss, tgs)
					ok = false
					break
				}
			}
			if runErr = tr.checkStepRunnersFailed(); runErr != nil {
				break stepLoop
			}
			if !ok {
				// Wait for notification: as progress is being made, we get notified.
				tr.monitorCond.Wait()
				continue
			}
			// All targets ok, close the step's input channel.
			ctx.Debugf("monitor pass %d: %s: no more targets, closing input channel", pass, ss)
			ss.Stop()
			step++
		}
		return runErr
	}()
	if runErr != nil {
		return runErr
	}

	//
	// After all targets were sent to the steps we should monitor steps for incoming errors
	//
	tr.mu.Lock()
	defer tr.mu.Unlock()

	var pass int
tgtLoop:
	for ; runErr == nil; pass++ {
		if runErr = tr.checkStepRunnersFailed(); runErr != nil {
			break tgtLoop
		}
		done := true
		for _, tgs := range tr.targets {
			ctx.Debugf("monitor pass %d: %s", pass, tgs)
			if tgs.handlerRunning && (tgs.CurStep < len(tr.steps) || tgs.CurPhase != targetStepPhaseEnd) {
				if tgs.CurPhase == targetStepPhaseRun && tr.steps[tgs.CurStep].GetError() == xcontext.ErrPaused {
					// It's been paused, this is fine.
					continue
				}
				done = false
				break
			}
		}
		if done {
			break
		}
		// Wait for notification: as progress is being made, we get notified.
		tr.monitorCond.Wait()
	}
	ctx.Debugf("monitor: finished, %v", runErr)
	return runErr
}

func NewTestRunnerWithTimeouts(shutdownTimeout time.Duration) *TestRunner {
	tr := &TestRunner{
		shutdownTimeout: shutdownTimeout,
	}
	tr.monitorCond = sync.NewCond(&tr.mu)
	return tr
}

func NewTestRunner() *TestRunner {
	return NewTestRunnerWithTimeouts(config.TestRunnerShutdownTimeout)
}

func (tph targetStepPhase) String() string {
	switch tph {
	case targetStepPhaseInvalid:
		return "INVALID"
	case targetStepPhaseInit:
		return "init"
	case targetStepPhaseBegin:
		return "begin"
	case targetStepPhaseRun:
		return "run"
	case targetStepPhaseObsolete:
		return "result_pending_obsolete"
	case targetStepPhaseEnd:
		return "end"
	}
	return fmt.Sprintf("???(%d)", tph)
}

func (tgs *targetState) String() string {
	var resText string
	if tgs.Res != nil {
		resStr := fmt.Sprintf("%v", tgs.Res)
		if len(resStr) > 20 {
			resStr = resStr[:20] + "..."
		}
		resText = fmt.Sprintf("%q", resStr)
	} else {
		resText = "<nil>"
	}
	finished := !tgs.handlerRunning
	return fmt.Sprintf("[%s %d %s %t %s]",
		tgs.tgt, tgs.CurStep, tgs.CurPhase, finished, resText)
}
