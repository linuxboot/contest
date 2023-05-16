package runner

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/insomniacslk/xjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
	"github.com/linuxboot/contest/plugins/targetmanagers/targetlist"
	"github.com/linuxboot/contest/plugins/teststeps"
	"github.com/linuxboot/contest/plugins/teststeps/echo"
)

type JobRunnerSuite struct {
	BaseTestSuite
}

func TestTestStepSuite(t *testing.T) {
	suite.Run(t, &JobRunnerSuite{})
}

func (s *JobRunnerSuite) SetupTest() {
	s.BaseTestSuite.SetupTest()

	for _, e := range []struct {
		name    string
		factory test.TestStepFactory
		events  []event.Name
	}{
		{echo.Name, echo.New, echo.Events},
	} {
		require.NoError(s.T(), s.PluginRegistry.RegisterTestStep(e.name, e.factory, e.events))
	}
}

func (s *JobRunnerSuite) TestSimpleJobStartFinish() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	var mu sync.Mutex
	var resultTargets []*target.Target

	require.NoError(s.T(), s.RegisterStateFullStep(
		func(ctx xcontext.Context, io test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			return teststeps.ForEachTarget(stateFullStepName, ctx, io, func(ctx xcontext.Context, target *target.Target) error {
				assert.NotNil(s.T(), target)
				mu.Lock()
				defer mu.Unlock()
				resultTargets = append(resultTargets, target)
				return nil
			})
		},
		nil,
	))

	acquireParameters := targetlist.AcquireParameters{
		Targets: []*target.Target{
			{
				ID: "T1",
			},
		},
	}

	j := job.Job{
		ID:                          1,
		Runs:                        1,
		TargetManagerAcquireTimeout: 10 * time.Second,
		TargetManagerReleaseTimeout: 10 * time.Second,
		Tests: []*test.Test{
			{
				Name: testName,
				TargetManagerBundle: &target.TargetManagerBundle{
					AcquireParameters: acquireParameters,
					TargetManager:     targetlist.New(),
				},
				TestStepsBundles: []test.TestStepBundle{
					s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
				},
			},
		},
	}

	jsm := storage.NewJobStorageManager(s.MemoryStorage.StorageEngineVault)
	jr := NewJobRunner(jsm, s.MemoryStorage.StorageEngineVault, clock.New(), time.Second)
	require.NotNil(s.T(), jr)

	resumeState, err := jr.Run(ctx, &j, nil)
	require.NoError(s.T(), err)
	require.Nil(s.T(), resumeState)

	require.Equal(s.T(), acquireParameters.Targets, resultTargets)

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 ][Target{ID: "T1"} TargetAcquired]}
{[1 1 SimpleTest 0 test_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 test_step_label][Target{ID: "T1"} TargetOut]}
{[1 1 SimpleTest 0 ][Target{ID: "T1"} TargetReleased]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
}

