// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

	steps     []*stepState            // The pipeline, in order of execution
	targets   map[string]*targetState // Target state lookup map
	targetsWg sync.WaitGroup          // Tracks all the target handlers

	// One mutex to rule them all, used to serialize access to all the state above.
	// Could probably be split into several if necessary.
	mu          sync.Mutex
	monitorCond *sync.Cond // Used to notify the monitor about changes
}

// stepState contains state associated with one state of the pipeline:
type stepState struct {
	ctx    xcontext.Context
	cancel xcontext.CancelFunc

	stepIndex int                 // Index of this step in the pipeline.
	sb        test.TestStepBundle // The test bundle.

	ev                 testevent.Emitter
	stepRunner         *StepRunner
	addTarget          AddTargetToStep
	readingLoopRunning bool

	resumeState        json.RawMessage // Resume state passed to and returned by the Run method.
	resumeStateTargets []target.Target // Targets that were being processed during pause.
	runErr             error           // Runner error, returned from Run() or an error condition detected by the reader.
}

// targetStepPhase denotes progression of a target through a step
type targetStepPhase int

const (
	targetStepPhaseInvalid       targetStepPhase = iota
	targetStepPhaseInit                          // (1) Created
	targetStepPhaseBegin                         // (2) Picked up for execution.
	targetStepPhaseRun                           // (3) Injected into step.
	targetStepPhaseResultPending                 // (4) Result posted to the handler.
	targetStepPhaseEnd                           // (5) Finished running a step.
)

// targetState contains state associated with one target progressing through the pipeline.
type targetState struct {
	tgt *target.Target

	// This part of state gets serialized into JSON for resumption.
	CurStep  int             `json:"S,omitempty"` // Current step number.
	CurPhase targetStepPhase `json:"P,omitempty"` // Current phase of step execution.
	Res      *xjson.Error    `json:"R,omitempty"` // Final result, if reached the end state.

	handlerRunning bool
	resCh          chan error // Channel used to communicate result by the step runner.
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
	stepsCtx, stepsCancel := xcontext.WithCancel(ctx)
	defer stepsCancel()
	targetsCtx, targetsCancel := xcontext.WithCancel(ctx)
	defer targetsCancel()

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
		// Buffer of 1 is needed so that the step reader does not block when submitting result back
		// to the target handler. Target handler may not yet be ready to receive the result,
		// i.e. reporting TargetIn event which may involve network I/O.
		tgs.resCh = make(chan error, 1)
		tr.targets[tgt.ID] = tgs

		if tgs.CurStep < minStep {
			minStep = tgs.CurStep
		}
	}

	// Set up the pipeline
	for i, sb := range t.TestStepsBundles {
		stepCtx, stepCancel := xcontext.WithCancel(stepsCtx)
		stepCtx = stepCtx.WithField("step_index", strconv.Itoa(i))
		stepCtx = stepCtx.WithField("step_label", sb.TestStepLabel)

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

		ss := &stepState{
			ctx:                stepCtx,
			cancel:             stepCancel,
			stepIndex:          i,
			sb:                 sb,
			ev:                 emitterFactory.New(sb.TestStepLabel),
			stepRunner:         NewStepRunner(),
			resumeState:        srs,
			resumeStateTargets: resumeStateTargets,
		}
		// Step handlers will be started from target handlers as targets reach them.
		tr.steps = append(tr.steps, ss)
	}

	for _, tgs := range tr.targets {
		tgs.handlerRunning = true
		tr.targetsWg.Add(1)

		go func(state *targetState) {
			tr.targetHandler(targetsCtx, state)
			tr.targetsWg.Done()
		}(tgs)
	}

	// Run until no more progress can be made.
	runErr := tr.runMonitor(ctx, minStep)
	if runErr != nil {
		ctx.Errorf("monitor returned error: %q, canceling", runErr)
		stepsCancel()
	}

	// Wait for step runners and readers to exit.
	if err := tr.waitStepRunners(ctx); err != nil {
		if runErr == nil {
			runErr = err
		}
	}

	// There will be no more results, reel in all the target handlers (if any).
	ctx.Debugf("cancel target handlers")
	targetsCancel()

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
	resumeOk := (runErr == nil)
	numInFlightTargets := 0
	for i, tgt := range targets {
		tgs := tr.targets[tgt.ID]
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
		ctx.Debugf("  %d %s %v %t", i, tgs, stepErr, resumeOk)
	}
	ctx.Debugf("- %d in flight, ok to resume? %t", numInFlightTargets, resumeOk)
	ctx.Debugf("step states:")
	for i, ss := range tr.steps {
		ctx.Debugf("  %d %s %t %t %v %s",
			i, ss, ss.stepRunner.Started(), ss.readingLoopRunning, ss.runErr, ss.resumeState)
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
			Version: resumeStateStructVersion,
			Targets: tr.targets,
		}
		for _, ss := range tr.steps {
			rs.StepResumeState = append(rs.StepResumeState, ss.resumeState)
		}
		resumeState, runErr = json.Marshal(rs)
		if runErr != nil {
			ctx.Errorf("unable to serialize the state: %s", runErr)
			return nil, nil, runErr
		}
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

