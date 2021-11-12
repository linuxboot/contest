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

	"github.com/benbjohnson/clock"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/frameworkevent"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// jobInfo describes jobs currently being run.
type jobInfo struct {
	jobID     types.JobID
	targets   []*target.Target
	jobCtx    xcontext.Context
	jobCancel func()
}

// JobRunner implements logic to run, cancel and stop Jobs
type JobRunner struct {
	jobsMapLock sync.Mutex
	jobsMap     map[types.JobID]*jobInfo

	// jobStorage is used to store job reports
	jobStorage storage.JobStorage

	// frameworkEventManager is used by the JobRunner to emit framework events
	frameworkEventManager frameworkevent.EmitterFetcher

	// testEvManager is used by the JobRunner to emit test events
	testEvManager testevent.Fetcher

	// targetLockDuration is the amount of time target lock is extended by
	// while the job is running.
	targetLockDuration time.Duration

	// clock is the time measurement device, mocked out in tests.
	clock clock.Clock

	stopLockRefresh    chan struct{}
	lockRefreshStopped chan struct{}
}

// Run implements the main job running logic. It holds a registry of all running
// jobs that can be referenced when when cancellation/pause/stop requests come in
//
// It returns:
//
// * [][]job.Report: all the run reports, grouped by run, sorted from first to
//                   last
// * []job.Report:   all the final reports
// * error:          an error, if any
func (jr *JobRunner) Run(ctx xcontext.Context, j *job.Job, resumeState *job.PauseEventPayload) (*job.PauseEventPayload, error) {
	var (
		runID           types.RunID = 1
		testID                      = 1
		runDelay        time.Duration
		keepJobEntry    bool
		testRetry       uint32
		nextTestAttempt *time.Time
	)

	// Values are for plugins to read...
	ctx = xcontext.WithValue(ctx, types.KeyJobID, j.ID)
	// .. Fields are for structured logging
	ctx, jobCancel := xcontext.WithCancel(ctx.WithField("job_id", j.ID))

	jr.jobsMapLock.Lock()
	jr.jobsMap[j.ID] = &jobInfo{jobID: j.ID, jobCtx: ctx, jobCancel: jobCancel}
	jr.jobsMapLock.Unlock()
	defer func() {
		if !keepJobEntry {
			jr.jobsMapLock.Lock()
			delete(jr.jobsMap, j.ID)
			jr.jobsMapLock.Unlock()
		}
	}()

	if resumeState != nil {
		runID = resumeState.RunID
		testRetry = resumeState.TestRetry
		nextTestAttempt = resumeState.NextTestAttempt
		if resumeState.TestID > 0 {
			testID = resumeState.TestID
		}
		if resumeState.StartAt != nil {
			// This may get negative. It's fine.
			runDelay = resumeState.StartAt.Sub(jr.clock.Now())
		}

		if len(resumeState.Targets) > 0 {
			jr.jobsMapLock.Lock()
			jr.jobsMap[j.ID].targets = resumeState.Targets
			jr.jobsMapLock.Unlock()
		}
	}

	if j.Runs == 0 {
		ctx.Infof("Running job '%s' (id %v) indefinitely, current run #%d test #%d", j.Name, j.ID, runID, testID)
	} else {
		ctx.Infof("Running job '%s' %d times, starting at #%d test #%d", j.Name, j.Runs, runID, testID)
	}

	onTestPause := func(runID types.RunID, testID int, testRetry uint32, targets []*target.Target, testRunnerState json.RawMessage) (*job.PauseEventPayload, error) {
		ctx.Infof("pause requested for job ID %v", j.ID)
		// Return without releasing targets and keep the job entry so locks continue to be refreshed
		// all the way to server exit.
		keepJobEntry = true
		return &job.PauseEventPayload{
			Version:         job.CurrentPauseEventPayloadVersion,
			JobID:           j.ID,
			RunID:           runID,
			TestID:          testID,
			TestRetry:       testRetry,
			NextTestAttempt: nextTestAttempt,
			Targets:         targets,
			TestRunnerState: testRunnerState,
		}, xcontext.ErrPaused
	}

	ev := storage.NewTestEventFetcher()
	for ; runID <= types.RunID(j.Runs) || j.Runs == 0; runID++ {
		runCtx := xcontext.WithValue(ctx, types.KeyRunID, runID)
		runCtx = runCtx.WithField("run_id", runID)
		if runDelay > 0 {
			nextRun := jr.clock.Now().Add(runDelay)
			runCtx.Infof("Sleeping %s before the next run...", runDelay)
			select {
			case <-jr.clock.After(runDelay):
			case <-ctx.Until(xcontext.ErrPaused):
				resumeState = &job.PauseEventPayload{
					Version: job.CurrentPauseEventPayloadVersion,
					JobID:   j.ID,
					RunID:   runID,
					StartAt: &nextRun,
				}
				runCtx.Infof("Job paused with %s left until next run", nextRun.Sub(jr.clock.Now()))
				return resumeState, xcontext.ErrPaused
			case <-ctx.Done():
				return nil, xcontext.ErrCanceled
			}
		}

		// If we can't emit the run start event, we ignore the error. The framework will
		// try to rebuild the status if it detects that an event might have gone missing
		payload := RunStartedPayload{RunID: runID}
		err := jr.emitEvent(runCtx, j.ID, EventRunStarted, payload)
		if err != nil {
			runCtx.Warnf("Could not emit event run (run %d) start for job %d: %v", runID, j.ID, err)
		}

		for ; testID <= len(j.Tests); testID++ {
			retryParameters := j.Tests[testID-1].RetryParameters
			for ; testRetry <= retryParameters.NumRetries; testRetry++ {
				runCtx.Infof("Current attempt: %d, allowed retries: %d",
					testRetry,
					retryParameters.NumRetries,
				)
				if nextTestAttempt != nil {
					sleepTime := time.Until(*nextTestAttempt)
					if sleepTime > 0 {
						runCtx.Infof("Sleep until next test attempt at '%v'", *nextTestAttempt)
						select {
						case <-runCtx.Done():
							return nil, runCtx.Err()
						case <-runCtx.Until(xcontext.ErrPaused):
							return onTestPause(runID, testID, testRetry, resumeState.Targets, resumeState.TestRunnerState)
						case <-time.After(sleepTime):
							runCtx.Infof("Finish sleep")
						}
					}
				}

				targets, testRunnerState, succeeded, runErr := jr.runTest(runCtx, j, runID, testID, testRetry, resumeState)
				if runErr == xcontext.ErrPaused {
					return onTestPause(runID, testID, testRetry, targets, testRunnerState)
				}
				if runErr != nil {
					return nil, runErr
				}

				if succeeded {
					break
				}

				resumeState = nil
				if retryParameters.RetryInterval != nil {
					nextAttempt := time.Now().Add(time.Duration(*retryParameters.RetryInterval))
					nextTestAttempt = &nextAttempt
				}
			}
			testRetry = 0
			nextTestAttempt = nil
			resumeState = nil
		}

		// Calculate results for this run via the registered run reporters
		runCoordinates := job.RunCoordinates{JobID: j.ID, RunID: runID}
		for _, bundle := range j.RunReporterBundles {
			runStatus, err := jr.BuildRunStatus(ctx, runCoordinates, j)
			if err != nil {
				ctx.Warnf("could not build run status for job %d: %v. Run report will not execute", j.ID, err)
				continue
			}
			success, data, err := bundle.Reporter.RunReport(runCtx, bundle.Parameters, runStatus, ev)
			if err != nil {
				ctx.Warnf("Run reporter failed while calculating run results, proceeding anyway: %v", err)
			} else {
				if success {
					ctx.Infof("Run #%d of job %d considered successful according to %s", runID, j.ID, bundle.Reporter.Name())
				} else {
					ctx.Errorf("Run #%d of job %d considered failed according to %s", runID, j.ID, bundle.Reporter.Name())
				}
			}
			report := &job.Report{
				JobID:        j.ID,
				RunID:        runID,
				ReporterName: bundle.Reporter.Name(),
				ReportTime:   jr.clock.Now(),
				Success:      success,
				Data:         data,
			}
			if err := jr.jobStorage.StoreReport(ctx, report); err != nil {
				ctx.Warnf("Could not store job run report: %v", err)
			}
		}

		testID = 1
		runDelay = j.RunInterval
	}

	// Prepare final reports.

	for _, bundle := range j.FinalReporterBundles {
		// Build a RunStatus object for each run that we executed. We need to check if we interrupted
		// execution early and we did not perform all runs
		runStatuses, err := jr.BuildRunStatuses(ctx, j)
		if err != nil {
			ctx.Warnf("could not calculate run statuses: %v. Run report will not execute", err)
			continue
		}

		success, data, err := bundle.Reporter.FinalReport(ctx, bundle.Parameters, runStatuses, ev)
		if err != nil {
			ctx.Warnf("Final reporter failed while calculating test results, proceeding anyway: %v", err)
		} else {
			if success {
				ctx.Infof("Job %d (%d runs out of %d desired) considered successful", j.ID, runID-1, j.Runs)
			} else {
				ctx.Errorf("Job %d (%d runs out of %d desired) considered failed", j.ID, runID-1, j.Runs)
			}
		}
		report := &job.Report{
			JobID:        j.ID,
			RunID:        0,
			ReporterName: bundle.Reporter.Name(),
			ReportTime:   jr.clock.Now(),
			Success:      success,
			Data:         data,
		}
		if err := jr.jobStorage.StoreReport(ctx, report); err != nil {
			ctx.Warnf("Could not store job run report: %v", err)
		}
	}

	return nil, nil
}

