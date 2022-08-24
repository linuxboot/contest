package runner

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/linuxboot/contest/pkg/cerrors"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
	"github.com/linuxboot/contest/plugins/teststeps"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestStepRunnerSuite(t *testing.T) {
	suite.Run(t, new(StepRunnerSuite))
}

type StepRunnerSuite struct {
	BaseTestSuite
}

func checkSuccessfulResult(t *testing.T, result ChanNotifier) {
	err, ok := <-result.NotifyCh()
	require.True(t, ok)
	require.NoError(t, err)
	_, ok = <-result.NotifyCh()
	require.False(t, ok)
}

func checkErrorResult(t *testing.T, result ChanNotifier) {
	err, ok := <-result.NotifyCh()
	require.True(t, ok)
	require.Error(t, err)
	_, ok = <-result.NotifyCh()
	require.False(t, ok)
}

func (s *StepRunnerSuite) TestRunningStep() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	targetsReaction := map[string]error{
		"TSucc": nil,
		"TFail": fmt.Errorf("oops"),
	}

	var mu sync.Mutex
	var obtainedTargets []target.Target
	var obtainedResumeState json.RawMessage

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, io test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			obtainedResumeState = resumeState
			_, err := teststeps.ForEachTarget(stateFullStepName, ctx, io, func(ctx xcontext.Context, target *target.Target) error {
				require.NotNil(s.T(), target)

				mu.Lock()
				defer mu.Unlock()
				obtainedTargets = append(obtainedTargets, *target)
				return targetsReaction[target.ID]
			})
			if err != nil {
				return nil, err
			}
			return json.RawMessage("{\"output\": true}"), nil
		},
		nil,
	)
	require.NoError(s.T(), err)

	stepRunner := NewStepRunner()
	require.NotNil(s.T(), stepRunner)

	emitterFactory := NewTestStepEventsEmitterFactory(s.MemoryStorage.StorageEngineVault, 1, 1, testName, 0)
	emitter := emitterFactory.New("test_step_label")

	inputResumeState := json.RawMessage("{\"some_input\": 42}")
	addTarget, resumedTargetsResults, runResult, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		newStepsVariablesMock(nil, nil),
		emitter,
		inputResumeState,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), addTarget)
	require.Empty(s.T(), resumedTargetsResults)
	require.NotNil(s.T(), runResult)

	tgtResult, err := addTarget(ctx, tgt("TSucc"))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), tgtResult)
	checkSuccessfulResult(s.T(), tgtResult)

	tgtResult, err = addTarget(ctx, tgt("TFail"))
	require.NoError(s.T(), err)
	require.NotNil(s.T(), tgtResult)
	checkErrorResult(s.T(), tgtResult)

	stepRunner.Stop()
	checkSuccessfulResult(s.T(), runResult)

	closedCtx, closedCtxCancel := xcontext.WithCancel(ctx)
	closedCtxCancel()

	// if step runner has results, it should return them even if input context is closed
	res, err := stepRunner.WaitResults(closedCtx)
	require.NoError(s.T(), err)

	require.Equal(s.T(), json.RawMessage("{\"output\": true}"), res.ResumeState)
	require.NoError(s.T(), res.Err)

	require.Equal(s.T(), inputResumeState, obtainedResumeState)
}

func (s *StepRunnerSuite) TestAddSameTargetSequentiallyTimes() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	const inputTargetID = "input_target_id"

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, io test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			_, err := teststeps.ForEachTarget(stateFullStepName, ctx, io, func(ctx xcontext.Context, target *target.Target) error {
				require.NotNil(s.T(), target)
				require.Equal(s.T(), inputTargetID, target.ID)
				return nil
			})
			require.NoError(s.T(), err)
			return nil, nil
		},
		nil,
	)
	require.NoError(s.T(), err)

	emitterFactory := NewTestStepEventsEmitterFactory(s.MemoryStorage.StorageEngineVault, 1, 1, testName, 0)
	emitter := emitterFactory.New("test_step_label")

	stepRunner := NewStepRunner()
	require.NotNil(s.T(), stepRunner)
	defer stepRunner.Stop()

	addTarget, _, runResult, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		newStepsVariablesMock(nil, nil),
		emitter,
		nil,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), runResult)

	for i := 0; i < 10; i++ {
		tgtResult, err := addTarget(ctx, tgt(inputTargetID))
		require.NoError(s.T(), err)
		checkSuccessfulResult(s.T(), tgtResult)
	}
	stepRunner.Stop()
	checkSuccessfulResult(s.T(), runResult)
}

