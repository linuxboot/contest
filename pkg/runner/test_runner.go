// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package runner

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/insomniacslk/xjson"

	"github.com/linuxboot/contest/pkg/cerrors"
	"github.com/linuxboot/contest/pkg/config"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"
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

	steps     []StepState             // The pipeline, in order of execution
	targets   map[string]*targetState // Target state lookup map
	targetsWg sync.WaitGroup          // Tracks all the target handlers

	// One mutex to rule them all, used to serialize access to all the state above.
	// Could probably be split into several if necessary.
	mu   sync.Mutex
	cond *sync.Cond // Used to notify the monitor about changes
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
	targetStepPhaseSleepRetry                    // (6) A step is sleeping waiting to be retried
)

// targetState contains state associated with one target progressing through the pipeline.
type targetState struct {
	tgt *target.Target

	// This part of state gets serialized into JSON for resumption.
	CurStep     int             `json:"S,omitempty"`  // Current step number.
	CurPhase    targetStepPhase `json:"P,omitempty"`  // Current phase of step execution.
	Res         *xjson.Error    `json:"R,omitempty"`  // Result of the last attempt or final, if reached the end state.
	CurRetry    int             `json:"CR,omitempty"` // Current retry number.
	NextAttempt *time.Time      `json:"NA,omitempty"` // Timestap for the next attempt to begin.

	handlerRunning bool
	resCh          chan error // Channel used to communicate result by the step runner.
}

// resumeStateStruct is used to serialize runner state to be resumed in the future.
type resumeStateStruct struct {
	Version         int                     `json:"V"`
	JobID           types.JobID             `json:"J"`
	RunID           types.RunID             `json:"R"`
	Targets         map[string]*targetState `json:"T"`
	StepResumeState []json.RawMessage       `json:"SRS,omitempty"`
}

// Resume state version we are compatible with.
// When imcompatible changes are made to the state format, bump this.
// Restoring incompatible state will abort the job.
const resumeStateStructVersion = 2

// Run is the main enty point of the runner.
func (tr *TestRunner) Run(
	ctx xcontext.Context,
	t *test.Test, targets []*target.Target,
	jobID types.JobID, runID types.RunID,
	resumeState json.RawMessage,
) (json.RawMessage, error) {

	ctx = ctx.WithFields(xcontext.Fields{
		"job_id": jobID,
		"run_id": runID,
	})
	ctx = xcontext.WithValue(ctx, types.KeyJobID, jobID)
	ctx = xcontext.WithValue(ctx, types.KeyRunID, runID)

	ctx.Debugf("== test runner starting job %d, run %d", jobID, runID)
	resumeState, err := tr.run(ctx.WithTag("phase", "run"), t, targets, jobID, runID, resumeState)
	ctx.Debugf("== test runner finished job %d, run %d, err: %v", jobID, runID, err)
	return resumeState, err
}