func (jr *JobRunner) runTest(ctx xcontext.Context,
	j *job.Job, runID types.RunID, testID int, testRetry uint32,
	resumeState *job.PauseEventPayload,
) ([]*target.Target, json.RawMessage, bool, error) {
	t := j.Tests[testID-1]
	ctx.Infof("Run #%d: fetching targets for test '%s'", runID, t.Name)
	bundle := t.TargetManagerBundle
	var (
		targets  []*target.Target
		errCh    = make(chan error, 1)
		acquired bool
		succeed  bool
		runErr   error
	)

	// the Acquire semantic is synchronous, so that the implementation
	// is simpler on the user's side. We run it in a goroutine in
	// order to use a timeout for target acquisition.
	tl := target.GetLocker()
	go func() {
		// If resuming with targets already acquired, just make sure we still own them.
		if resumeState != nil && resumeState.Targets != nil {
			targets = resumeState.Targets
			if err := tl.RefreshLocks(ctx, j.ID, jr.targetLockDuration, targets); err != nil {
				errCh <- fmt.Errorf("failed to refresh locks %v: %w", targets, err)
			}
			errCh <- nil
			return
		}
		if len(targets) == 0 {
			var err error
			targets, err = bundle.TargetManager.Acquire(ctx, j.ID, j.TargetManagerAcquireTimeout+jr.targetLockDuration, bundle.AcquireParameters, tl)
			if err != nil {
				errCh <- err
				return
			}
			acquired = true
		}
		// Lock all the targets returned by Acquire.
		// Targets can also be locked in the `Acquire` method, for
		// example to allow dynamic acquisition.
		// We lock them again to ensure that all the acquired
		// targets are locked before running the job.
		// Locking an already-locked target (by the same owner)
		// extends the locking deadline.
		if err := tl.Lock(ctx, j.ID, jr.targetLockDuration, targets); err != nil {
			errCh <- fmt.Errorf("target locking failed: %w", err)
		}
		errCh <- nil
	}()

	// wait for targets up to a certain amount of time
	select {
	case err := <-errCh:
		if err != nil {
			err = fmt.Errorf("run #%d: cannot fetch targets for test '%s': %v", runID, t.Name, err)
			ctx.Errorf(err.Error())
			return nil, nil, false, err
		}
		// Associate targets with the job. Background routine will refresh the locks periodically.
		jr.jobsMapLock.Lock()
		jr.jobsMap[j.ID].targets = targets
		jr.jobsMapLock.Unlock()
	case <-jr.clock.After(j.TargetManagerAcquireTimeout):
		return nil, nil, false, fmt.Errorf("target manager acquire timed out after %s", j.TargetManagerAcquireTimeout)
		// Note: not handling cancellation here to allow TM plugins to wrap up correctly.
		// We have timeout to ensure it doesn't get stuck forever.
	}

	header := testevent.Header{JobID: j.ID, RunID: runID, TestName: t.Name, TestRetry: testRetry}
	testEventEmitter := storage.NewTestEventEmitter(header)

	// Emit events tracking targets acquisition
	if acquired {
		runErr = jr.emitTargetEvents(ctx, testEventEmitter, targets, target.EventTargetAcquired)
	}

	// Check for pause during target acquisition.
	select {
	case <-ctx.Until(xcontext.ErrPaused):
		ctx.Infof("pause requested for job ID %v", j.ID)
		return targets, nil, false, xcontext.ErrPaused
	default:
	}

	if runErr == nil {
		ctx.Infof("Run #%d: running test #%d for job '%s' (job ID: %d) on %d targets",
			runID, testID, j.Name, j.ID, len(targets))
		testRunner := NewTestRunner()
		var testRunnerState json.RawMessage
		if resumeState != nil {
			testRunnerState = resumeState.TestRunnerState
		}

		testRunnerState, targetsResults, err := testRunner.Run(ctx, t, targets, j.ID, runID, testRetry, testRunnerState)
		succeed = len(targetsResults) == len(targets)
		for targetID, targetErr := range targetsResults {
			if targetErr != nil {
				ctx.Infof("target '%s' failed with err: '%v'", targetID, err)
				succeed = false
			}
		}

		if err == xcontext.ErrPaused {
			return targets, testRunnerState, succeed, err
		}
		runErr = err
	}

	// Job is done, release all the targets
	go func() {
		// the Release semantic is synchronous, so that the implementation
		// is simpler on the user's side. We run it in a goroutine in
		// order to use a timeout for target acquisition. If Release fails, whether
		// due to an error or for a timeout, the whole Job is considered failed
		err := bundle.TargetManager.Release(ctx, j.ID, targets, bundle.ReleaseParameters)
		// Announce that targets have been released
		_ = jr.emitTargetEvents(ctx, testEventEmitter, targets, target.EventTargetReleased)
		// Stop refreshing the targets.
		// Here we rely on the fact that jobsMapLock is held continuously during refresh.
		jr.jobsMapLock.Lock()
		jr.jobsMap[j.ID].targets = nil
		jr.jobsMapLock.Unlock()
		if err := tl.Unlock(ctx, j.ID, targets); err == nil {
			ctx.Infof("Unlocked %d target(s) for job ID %d", len(targets), j.ID)
		} else {
			ctx.Warnf("Failed to unlock %d target(s) (%v): %v", len(targets), targets, err)
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err != nil {
			errRelease := fmt.Sprintf("Failed to release targets: %v", err)
			ctx.Errorf(errRelease)
			return nil, nil, succeed, fmt.Errorf(errRelease)
		}
	case <-jr.clock.After(j.TargetManagerReleaseTimeout):
		return nil, nil, succeed, fmt.Errorf("target manager release timed out after %s", j.TargetManagerReleaseTimeout)
		// Ignore cancellation here, we want release and unlock to happen in that case.
	}
	// return the Run error only after releasing the targets, and only
	// if we are not running indefinitely. An error returned by the TestRunner
	// is considered a fatal condition and will cause the termination of the
	// whole job.
	return nil, nil, succeed, runErr
}

func (jr *JobRunner) lockRefresher() {
	// refresh locks a bit faster than locking timeout to avoid races
	interval := jr.targetLockDuration / 10 * 9
loop:
	for {
		select {
		case <-jr.clock.After(interval):
			jr.RefreshLocks()
		case <-jr.stopLockRefresh:
			break loop
		}
	}
	close(jr.lockRefreshStopped)
}

// StartLockRefresh starts the background lock refresh routine.
func (jr *JobRunner) StartLockRefresh() {
	go jr.lockRefresher()
}

// StopLockRefresh stops the background lock refresh routine.
func (jr *JobRunner) StopLockRefresh() {
	close(jr.stopLockRefresh)
	<-jr.lockRefreshStopped
}

// RefreshLocks refreshes locks for running or paused jobs.
func (jr *JobRunner) RefreshLocks() {
	// Note: For simplicity we perform refresh for all jobs while continuously holding the lock over the entire map.
	// Should this become a problem, this can be made more granualar. When doing it, it is important to synchronise
	// refresh with release (avoid releasing targets while refresh is ongoing).
	jr.jobsMapLock.Lock()
	defer jr.jobsMapLock.Unlock()
	var wg sync.WaitGroup
	for jobID := range jr.jobsMap {
		ji := jr.jobsMap[jobID]
		if len(ji.targets) == 0 {
			continue
		}
		wg.Add(1)
		go func() { // Refresh locks for all the jobs in parallel.
			tl := target.GetLocker()
			select {
			case <-ji.jobCtx.Done():
				// Job has been canceled, nothing to do
				break
			default:
				ji.jobCtx.Debugf("Refreshing target locks...")
				if err := tl.RefreshLocks(ji.jobCtx, ji.jobID, jr.targetLockDuration, ji.targets); err != nil {
					ji.jobCtx.Errorf("Failed to refresh %d locks for job ID %d (%v), aborting job", len(ji.targets), ji.jobID, err)
					// We lost our grip on targets, fold the tent and leave ASAP.
					ji.jobCancel()
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

// emitTargetEvents emits test events to keep track of Target acquisition and release
func (jr *JobRunner) emitTargetEvents(ctx xcontext.Context, emitter testevent.Emitter, targets []*target.Target, eventName event.Name) error {
	// The events hold a serialization of the Target in the payload
	for _, t := range targets {
		data := testevent.Data{EventName: eventName, Target: t}
		if err := emitter.Emit(ctx, data); err != nil {
			ctx.Warnf("could not emit event %s: %v", eventName, err)
			return err
		}
	}
	return nil
}

func (jr *JobRunner) emitEvent(ctx xcontext.Context, jobID types.JobID, eventName event.Name, payload interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		ctx.Warnf("could not encode payload for event %s: %v", eventName, err)
		return err
	}

	rawPayload := json.RawMessage(payloadJSON)
	ev := frameworkevent.Event{JobID: jobID, EventName: eventName, Payload: &rawPayload, EmitTime: jr.clock.Now()}
	if err := jr.frameworkEventManager.Emit(ctx, ev); err != nil {
		ctx.Warnf("could not emit event %s: %v", eventName, err)
		return err
	}
	return nil
}

// NewJobRunner returns a new JobRunner, which holds an empty registry of jobs
func NewJobRunner(js storage.JobStorage, clk clock.Clock, lockDuration time.Duration) *JobRunner {
	jr := &JobRunner{
		jobsMap:               make(map[types.JobID]*jobInfo),
		jobStorage:            js,
		frameworkEventManager: storage.NewFrameworkEventEmitterFetcher(),
		testEvManager:         storage.NewTestEventFetcher(),
		targetLockDuration:    lockDuration,
		clock:                 clk,
		stopLockRefresh:       make(chan struct{}),
		lockRefreshStopped:    make(chan struct{}),
	}
	return jr
}