func (s *JobRunnerSuite) TestJobWithTestRetry() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	var mu sync.Mutex
	var resultTargets []*target.Target
	var callsCount int

	require.NoError(s.T(), s.RegisterStateFullStep(
		func(ctx xcontext.Context, io test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			return teststeps.ForEachTarget(stateFullStepName, ctx, io, func(ctx xcontext.Context, target *target.Target) error {
				assert.NotNil(s.T(), target)
				mu.Lock()
				defer mu.Unlock()
				defer func() {
					callsCount++
				}()
				resultTargets = append(resultTargets, target)
				if callsCount == 0 {
					return fmt.Errorf("some error")
				}
				return nil
			})
		},
		nil,
	))

	acquireParameters := targetlist.AcquireParameters{
		Targets: []*target.Target{
			{
				ID: "T1",
			},
		},
	}

	reporter := &collectingReporter{}
	j := job.Job{
		ID:                          1,
		Runs:                        1,
		TargetManagerAcquireTimeout: 10 * time.Second,
		TargetManagerReleaseTimeout: 10 * time.Second,
		RunReporterBundles: []*job.ReporterBundle{
			{
				Reporter: reporter,
			},
		},
		Tests: []*test.Test{
			{
				Name: testName,
				RetryParameters: test.RetryParameters{
					NumRetries:    1,
					RetryInterval: xjson.Duration(time.Millisecond), // make a small interval to test waiting branch
				},
				TargetManagerBundle: &target.TargetManagerBundle{
					AcquireParameters: acquireParameters,
					TargetManager:     targetlist.New(),
				},
				TestStepsBundles: []test.TestStepBundle{
					s.NewStep(ctx, "echo1_step_label", echo.Name, map[string][]test.Param{
						"text": {*test.NewParam("hello")},
					}),
					s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
					s.NewStep(ctx, "echo2_step_label", echo.Name, map[string][]test.Param{
						"text": {*test.NewParam("world")},
					}),
				},
			},
		},
	}

	jsm := storage.NewJobStorageManager(s.MemoryStorage.StorageEngineVault)
	jr := NewJobRunner(jsm, s.MemoryStorage.StorageEngineVault, clock.New(), time.Second)
	require.NotNil(s.T(), jr)

	resumeState, err := jr.Run(ctx, &j, nil)
	require.NoError(s.T(), err)
	require.Nil(s.T(), resumeState)

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 ][Target{ID: "T1"} TargetAcquired]}
{[1 1 SimpleTest 0 echo1_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 echo1_step_label][Target{ID: "T1"} TargetOut]}
{[1 1 SimpleTest 0 test_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 test_step_label][Target{ID: "T1"} TargetErr &"{\"Error\":\"some error\"}"]}
{[1 1 SimpleTest 0 ][Target{ID: "T1"} TargetReleased]}
{[1 1 SimpleTest 1 ][Target{ID: "T1"} TargetAcquired]}
{[1 1 SimpleTest 1 echo1_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 1 echo1_step_label][Target{ID: "T1"} TargetOut]}
{[1 1 SimpleTest 1 test_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 1 test_step_label][Target{ID: "T1"} TargetOut]}
{[1 1 SimpleTest 1 echo2_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 1 echo2_step_label][Target{ID: "T1"} TargetOut]}
{[1 1 SimpleTest 1 ][Target{ID: "T1"} TargetReleased]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))

	require.Len(s.T(), reporter.runStatuses, 1)
	require.Len(s.T(), reporter.runStatuses[0].TestStatuses, 1)

	ts := reporter.runStatuses[0].TestStatuses[0]
	require.Equal(s.T(), j.ID, ts.JobID)
	require.Equal(s.T(), types.RunID(0x1), ts.RunID)

	for _, tgs := range ts.TargetStatuses {
		require.Equal(s.T(), "T1", tgs.Target.ID)
		for _, ev := range tgs.Events {
			require.Equal(s.T(), uint32(1), ev.Header.TestAttempt)
		}
	}
}