func (tr *TestRunner) run(
	ctx xcontext.Context,
	t *test.Test, targets []*target.Target,
	jobID types.JobID, runID types.RunID,
	resumeState json.RawMessage,
) (json.RawMessage, error) {

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
			return nil, fmt.Errorf("invalid resume state: %w", err)
		}
		if rs.Version != resumeStateStructVersion {
			return nil, fmt.Errorf("incompatible resume state version %d (want %d)",
				rs.Version, resumeStateStructVersion)
		}
		if rs.JobID != jobID {
			return nil, fmt.Errorf("wrong resume state, job id %d (want %d)", rs.JobID, jobID)
		}
		tr.targets = rs.Targets
	}

	// Set up the pipeline
	for i, sb := range t.TestStepsBundles {
		stepCtx, stepCancel := xcontext.WithCancel(stepsCtx)
		var srs json.RawMessage
		if i < len(rs.StepResumeState) && string(rs.StepResumeState[i]) != "null" {
			srs = rs.StepResumeState[i]
		}
		tr.steps = append(tr.steps, &stepState{
			tr:              tr,
			mu:              &tr.mu,
			ctx:             stepCtx,
			cancel:          stepCancel,
			stepIndex:       i,
			sb:              sb,
			shutdownTimeout: tr.shutdownTimeout,
			inCh:            make(chan *target.Target),
			outCh:           make(chan test.TestStepResult),
			ev: storage.NewTestEventEmitter(testevent.Header{
				JobID:         jobID,
				RunID:         runID,
				TestName:      t.Name,
				TestStepLabel: sb.TestStepLabel,
			}),
			tgtDone:     make(map[*target.Target]bool),
			resumeState: srs,
		})
		// Step handlers will be started from target handlers as targets reach them.
	}

	// Set up the targets
	if tr.targets == nil {
		tr.targets = make(map[string]*targetState)
	}
	// Initialize remaining fields of the target structures,
	// build the map and kick off target processing.
	for _, tgt := range targets {
		tr.mu.Lock()
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
		tgs.handlerRunning = true
		tr.targets[tgt.ID] = tgs
		tr.mu.Unlock()
		tr.targetsWg.Add(1)
		go func() {
			tr.targetHandler(targetsCtx, tgs)
			tr.targetsWg.Done()
		}()
	}

	// Run until no more progress can be made.
	runErr := tr.runMonitor(ctx)
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
	ctx.Debugf("waiting for target handlers to finish")
	targetsCancel()
	tr.targetsWg.Wait()

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
		stepErr := tr.steps[tgs.CurStep].RunErr()
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
		ctx.Debugf("  %d: %s", i, ss)
	}

	// Is there a useful error to report?
	if runErr != nil {
		return nil, runErr
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
			JobID:   jobID, RunID: runID,
			Targets: tr.targets,
		}
		for _, ss := range tr.steps {
			rs.StepResumeState = append(rs.StepResumeState, ss.ResumeState())
		}
		resumeState, runErr = json.Marshal(rs)
		if runErr != nil {
			ctx.Errorf("unable to serialize the state: %s", runErr)
		} else {
			runErr = xcontext.ErrPaused
		}
	default:
	}

	return resumeState, runErr
}

func (tr *TestRunner) waitStepRunners(ctx xcontext.Context) error {
	ctx.Debugf("waiting for step runners to finish")
	swch := make(chan struct{})
	go func() {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		for {
			ok := true
			for _, ss := range tr.steps {
				if ss.IsActive(ctx) {
					ok = false
					break
				}
			}
			if ok {
				close(swch)
				return
			}
			tr.cond.Wait()
		}
	}()
	var err error
	select {
	case <-swch:
		ctx.Debugf("step runners finished")
		tr.mu.Lock()
		defer tr.mu.Unlock()
		err = tr.checkStepRunnersLocked()
	case <-time.After(tr.shutdownTimeout):
		ctx.Errorf("step runners failed to shut down correctly")
		tr.mu.Lock()
		defer tr.mu.Unlock()
		// If there is a step with an error set, use that.
		err = tr.checkStepRunnersLocked()
		// If there isn't, enumerate ones that were still running at the time.
		nrerr := &cerrors.ErrTestStepsNeverReturned{}
		if err == nil {
			err = nrerr
		}
		for _, ss := range tr.steps {
			if ss.StepRunning() {
				ss.SetErrLocked(&cerrors.ErrTestStepsNeverReturned{StepNames: []string{ss.Label()}})
				nrerr.StepNames = append(nrerr.StepNames, ss.Label())
				// Cancel this step's context, this will help release the reader.
				ss.Cancel()
			}
		}
	}
	// Emit step error events.
	for _, ss := range tr.steps {
		if ss.RunErr() != nil && ss.RunErr() != xcontext.ErrPaused && ss.RunErr() != xcontext.ErrCanceled {
			if err := ss.EmitEvent(ctx, EventTestError, nil, ss.RunErr().Error()); err != nil {
				ctx.Errorf("failed to emit event: %s", err)
			}
		}
	}
	return err
}