func (tr *TestRunner) waitStepRunners(ctx xcontext.Context) error {
	ctx.Debugf("waiting for step runners to finish")

	shutdownCtx, cancel := context.WithTimeout(ctx, tr.shutdownTimeout)
	defer cancel()

	var stepsNeverReturned []string
	for _, ss := range tr.steps {
		if !ss.stepRunner.Started() {
			continue
		}
		result, err := ss.stepRunner.WaitResults(shutdownCtx)
		if err != nil {
			result.Err = &cerrors.ErrTestStepsNeverReturned{StepNames: []string{ss.sb.TestStepLabel}}
			stepsNeverReturned = append(stepsNeverReturned, ss.sb.TestStepLabel)
			// Cancel this step's context, this will help release the reader.
			ss.cancel()
		} else {
			// reading loop can still be running, wait for it to finish
			tr.mu.Lock()
			for {
				if !ss.readingLoopRunning {
					break
				}
				tr.monitorCond.Wait()
			}
			tr.mu.Unlock()
		}
		tr.mu.Lock()
		ss.resumeState = result.ResumeState
		ss.setErrLocked(result.Err)
		tr.mu.Unlock()
	}

	for _, ss := range tr.steps {
		tr.mu.Lock()
		stepErr := ss.runErr
		tr.mu.Unlock()

		if stepErr != nil && stepErr != xcontext.ErrPaused && stepErr != xcontext.ErrCanceled {
			if err := emitEvent(ctx, ss.ev, EventTestError, nil, stepErr.Error()); err != nil {
				ctx.Errorf("failed to emit event: %s", err)
			}
		}
	}

	resultErr := tr.checkStepRunnersFailed()
	if len(stepsNeverReturned) > 0 && resultErr == nil {
		resultErr = &cerrors.ErrTestStepsNeverReturned{StepNames: stepsNeverReturned}
	}
	return resultErr
}

func (tr *TestRunner) injectTarget(ctx xcontext.Context, tgs *targetState, ss *stepState) error {
	ctx.Debugf("%s: injecting into %s", tgs, ss)

	tgt := tgs.tgt
	err := ss.addTarget(ctx, tgt)
	if err == nil {
		tr.mu.Lock()
		// By the time we get here the target could have been processed and result posted already, hence the check.
		if tgs.CurPhase == targetStepPhaseBegin {
			tgs.CurPhase = targetStepPhaseRun
		}
		tr.mu.Unlock()
	}
	tr.monitorCond.Signal()
	return err
}

func (tr *TestRunner) awaitTargetResult(ctx xcontext.Context, tgs *targetState, ss *stepState) error {
	tr.mu.Lock()
	resCh := tgs.resCh
	tr.mu.Unlock()

	if resCh == nil {
		// Channel is closed when job is paused to make sure all results are processed.
		ctx.Debugf("%s: result channel closed", tgs)
		return xcontext.ErrPaused
	}

	processTargetResult := func(res error) {
		ctx.Debugf("%s: result recd for %s", tgs, ss)
		tr.mu.Lock()
		if res != nil {
			tgs.Res = xjson.NewError(res)
		}
		tgs.CurPhase = targetStepPhaseEnd
		tr.mu.Unlock()
		tr.monitorCond.Signal()
	}

	onCancelled := func() error {
		// we might have a race-condition here when both events happen
		// in this case we should prioritise result processing
		select {
		case res := <-resCh:
			processTargetResult(res)
			return nil
		default:
		}
		tr.mu.Lock()
		ctx.Debugf("%s: canceled 2", tgs)
		tr.mu.Unlock()
		return xcontext.ErrCanceled
	}

	select {
	case res, ok := <-resCh:
		if !ok {
			// Channel is closed when job is paused to make sure all results are processed.
			ctx.Debugf("%s: result channel closed", tgs)
			return xcontext.ErrPaused
		}
		processTargetResult(res)
		return nil
	// Check for cancellation.
	// Notably we are not checking for the pause condition here:
	// when paused, we want to let all the injected targets to finish
	// and collect all the results they produce. If that doesn't happen,
	// step runner will close resCh on its way out and unblock us.
	case <-ss.ctx.Done():
		return onCancelled()
	case <-ctx.Done():
		return onCancelled()
	}
}