func (s *JobRunnerSuite) TestJobRetryOnFailedAcquire() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	var mu sync.Mutex
	var acquireCalledCnt int
	tm := stateFullTargetManager{
		validateAcquireParametersFunc: func([]byte) (interface{}, error) { return nil, nil },
		validateReleaseParametersFunc: func(bytes []byte) (interface{}, error) { return nil, nil },
		acquireFunc: func(ctx xcontext.Context, jobID types.JobID, jobTargetManagerAcquireTimeout time.Duration,
			parameters interface{}, tl target.Locker,
		) ([]*target.Target, error) {
			mu.Lock()
			defer mu.Unlock()
			acquireCalledCnt++

			require.Less(s.T(), acquireCalledCnt, 3)
			if acquireCalledCnt == 1 {
				return nil, fmt.Errorf("some error")
			}
			return []*target.Target{
				{
					ID: "T1",
				},
			}, nil
		},
		releaseFunc: func(ctx xcontext.Context, jobID types.JobID, targets []*target.Target, parameters interface{}) error {
			return nil
		},
	}

	reporter := &collectingReporter{}
	j := job.Job{
		ID:                          1,
		Runs:                        1,
		TargetManagerAcquireTimeout: 10 * time.Second,
		TargetManagerReleaseTimeout: 10 * time.Second,
		RunReporterBundles: []*job.ReporterBundle{
			{
				Reporter: reporter,
			},
		},
		Tests: []*test.Test{
			{
				Name: testName,
				RetryParameters: test.RetryParameters{
					NumRetries:    1,
					RetryInterval: xjson.Duration(time.Millisecond), // make a small interval to test waiting branch
				},
				TargetManagerBundle: &target.TargetManagerBundle{
					TargetManager: tm,
				},
				TestStepsBundles: []test.TestStepBundle{
					s.NewStep(ctx, "echo1_step_label", echo.Name, map[string][]test.Param{
						"text": {*test.NewParam("hello")},
					}),
				},
			},
		},
	}

	jsm := storage.NewJobStorageManager(s.MemoryStorage.StorageEngineVault)
	jr := NewJobRunner(jsm, s.MemoryStorage.StorageEngineVault, clock.New(), time.Second)
	require.NotNil(s.T(), jr)

	resumeState, err := jr.Run(ctx, &j, nil)
	require.NoError(s.T(), err)
	require.Nil(s.T(), resumeState)

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 ][(*Target)(nil) TargetAcquireErr &"{\"Error\":\"some error\"}"]}
{[1 1 SimpleTest 1 ][Target{ID: "T1"} TargetAcquired]}
{[1 1 SimpleTest 1 echo1_step_label][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 1 echo1_step_label][Target{ID: "T1"} TargetOut]}
{[1 1 SimpleTest 1 ][Target{ID: "T1"} TargetReleased]}
`, s.MemoryStorage.GetTestEvents(ctx, testName))

	require.Len(s.T(), reporter.runStatuses, 1)
	require.Len(s.T(), reporter.runStatuses[0].TestStatuses, 1)

	ts := reporter.runStatuses[0].TestStatuses[0]
	require.Equal(s.T(), j.ID, ts.JobID)
	require.Equal(s.T(), types.RunID(0x1), ts.RunID)

	for _, tgs := range ts.TargetStatuses {
		require.Equal(s.T(), "T1", tgs.Target.ID)
		require.Empty(s.T(), tgs.Error)
		for _, ev := range tgs.Events {
			require.Equal(s.T(), uint32(1), ev.Header.TestAttempt)
		}
	}
}

func (s *JobRunnerSuite) TestAcquireFailed() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	tm := stateFullTargetManager{
		validateAcquireParametersFunc: func([]byte) (interface{}, error) { return nil, nil },
		validateReleaseParametersFunc: func(bytes []byte) (interface{}, error) { return nil, nil },
		acquireFunc: func(ctx xcontext.Context, jobID types.JobID, jobTargetManagerAcquireTimeout time.Duration,
			parameters interface{}, tl target.Locker,
		) ([]*target.Target, error) {
			return nil, fmt.Errorf("some error")
		},
		releaseFunc: func(ctx xcontext.Context, jobID types.JobID, targets []*target.Target, parameters interface{}) error {
			return nil
		},
	}

	reporter := &collectingReporter{}
	j := job.Job{
		ID:                          1,
		Runs:                        1,
		TargetManagerAcquireTimeout: 10 * time.Second,
		TargetManagerReleaseTimeout: 10 * time.Second,
		RunReporterBundles: []*job.ReporterBundle{
			{
				Reporter: reporter,
			},
		},
		Tests: []*test.Test{
			{
				Name: testName,
				RetryParameters: test.RetryParameters{
					NumRetries:    1,
					RetryInterval: xjson.Duration(time.Millisecond), // make a small interval to test waiting branch
				},
				TargetManagerBundle: &target.TargetManagerBundle{
					TargetManager: tm,
				},
				TestStepsBundles: []test.TestStepBundle{
					s.NewStep(ctx, "echo1_step_label", echo.Name, map[string][]test.Param{
						"text": {*test.NewParam("hello")},
					}),
				},
			},
		},
	}

	jsm := storage.NewJobStorageManager(s.MemoryStorage.StorageEngineVault)
	jr := NewJobRunner(jsm, s.MemoryStorage.StorageEngineVault, clock.New(), time.Second)
	require.NotNil(s.T(), jr)

	resumeState, err := jr.Run(ctx, &j, nil)
	require.NoError(s.T(), err)
	require.Nil(s.T(), resumeState)

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 ][(*Target)(nil) TargetAcquireErr &"{\"Error\":\"some error\"}"]}
{[1 1 SimpleTest 1 ][(*Target)(nil) TargetAcquireErr &"{\"Error\":\"some error\"}"]}
`, s.MemoryStorage.GetTestEvents(ctx, testName))

	require.Len(s.T(), reporter.runStatuses, 1)
	require.Len(s.T(), reporter.runStatuses[0].TestStatuses, 1)

	ts := reporter.runStatuses[0].TestStatuses[0]
	require.Equal(s.T(), j.ID, ts.JobID)
	require.Equal(s.T(), types.RunID(0x1), ts.RunID)

	for _, tgs := range ts.TargetStatuses {
		require.NotEmpty(s.T(), tgs.Error)
		for _, ev := range tgs.Events {
			require.Equal(s.T(), uint32(1), ev.Header.TestAttempt)
		}
	}
}