func (s *StepRunnerSuite) TestAddTargetReturnsErrorIfFailsToInput() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	const inputTargetID = "input_target_id"

	hangCh := make(chan struct{})
	defer func() {
		select {
		case <-hangCh:
		default:
			close(hangCh)
		}
	}()
	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, io test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			<-hangCh
			for {
				tgt, err := io.Get(ctx)
				require.NoError(s.T(), err)
				require.Nil(s.T(), tgt, "unexpected input")
				break
			}
			return nil, nil
		},
		nil,
	)
	require.NoError(s.T(), err)

	emitterFactory := NewTestStepEventsEmitterFactory(s.MemoryStorage.StorageEngineVault, 1, 1, testName, 0)
	emitter := emitterFactory.New("test_step_label")

	stepRunner := NewStepRunner()
	require.NotNil(s.T(), stepRunner)
	defer stepRunner.Stop()

	addTarget, resumedTargetsResults, runResult, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		newStepsVariablesMock(nil, nil),
		emitter,
		nil,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), addTarget)
	require.Empty(s.T(), resumedTargetsResults)
	require.NotNil(s.T(), runResult)

	s.Run("input_context_cancelled", func() {
		cancelCtx, cncl := xcontext.WithCancel(ctx)
		cncl()

		tgtResult, err := addTarget(cancelCtx, tgt(inputTargetID))
		require.Error(s.T(), err)
		require.Nil(s.T(), tgtResult)
	})

	s.Run("sopped_during_input", func() {
		go func() {
			<-time.After(time.Millisecond)
			stepRunner.Stop()
		}()
		tgtResult, err := addTarget(ctx, tgt(inputTargetID))
		require.Error(s.T(), err)
		require.Nil(s.T(), tgtResult)
	})

	close(hangCh)
	stepRunner.Stop()
	checkSuccessfulResult(s.T(), runResult)
}

func (s *StepRunnerSuite) TestStepPanics() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, ch test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			panic("panic")
		},
		nil,
	)
	require.NoError(s.T(), err)

	stepRunner := NewStepRunner()
	require.NotNil(s.T(), stepRunner)
	defer stepRunner.Stop()

	addTarget, resumedTargetsResults, runResult, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		newStepsVariablesMock(nil, nil),
		NewTestStepEventsEmitterFactory(
			s.MemoryStorage.StorageEngineVault,
			1,
			1,
			testName,
			0,
		).New("test_step_label"),
		nil,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), addTarget)
	require.Empty(s.T(), resumedTargetsResults)
	require.NotNil(s.T(), runResult)

	// some of AddTarget may succeed as it takes some time for a step to panic
	var gotError error
	for i := 0; i < 10; i++ {
		time.Sleep(time.Millisecond)
		if _, err := addTarget(ctx, tgt("target-id")); err != nil {
			gotError = err
		}
	}
	var expectedErrType *cerrors.ErrTestStepPaniced
	require.ErrorAs(s.T(), gotError, &expectedErrType)

	runErr := <-runResult.NotifyCh()
	require.ErrorAs(s.T(), runErr, &expectedErrType)
	_, ok := <-runResult.NotifyCh()
	require.False(s.T(), ok)
}

func (s *StepRunnerSuite) TestCornerCases() {
	ctx, cancel := logrusctx.NewContext(logger.LevelDebug)
	defer cancel()

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, in test.TestStepInputOutput, ev testevent.Emitter,
			stepsVars test.StepsVariables, params test.TestStepParameters, resumeState json.RawMessage) (json.RawMessage, error) {
			_, err := teststeps.ForEachTarget(stateFullStepName, ctx, in, func(ctx xcontext.Context, target *target.Target) error {
				return fmt.Errorf("should not be called")
			})
			return nil, err
		},
		nil,
	)
	require.NoError(s.T(), err)

	emitterFactory := NewTestStepEventsEmitterFactory(s.MemoryStorage.StorageEngineVault, 1, 1, testName, 0)
	emitter := emitterFactory.New("test_step_label")

	s.Run("add_target_after_stop", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		addTarget, _, runResult, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			newStepsVariablesMock(nil, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), addTarget)
		require.NotNil(s.T(), runResult)

		stepRunner.Stop()

		tgtResult, err := addTarget(ctx, tgt("dummy_target"))
		require.Error(s.T(), err)
		require.Nil(s.T(), tgtResult)
		checkSuccessfulResult(s.T(), runResult)
	})

	s.Run("run_twice", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		addTarget, _, runResult, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			newStepsVariablesMock(nil, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), addTarget)
		require.NotNil(s.T(), runResult)

		addTarget2, _, runResult2, err2 := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			newStepsVariablesMock(nil, nil),
			emitter,
			nil,
			nil,
		)
		require.Error(s.T(), err2)
		require.Nil(s.T(), addTarget2)
		require.Nil(s.T(), runResult2)
	})

	s.Run("stop_twice", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		addTarget, _, runResult, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			newStepsVariablesMock(nil, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), addTarget)
		require.NotNil(s.T(), runResult)

		stepRunner.Stop()
		stepRunner.Stop()
		checkSuccessfulResult(s.T(), runResult)
	})

	s.Run("stop_before_run", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		stepRunner.Stop()
		addTarget, _, runResult, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			newStepsVariablesMock(nil, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), addTarget)
		require.NotNil(s.T(), runResult)
		checkSuccessfulResult(s.T(), runResult)
	})
}

type stepsVariablesMock struct {
	add func(tgtID string, name string, value interface{}) error
	get func(tgtID string, stepLabel, name string, value interface{}) error
}

func (sm *stepsVariablesMock) Add(tgtID string, name string, value interface{}) error {
	return sm.add(tgtID, name, value)
}

func (sm *stepsVariablesMock) Get(tgtID string, stepLabel, name string, value interface{}) error {
	return sm.get(tgtID, stepLabel, name, value)
}

func newStepsVariablesMock(
	add func(tgtID string, name string, value interface{}) error,
	get func(tgtID string, stepLabel, name string, value interface{}) error,
) *stepsVariablesMock {
	return &stepsVariablesMock{
		add: add,
		get: get,
	}
}
