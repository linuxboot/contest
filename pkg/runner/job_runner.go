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

	"github.com/benbjohnson/clock"
	"github.com/facebookincubator/go-belt/beltctx"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/experimental/metrics"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/frameworkevent"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/metrics/perf"
	"github.com/linuxboot/contest/pkg/signaling"
	"github.com/linuxboot/contest/pkg/signals"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/types"
)

// jobInfo describes jobs currently being run.
type jobInfo struct {
	jobID     types.JobID
	targets   []*target.Target
	jobCtx    context.Context
	jobCancel func()
}

// JobRunner implements logic to run, cancel and stop Jobs
type JobRunner struct {
	jobsMapLock sync.Mutex
	jobsMap     map[types.JobID]*jobInfo

	// jobStorage is used to store job reports
	jobStorage storage.JobStorage

	// Vault to fetch storage-engines from
	storageEngineVault storage.EngineVault

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
//   - [][]job.Report: all the run reports, grouped by run, sorted from first to
//     last
//   - []job.Report:   all the final reports
//   - error:          an error, if any
func (jr *JobRunner) Run(ctx context.Context, j *job.Job, resumeState *job.PauseEventPayload) (*job.PauseEventPayload, error) {
	if resumeState != nil && resumeState.JobID != j.ID {
		return nil, fmt.Errorf("wrong resume state, job id %d (want %d)", resumeState.JobID, j.ID)
	}

	var (
		runID           types.RunID = 1
		testID                      = 1
		runDelay        time.Duration
		keepJobEntry    bool
		testAttempt     uint32
		nextTestAttempt *time.Time
	)

	// Values are for plugins to read...
	ctx = context.WithValue(ctx, types.KeyJobID, j.ID)
	// .. Fields are for structured logging
	ctx, jobCancel := context.WithCancel(beltctx.WithField(ctx, "job_id", j.ID))

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
		testAttempt = resumeState.TestAttempt
		nextTestAttempt = resumeState.NextTestAttempt
		if resumeState.TestID > 0 {
			testID = resumeState.TestID
		}
		if resumeState.StartAt != nil {
			// This may get negative. It's fine.
			runDelay = resumeState.StartAt.Sub(jr.clock.Now())
		}
	}

	if j.Runs == 0 {
		logging.Infof(ctx, "Running job '%s' (id %v) indefinitely, current run #%d test #%d", j.Name, j.ID, runID, testID)
	} else {
		logging.Infof(ctx, "Running job '%s' %d times, starting at #%d test #%d", j.Name, j.Runs, runID, testID)
	}

	pauseTest := func(
		runID types.RunID,
		testID int,
		testAttempt uint32,
		targets []*target.Target,
		testRunnerState json.RawMessage,
		cleanupRunnerState json.RawMessage,
	) (*job.PauseEventPayload, error) {
		logging.Infof(ctx, "pause requested for job ID %v", j.ID)
		// Return without releasing targets and keep the job entry so locks continue to be refreshed
		// all the way to server exit.
		keepJobEntry = true
		return &job.PauseEventPayload{
			Version:            job.CurrentPauseEventPayloadVersion,
			JobID:              j.ID,
			RunID:              runID,
			TestID:             testID,
			TestAttempt:        testAttempt,
			NextTestAttempt:    nextTestAttempt,
			Targets:            targets,
			TestRunnerState:    testRunnerState,
			CleanupRunnerState: cleanupRunnerState,
		}, signals.Paused
	}

	ev := storage.NewTestEventFetcher(jr.storageEngineVault)
	for ; runID <= types.RunID(j.Runs) || j.Runs == 0; runID++ {
		runCtx := context.WithValue(ctx, types.KeyRunID, runID)
		runCtx = beltctx.WithField(runCtx, "run_id", runID)
		if runDelay > 0 {
			nextRun := jr.clock.Now().Add(runDelay)
			logging.Infof(runCtx, "Sleeping %s before the next run...", runDelay)
			select {
			case <-jr.clock.After(runDelay):
			case <-signaling.Until(ctx, signals.Paused):
				resumeState = &job.PauseEventPayload{
					Version: job.CurrentPauseEventPayloadVersion,
					JobID:   j.ID,
					RunID:   runID,
					StartAt: &nextRun,
				}
				logging.Infof(runCtx, "Job paused with %s left until next run", nextRun.Sub(jr.clock.Now()))
				return resumeState, signals.Paused
			case <-ctx.Done():
				return nil, context.Canceled
			}
		}

		// If we can't emit the run start event, we ignore the error. The framework will
		// try to rebuild the status if it detects that an event might have gone missing
		payload := RunStartedPayload{RunID: runID}
		err := jr.emitEvent(runCtx, j.ID, EventRunStarted, payload)
		if err != nil {
			logging.Warnf(runCtx, "Could not emit event run (run %d) start for job %d: %v", runID, j.ID, err)
		}

		for ; testID <= len(j.Tests); testID++ {
			retryParameters := j.Tests[testID-1].RetryParameters
			for ; testAttempt < retryParameters.NumRetries+1; testAttempt++ {
				usedResumeState := resumeState
				resumeState = nil

				logging.Infof(runCtx, "Current attempt: %d, allowed retries: %d",
					testAttempt,
					retryParameters.NumRetries,
				)
				if nextTestAttempt != nil {
					sleepTime := time.Until(*nextTestAttempt)
					if sleepTime > 0 {
						logging.Infof(runCtx, "Sleep until next test attempt at '%v'", *nextTestAttempt)
						select {
						case <-runCtx.Done():
							return nil, runCtx.Err()
						case <-signaling.Until(ctx, signals.Paused):
							return pauseTest(
								runID,
								testID,
								testAttempt,
								usedResumeState.Targets,
								usedResumeState.TestRunnerState,
								usedResumeState.CleanupRunnerState,
							)
						case <-time.After(sleepTime):
							logging.Infof(runCtx, "Finish sleep")
						}
					}
				}

				targets, testRunnerState, cleanupRunnerState, succeeded, runErr := jr.runTest(runCtx, j, runID, testID, testAttempt, usedResumeState)
				if runErr == signals.Paused {
					return pauseTest(runID, testID, testAttempt, targets, testRunnerState, cleanupRunnerState)
				}
				if runErr != nil {
					return nil, runErr
				}

				if succeeded {
					break
				}

				if retryParameters.RetryInterval > 0 {
					nextAttempt := time.Now().Add(time.Duration(retryParameters.RetryInterval))
					nextTestAttempt = &nextAttempt
				}
			}
			testAttempt = 0
			nextTestAttempt = nil
		}

		// Calculate results for this run via the registered run reporters
		runCoordinates := job.RunCoordinates{JobID: j.ID, RunID: runID}
		for _, bundle := range j.RunReporterBundles {
			runStatus, err := jr.BuildRunStatus(ctx, runCoordinates, j)
			if err != nil {
				logging.Warnf(ctx, "could not build run status for job %d: %v. Run report will not execute", j.ID, err)
				continue
			}
			success, data, err := bundle.Reporter.RunReport(runCtx, bundle.Parameters, runStatus, ev)
			if err != nil {
				logging.Warnf(ctx, "Run reporter failed while calculating run results, proceeding anyway: %v", err)
			} else {
				if success {
					logging.Infof(ctx, "Run #%d of job %d considered successful according to %s", runID, j.ID, bundle.Reporter.Name())
				} else {
					logging.Errorf(ctx, "Run #%d of job %d considered failed according to %s", runID, j.ID, bundle.Reporter.Name())
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
				logging.Warnf(ctx, "Could not store job run report: %v", err)
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
			logging.Warnf(ctx, "could not calculate run statuses: %v. Run report will not execute", err)
			continue
		}

		success, data, err := bundle.Reporter.FinalReport(ctx, bundle.Parameters, runStatuses, ev)
		if err != nil {
			logging.Warnf(ctx, "Final reporter failed while calculating test results, proceeding anyway: %v", err)
		} else {
			if success {
				logging.Infof(ctx, "Job %d (%d runs out of %d desired) considered successful", j.ID, runID-1, j.Runs)
			} else {
				logging.Errorf(ctx, "Job %d (%d runs out of %d desired) considered failed", j.ID, runID-1, j.Runs)
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
			logging.Warnf(ctx, "Could not store job run report: %v", err)
		}
	}

	return nil, nil
}

func (jr *JobRunner) acquireTargets(
	ctx context.Context,
	j *job.Job,
	testID int,
	targetLocker target.Locker,
	resumeTargets []*target.Target,
) ([]*target.Target, bool, error) {
	t := j.Tests[testID-1]
	bundle := t.TargetManagerBundle

	if len(resumeTargets) > 0 {
		if err := targetLocker.RefreshLocks(ctx, j.ID, jr.targetLockDuration, resumeTargets); err != nil {
			return nil, false, fmt.Errorf("failed to refresh locks %v: %w", resumeTargets, err)
		}
		return resumeTargets, false, nil
	}

	var targets []*target.Target
	if len(targets) == 0 {
		var err error
		targets, err = bundle.TargetManager.Acquire(
			ctx, j.ID, j.TargetManagerAcquireTimeout+jr.targetLockDuration, bundle.AcquireParameters, targetLocker)
		if err != nil {
			return nil, false, err
		}
	}
	// Lock all the targets returned by Acquire.
	// Targets can also be locked in the `Acquire` method, for
	// example to allow dynamic acquisition.
	// We lock them again to ensure that all the acquired
	// targets are locked before running the job.
	// Locking an already-locked target (by the same owner)
	// extends the locking deadline.
	if err := targetLocker.Lock(ctx, j.ID, jr.targetLockDuration, targets); err != nil {
		return nil, false, fmt.Errorf("target locking failed: %w", err)
	}

	logging.Infof(ctx, "%d Targets acquired", len(targets))
	// when the targets are acquired, update the counter
	if metrics := metrics.FromCtx(ctx); metrics != nil {
		metrics.IntGauge(perf.ACQUIRED_TARGETS).Add(int64(len(targets)))
	}

	return targets, true, nil
}

func (jr *JobRunner) runTest(ctx context.Context,
	j *job.Job, runID types.RunID, testID int, testAttempt uint32,
	resumeState *job.PauseEventPayload,
) ([]*target.Target, json.RawMessage, json.RawMessage, bool, error) {
	t := j.Tests[testID-1]
	logging.Infof(ctx, "Run #%d: fetching targets for test '%s'", runID, t.Name)
	bundle := t.TargetManagerBundle
	var (
		targets  []*target.Target
		acquired bool
		succeed  bool
		runErr   error
	)

	header := testevent.Header{JobID: j.ID, RunID: runID, TestName: t.Name, TestAttempt: testAttempt}
	testEventEmitter := storage.NewTestEventEmitter(jr.storageEngineVault, header)

	var resumeTargets []*target.Target
	if resumeState != nil && resumeState.Targets != nil {
		resumeTargets = resumeState.Targets
	}

	tl := target.GetLocker()

	acquireCtx, acquireCancel := context.WithTimeout(ctx, j.TargetManagerAcquireTimeout)
	defer acquireCancel()

	// the Acquire semantic is synchronous, so that the implementation
	// is simpler on the user's side. We run it in a goroutine in
	// order to use a timeout for target acquisition.
	// TODO: Make lockers finish on context cancel and remove goroutine
	errCh := make(chan error, 1)
	go func() {
		var err error
		targets, acquired, err = jr.acquireTargets(acquireCtx, j, testID, tl, resumeTargets)
		errCh <- err
	}()

	// wait for targets up to a certain amount of time
	select {
	case acquireErr := <-errCh:
		if acquireErr != nil {
			logging.Errorf(ctx, "run #%d: cannot fetch targets for test '%s': %w", runID, t.Name, acquireErr)

			// Assume that all errors could be retried except cancellation as both
			// target manager and target locking problems can disappear if retried
			if acquireErr == context.Canceled {
				return nil, nil, nil, false, acquireErr
			}

			payload, err := target.MarshallErrPayload(acquireErr.Error())
			if err != nil {
				return nil, nil, nil, false, err
			}

			eventData := testevent.Data{EventName: target.EventTargetAcquireErr, Payload: &payload}
			if err := testEventEmitter.Emit(ctx, eventData); err != nil {
				return nil, nil, nil, false, err
			}
			return nil, nil, nil, false, nil
		}
		// Associate targets with the job. Background routine will refresh the locks periodically.
		jr.jobsMapLock.Lock()
		jr.jobsMap[j.ID].targets = targets
		jr.jobsMapLock.Unlock()
	case <-jr.clock.After(j.TargetManagerAcquireTimeout):
		return nil, nil, nil, false, fmt.Errorf("target manager acquire timed out after %s", j.TargetManagerAcquireTimeout)
		// Note: not handling cancellation here to allow TM plugins to wrap up correctly.
		// We have timeout to ensure it doesn't get stuck forever.
	}

	// Emit events tracking targets acquisition
	if acquired {
		runErr = jr.emitTargetEvents(ctx, testEventEmitter, targets, target.EventTargetAcquired)
	}

	// Check for pause during target acquisition.
	select {
	case <-signaling.Until(ctx, signals.Paused):
		logging.Infof(ctx, "pause requested for job ID %v", j.ID)
		return targets, nil, nil, false, signals.Paused
	default:
	}

	if runErr == nil {
		logging.Infof(ctx, "Run #%d: running test #%d for job '%s' (job ID: %d) on %d targets",
			runID, testID, j.Name, j.ID, len(targets))

		var (
			testRunnerState    json.RawMessage
			cleanupRunnerState json.RawMessage
			targetsResults     map[string]error
			stepOutputs        *testStepsVariables
			err                error
		)
		if resumeState != nil {
			testRunnerState = resumeState.TestRunnerState
			cleanupRunnerState = resumeState.CleanupRunnerState
		}

		runCtx := beltctx.WithFields(ctx, field.Map[any]{
			"job_id":  j.ID,
			"run_id":  runID,
			"attempt": testAttempt,
		})
		runCtx = context.WithValue(runCtx, types.KeyJobID, j.ID)
		runCtx = context.WithValue(runCtx, types.KeyRunID, runID)

		// If a previous run was interrupted in cleanup, do not re-run tests
		if cleanupRunnerState != nil {
			logging.Debugf(runCtx, "== test runner skipping test steps as it was interrupted during cleanup")
		} else {
			testRunner := NewTestRunner()
			logging.Debugf(runCtx, "== test runner starting test steps")
			testRunnerState, targetsResults, stepOutputs, err = testRunner.Run(
				runCtx,
				t,
				targets,
				NewTestStepEventsEmitterFactory(jr.storageEngineVault, j.ID, runID, t.Name, testAttempt),
				testRunnerState,
				nil,
				t.TestStepsBundles,
			)
			logging.Debugf(runCtx, "== test runner test finished, err: %v", err)

			succeed = len(targetsResults) == len(targets)
			for targetID, targetErr := range targetsResults {
				if targetErr != nil {
					logging.Infof(ctx, "target '%s' failed with err: '%v'", targetID, err)
					succeed = false
				}
			}

			if err == signals.Paused {
				return targets, testRunnerState, nil, succeed, err
			}
			runErr = err
		}

		if t.CleanupStepsBundles != nil {
			logging.Debugf(runCtx, "== test runner starting cleanup steps")
			cleanupRunner := NewTestRunner()
			cleanupRunnerState, _, _, err := cleanupRunner.Run(
				runCtx,
				t,
				targets,
				NewTestStepEventsEmitterFactory(jr.storageEngineVault, j.ID, runID, fmt.Sprintf("%s [CLEANUP]", t.Name), testAttempt),
				cleanupRunnerState,
				stepOutputs,
				t.CleanupStepsBundles,
			)
			logging.Debugf(runCtx, "== test runner cleanup finished, err: %v", err)

			if err == signals.Paused {
				return targets, testRunnerState, cleanupRunnerState, succeed, err
			}
			// Ignore errors happening in cleanup steps
		}
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
			logging.Infof(ctx, "Unlocked %d target(s) for job ID %d", len(targets), j.ID)
		} else {
			logging.Warnf(ctx, "Failed to unlock %d target(s) (%v): %v", len(targets), targets, err)
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err != nil {
			errRelease := fmt.Sprintf("Failed to release targets: %v", err)
			logging.Errorf(ctx, errRelease)
			return nil, nil, nil, succeed, fmt.Errorf(errRelease)
		}
	case <-jr.clock.After(j.TargetManagerReleaseTimeout):
		return nil, nil, nil, succeed, fmt.Errorf("target manager release timed out after %s", j.TargetManagerReleaseTimeout)
		// Ignore cancellation here, we want release and unlock to happen in that case.
	}

	logging.Infof(ctx, "%d Targets released", len(targets))
	// If the targets are released, update the counter
	if metrics := metrics.FromCtx(ctx); metrics != nil {
		metrics.IntGauge(perf.ACQUIRED_TARGETS).Add(-int64(len(targets)))
	}

	// return the Run error only after releasing the targets, and only
	// if we are not running indefinitely. An error returned by the TestRunner
	// is considered a fatal condition and will cause the termination of the
	// whole job.
	return nil, nil, nil, succeed, runErr
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
				logging.Debugf(ji.jobCtx, "Refreshing target locks...")
				if err := tl.RefreshLocks(ji.jobCtx, ji.jobID, jr.targetLockDuration, ji.targets); err != nil {
					logging.Errorf(ji.jobCtx, "Failed to refresh %d locks for job ID %d (%v), aborting job", len(ji.targets), ji.jobID, err)
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
func (jr *JobRunner) emitTargetEvents(ctx context.Context, emitter testevent.Emitter, targets []*target.Target, eventName event.Name) error {
	// The events hold a serialization of the Target in the payload
	for _, t := range targets {
		data := testevent.Data{EventName: eventName, Target: t}
		if err := emitter.Emit(ctx, data); err != nil {
			logging.Warnf(ctx, "could not emit event %s: %v", eventName, err)
			return err
		}
	}
	return nil
}

func (jr *JobRunner) emitEvent(ctx context.Context, jobID types.JobID, eventName event.Name, payload interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		logging.Warnf(ctx, "could not encode payload for event %s: %v", eventName, err)
		return err
	}

	rawPayload := json.RawMessage(payloadJSON)
	ev := frameworkevent.Event{JobID: jobID, EventName: eventName, Payload: &rawPayload, EmitTime: jr.clock.Now()}
	if err := jr.frameworkEventManager.Emit(ctx, ev); err != nil {
		logging.Warnf(ctx, "could not emit event %s: %v", eventName, err)
		return err
	}
	return nil
}

type testStepEventsEmitterFactory struct {
	vault storage.EngineVault

	jobID       types.JobID
	runID       types.RunID
	testName    string
	testAttempt uint32
}

func (f *testStepEventsEmitterFactory) New(testStepLabel string) testevent.Emitter {
	return storage.NewTestEventEmitter(f.vault,
		testevent.Header{
			JobID:         f.jobID,
			RunID:         f.runID,
			TestName:      f.testName,
			TestAttempt:   f.testAttempt,
			TestStepLabel: testStepLabel,
		},
	)
}

func NewTestStepEventsEmitterFactory(vault storage.EngineVault,
	jobID types.JobID,
	runID types.RunID,
	testName string,
	testAttempt uint32,
) *testStepEventsEmitterFactory {
	return &testStepEventsEmitterFactory{
		vault:       vault,
		jobID:       jobID,
		runID:       runID,
		testName:    testName,
		testAttempt: testAttempt,
	}
}

// NewJobRunner returns a new JobRunner, which holds an empty registry of jobs
func NewJobRunner(js storage.JobStorage, storageVault storage.EngineVault, clk clock.Clock, lockDuration time.Duration) *JobRunner {
	jr := &JobRunner{
		jobsMap:               make(map[types.JobID]*jobInfo),
		jobStorage:            js,
		storageEngineVault:    storageVault,
		frameworkEventManager: storage.NewFrameworkEventEmitterFetcher(storageVault),
		testEvManager:         storage.NewTestEventFetcher(storageVault),
		targetLockDuration:    lockDuration,
		clock:                 clk,
		stopLockRefresh:       make(chan struct{}),
		lockRefreshStopped:    make(chan struct{}),
	}
	return jr
}