func (tr *TestRunner) injectTarget(ctx xcontext.Context, tgs *targetState, ss StepState) error {
	ctx.Debugf("%s: injecting into %s", tgs, ss)
	err := ss.Inject(ctx, tgs.tgt)
	if err == nil {
		err = ss.EmitEvent(ctx, target.EventTargetIn, tgs.tgt, nil)
		tr.mu.Lock()
		defer tr.mu.Unlock()
		// By the time we get here the target could have been processed and result posted already, hence the check.
		if tgs.CurPhase == targetStepPhaseBegin {
			tgs.CurPhase = targetStepPhaseRun
		}
		if err != nil {
			err = fmt.Errorf("failed to report target injection: %w", err)
		}
	}
	tr.cond.Signal()
	return err
}

func (tr *TestRunner) awaitTargetResult(ctx xcontext.Context, tgs *targetState, ss StepState) (error, error) {
	select {
	case res, ok := <-tgs.resCh:
		if !ok {
			// Channel is closed when job is paused to make sure all results are processed.
			ctx.Debugf("%s: result channel closed", tgs)
			return nil, xcontext.ErrPaused
		}
		ctx.Debugf("%s: result recd for %s", tgs, ss)
		var err error
		if res == nil {
			err = ss.EmitEvent(ctx, target.EventTargetOut, tgs.tgt, nil)
		} else {
			err = ss.EmitEvent(ctx, target.EventTargetErr, tgs.tgt, target.ErrPayload{Error: res.Error()})
		}
		if err != nil {
			ctx.Errorf("failed to emit event: %s", err)
		}
		return res, err
		// Check for cancellation.
		// Notably we are not checking for the pause condition here:
		// when paused, we want to let all the injected targets to finish
		// and collect all the results they produce. If that doesn't happen,
		// step runner will close resCh on its way out and unblock us.
	case <-ctx.Done():
		tr.mu.Lock()
		ctx.Debugf("%s: canceled 2", tgs)
		tr.mu.Unlock()
		return nil, xcontext.ErrCanceled
	}
}

// targetHandler takes a single target through each step of the pipeline in sequence.
// It injects the target, waits for the result, then moves on to the next step.
func (tr *TestRunner) targetHandler(ctx xcontext.Context, tgs *targetState) {
	ctx = ctx.WithField("target", tgs.tgt.ID)
	ctx.Debugf("%s: target handler active", tgs)
	// NB: CurStep may be non-zero on entry if resumed
loop:
	for stepIndex := tgs.CurStep; stepIndex < len(tr.steps); {
		// Early check for pause or cancelation.
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
		ss := tr.steps[stepIndex]
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
		case targetStepPhaseSleepRetry:
			sleepTime := time.Until(*tgs.NextAttempt)
			ctx.Debugf("%s: sleep for %s before retrying", tgs, sleepTime)
			tr.mu.Unlock()
			select {
			case <-time.After(sleepTime):
				tr.mu.Lock()
				tgs.CurRetry++
				tgs.CurPhase = targetStepPhaseBegin
				tr.mu.Unlock()
			case <-ctx.Until(xcontext.ErrPaused):
			case <-ctx.Done():
			}
			continue loop
		case targetStepPhaseEnd:
			// Resumed in terminal state, we are done.
			tr.mu.Unlock()
			break loop
		default:
			// Must be a job created by an incompatible version of the server, fail it.
			ss.SetErrLocked(fmt.Errorf("%s: invalid phase %s", tgs, tgs.CurPhase))
			break loop
		}
		tr.mu.Unlock()
		// Make sure we have a step runner active. If not, start one.
		ss.RunIfNeeded()
		// Inject the target.
		var err error
		if inject {
			err = tr.injectTarget(ctx, tgs, ss)
		}
		// Await result. It will be communicated to us by the step runner
		// and returned in tgs.res.
		var res error
		if err == nil {
			res, err = tr.awaitTargetResult(ctx, tgs, ss)
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
				ss.SetErrLocked(err)
			}
			tr.mu.Unlock()
			break
		}
		if res != nil {
			tgs.Res = xjson.NewError(res)
			if tgs.CurRetry >= ss.RetryParameters().NumRetries {
				tgs.CurPhase = targetStepPhaseEnd
				tr.mu.Unlock()
				break
			} else {
				nextAttempt := time.Now()
				if ri := ss.RetryParameters().RetryInterval; ri != nil {
					nextAttempt = nextAttempt.Add(time.Duration(*ri))
				}
				tgs.NextAttempt = &nextAttempt
				tr.mu.Unlock()
				tgs.CurPhase = targetStepPhaseSleepRetry
				continue loop
			}
		} else {
			tgs.Res = nil
		}
		// Advance to the next step.
		stepIndex++
		if stepIndex < len(tr.steps) {
			tgs.CurStep = stepIndex
			tgs.CurPhase = targetStepPhaseInit
		}
		tr.mu.Unlock()
		tr.cond.Signal()
	}
	tr.mu.Lock()
	ctx.Debugf("%s: target handler finished", tgs)
	tgs.handlerRunning = false
	tr.cond.Signal()
	tr.mu.Unlock()
}