// targetHandler takes a single target through each step of the pipeline in sequence.
// It injects the target, waits for the result, then moves on to the next step.
func (tr *TestRunner) targetHandler(ctx xcontext.Context, tgs *targetState) {
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
		var inject bool
		switch tgs.CurPhase {
		case targetStepPhaseInit:
			// Normal case, inject and wait for result.
			inject = true
			tgs.CurPhase = targetStepPhaseBegin
		case targetStepPhaseBegin:
			// Paused before injection.
			inject = true
		case targetStepPhaseRun:
			// Resumed in running state, skip injection.
			inject = false
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
		err := tr.runStepIfNeeded(ss)
		// Inject the target.
		if err == nil && inject {
			err = tr.injectTarget(ctx, tgs, ss)
		}
		// Await result. It will be communicated to us by the step runner
		// and returned in tgs.res.
		if err == nil {
			err = tr.awaitTargetResult(ctx, tgs, ss)
		}
		tr.mu.Lock()
		if err != nil {
			ctx.Errorf("%s", err)
			switch err {
			case xcontext.ErrPaused:
				ctx.Debugf("%s: paused 1", tgs)
			case xcontext.ErrCanceled:
				ctx.Debugf("%s: canceled 1", tgs)
			default:
				ss.setErrLocked(err)
			}
			tr.mu.Unlock()
			break
		}
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
	tr.mu.Lock()
	ctx.Debugf("%s: target handler finished", tgs)
	tgs.handlerRunning = false
	tr.monitorCond.Signal()
	tr.mu.Unlock()
}

// runStepIfNeeded starts the step runner goroutine if not already running.
func (tr *TestRunner) runStepIfNeeded(ss *stepState) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	if ss.stepRunner.Started() {
		return nil
	}

	resumeState := ss.resumeState
	ss.resumeState = nil

	resultCh, addTarget, err := ss.stepRunner.Run(ss.ctx, ss.sb, ss.ev, resumeState, ss.resumeStateTargets)
	if err != nil {
		return fmt.Errorf("failed to stert a step runner for '%s': %v", ss.sb.TestStepLabel, err)
	}
	ss.addTarget = addTarget

	ss.readingLoopRunning = true
	go func() {
		defer func() {
			tr.mu.Lock()
			defer tr.mu.Unlock()

			ss.readingLoopRunning = false
			tr.monitorCond.Signal()
		}()
		for {
			select {
			case stepResult, ok := <-resultCh:
				if !ok {
					ss.ctx.Debugf("step runner results channel was closed")
					return
				}
				if stepResult.Target == nil {
					ss.ctx.Infof("step runner results in err: %v", err)
					tr.mu.Lock()
					ss.setErrLocked(stepResult.Err)
					tr.mu.Unlock()
				} else {
					if err := tr.reportTargetResult(ss.ctx, ss, stepResult.Target, stepResult.Err); err != nil {
						ss.ctx.Errorf("Reporting target result failed: %v", err)
						tr.mu.Lock()
						ss.setErrLocked(err)
						tr.mu.Unlock()
					}
				}
				tr.monitorCond.Signal()
			case <-ss.ctx.Done():
				ss.ctx.Debugf("step context was cancelled")
				return
			}
		}
	}()
	return nil
}

// setErrLocked sets step runner error unless already set.
func (ss *stepState) setErrLocked(err error) {
	if err == nil || ss.runErr != nil {
		return
	}
	ss.ctx.Errorf("setErrLocked: %v", err)
	ss.runErr = err
}

// reportTargetResult reports result of executing a step to the appropriate target handler.
func (tr *TestRunner) reportTargetResult(ctx xcontext.Context, ss *stepState, tgt *target.Target, res error) error {
	resCh, err := func() (chan error, error) {
		tr.mu.Lock()
		defer tr.mu.Unlock()

		tgs := tr.targets[tgt.ID]
		if tgs.resCh == nil {
			// If canceled or paused, target handler may have left early. We don't care though.
			select {
			case <-ctx.Done():
				return nil, xcontext.ErrCanceled
			default:
				// This should not happen, must be an internal error.
				return nil, fmt.Errorf("%s: target handler %s is not there, dropping result on the floor", ss, tgs)
			}
		}
		tgs.CurPhase = targetStepPhaseResultPending
		ctx.Debugf("%s: result for %s: %v", ss, tgs, res)
		return tgs.resCh, nil
	}()
	if err != nil {
		return err
	}

	select {
	case resCh <- res:
	case <-ctx.Done():
		return ctx.Err()
	case <-ss.ctx.Done():
		return ss.ctx.Err()
	}
	return nil
}

// checkStepRunnersFailed checks if any step runner has encountered an error.
func (tr *TestRunner) checkStepRunnersFailed() error {
	for i, ss := range tr.steps {
		switch ss.runErr {
		case nil:
			// Nothing
		case xcontext.ErrPaused:
			// This is fine, just need to unblock target handlers waiting on result from this step.
			for _, tgs := range tr.targets {
				if tgs.resCh != nil && tgs.CurStep == i {
					close(tgs.resCh)
					tgs.resCh = nil
				}
			}
		default:
			return ss.runErr
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
			ss.stepRunner.Stop()
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
				if tgs.CurPhase == targetStepPhaseRun && tr.steps[tgs.CurStep].runErr == xcontext.ErrPaused {
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
	case targetStepPhaseResultPending:
		return "result_pending"
	case targetStepPhaseEnd:
		return "end"
	}
	return fmt.Sprintf("???(%d)", tph)
}

func (ss *stepState) String() string {
	return fmt.Sprintf("[#%d %s]", ss.stepIndex, ss.sb.TestStepLabel)
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