func (s *JobRunnerSuite) TestResumeStateBadJobId() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	acquireParameters := targetlist.AcquireParameters{
		Targets: []*target.Target{
			{
				ID: "T1",
			},
		},
	}

	j := job.Job{
		ID:                          1,
		Runs:                        1,
		TargetManagerAcquireTimeout: 10 * time.Second,
		TargetManagerReleaseTimeout: 10 * time.Second,
		RunReporterBundles: []*job.ReporterBundle{
			{
				Reporter: &collectingReporter{},
			},
		},
		Tests: []*test.Test{
			{
				Name: testName,
				RetryParameters: test.RetryParameters{
					NumRetries:    1,
					RetryInterval: xjson.Duration(time.Millisecond), // make a small interval to test waiting branch
				},
				TargetManagerBundle: &target.TargetManagerBundle{
					AcquireParameters: acquireParameters,
					TargetManager:     targetlist.New(),
				},
				TestStepsBundles: []test.TestStepBundle{
					s.NewStep(ctx, "echo1_step_label", echo.Name, map[string][]test.Param{
						"text": {*test.NewParam("hello")},
					}),
				},
			},
		},
	}

	jsm := storage.NewJobStorageManager(s.MemoryStorage.StorageEngineVault)
	jr := NewJobRunner(jsm, s.MemoryStorage.StorageEngineVault, clock.New(), time.Second)
	require.NotNil(s.T(), jr)

	inputResumeState := job.PauseEventPayload{
		Version: job.CurrentPauseEventPayloadVersion,
		JobID:   j.ID + 1,
		RunID:   1,
		TestID:  1,
	}

	resumeState, err := jr.Run(ctx, &j, &inputResumeState)
	require.Error(s.T(), err)
	require.Nil(s.T(), resumeState)
}

const stateFullStepName = "statefull"

type stateFullStep struct {
	runFunction func(ctx xcontext.Context, io test.TestStepInputOutput, ev testevent.Emitter,
		stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error)
	validateFunction func(ctx xcontext.Context, params test.TestStepParameters) error
}

func (sfs *stateFullStep) Name() string {
	return stateFullStepName
}

func (sfs *stateFullStep) Run(
	ctx xcontext.Context,
	io test.TestStepInputOutput,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	if sfs.runFunction == nil {
		return nil, fmt.Errorf("stateFullStep run is not initialised")
	}
	return sfs.runFunction(ctx, io, ev, stepsVars, params, resumeState)
}

func (sfs *stateFullStep) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	if sfs.validateFunction == nil {
		return nil
	}
	return sfs.validateFunction(ctx, params)
}

type stateFullTargetManager struct {
	validateAcquireParametersFunc func([]byte) (interface{}, error)
	validateReleaseParametersFunc func([]byte) (interface{}, error)
	acquireFunc                   func(ctx xcontext.Context, jobID types.JobID, jobTargetManagerAcquireTimeout time.Duration,
		parameters interface{}, tl target.Locker) ([]*target.Target, error)
	releaseFunc func(ctx xcontext.Context, jobID types.JobID, targets []*target.Target, parameters interface{}) error
}

func (sfm stateFullTargetManager) ValidateAcquireParameters(params []byte) (interface{}, error) {
	return sfm.validateAcquireParametersFunc(params)
}

func (sfm stateFullTargetManager) ValidateReleaseParameters(params []byte) (interface{}, error) {
	return sfm.validateReleaseParametersFunc(params)
}

func (sfm stateFullTargetManager) Acquire(
	ctx xcontext.Context,
	jobID types.JobID,
	jobTargetManagerAcquireTimeout time.Duration,
	parameters interface{},
	tl target.Locker,
) ([]*target.Target, error) {
	return sfm.acquireFunc(ctx, jobID, jobTargetManagerAcquireTimeout, parameters, tl)
}

func (sfm stateFullTargetManager) Release(ctx xcontext.Context, jobID types.JobID, targets []*target.Target, parameters interface{}) error {
	return sfm.releaseFunc(ctx, jobID, targets, parameters)
}

const collectingReporterName = "collectingReporter"

type collectingReporter struct {
	runStatuses []job.RunStatus
}

func (r *collectingReporter) Name() string {
	return collectingReporterName
}

func (r *collectingReporter) ValidateRunParameters([]byte) (interface{}, error) {
	return nil, nil
}

func (r *collectingReporter) ValidateFinalParameters([]byte) (interface{}, error) {
	return nil, nil
}

func (r *collectingReporter) RunReport(ctx xcontext.Context, parameters interface{}, runStatus *job.RunStatus, ev testevent.Fetcher) (bool, interface{}, error) {
	r.runStatuses = append(r.runStatuses, *runStatus)
	return true, nil, nil
}

func (r *collectingReporter) FinalReport(ctx xcontext.Context, parameters interface{}, runStatuses []job.RunStatus, ev testevent.Fetcher) (bool, interface{}, error) {
	return true, nil, nil
}