// checkStepRunnersLocked checks if any step runner has encountered an error.
func (tr *TestRunner) checkStepRunnersLocked() error {
	for i, ss := range tr.steps {
		switch ss.RunErr() {
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
			return ss.RunErr()
		}
	}
	return nil
}

// runMonitor monitors progress of targets through the pipeline
// and closes input channels of the steps to indicate that no more are expected.
// It also monitors steps for critical errors and cancels the whole run.
// Note: input channels remain open when cancellation is requested,
// plugins are expected to handle it explicitly.
func (tr *TestRunner) runMonitor(ctx xcontext.Context) error {
	ctx.Debugf("monitor: active")
	tr.mu.Lock()
	defer tr.mu.Unlock()
	// First, compute the starting step of the pipeline (it may be non-zero
	// if the pipeline was resumed).
	minStep := len(tr.steps)
	for _, tgs := range tr.targets {
		if tgs.CurStep < minStep {
			minStep = tgs.CurStep
		}
	}
	if minStep < len(tr.steps) {
		ctx.Debugf("monitor: starting at step %s", tr.steps[minStep])
	}

	// Run the main loop.
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
		if runErr = tr.checkStepRunnersLocked(); runErr != nil {
			break stepLoop
		}
		if !ok {
			// Wait for notification: as progress is being made, we get notified.
			tr.cond.Wait()
			continue
		}
		// All targets ok, close the step's input channel.
		ctx.Debugf("monitor pass %d: %s: no more targets, closing input channel", pass, ss)
		ss.InputDone()
		step++
	}
	// Wait for all the targets to finish.
	ctx.Debugf("monitor: waiting for targets to finish")
tgtLoop:
	for ; runErr == nil; pass++ {
		if runErr = tr.checkStepRunnersLocked(); runErr != nil {
			break tgtLoop
		}
		done := true
		for _, tgs := range tr.targets {
			ctx.Debugf("monitor pass %d: %s", pass, tgs)
			if tgs.handlerRunning && (tgs.CurStep < len(tr.steps) || tgs.CurPhase != targetStepPhaseEnd) {
				if tgs.CurPhase == targetStepPhaseRun {
					// We have a target inside a step.
					ss := tr.steps[tgs.CurStep]
					if ss.RunErr() == xcontext.ErrPaused {
						// It's been paused, this is fine.
						continue
					}
					if ss.Started() && !ss.IsActive(ctx) {
						// Target has been injected but step runner has exited without a valid reason, this target has been lost.
						runErr = &cerrors.ErrTestStepLostTargets{
							StepName: ss.Label(),
							Targets:  []string{tgs.tgt.ID},
						}
						break tgtLoop
					}
				}
				done = false
				break
			}
		}
		if done {
			break
		}
		// Wait for notification: as progress is being made, we get notified.
		tr.cond.Wait()
	}
	ctx.Debugf("monitor: finished, %v", runErr)
	return runErr
}

func NewTestRunnerWithTimeouts(shutdownTimeout time.Duration) *TestRunner {
	tr := &TestRunner{
		shutdownTimeout: shutdownTimeout,
	}
	tr.cond = sync.NewCond(&tr.mu)
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
	case targetStepPhaseSleepRetry:
		return "sleep_retry"
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
	return fmt.Sprintf("[%s %d %s %d %t %s]",
		tgs.tgt, tgs.CurStep, tgs.CurPhase, tgs.CurRetry, finished, resText)
}
