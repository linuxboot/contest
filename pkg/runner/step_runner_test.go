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

func checkStoppedSuccessfully(t *testing.T, resultChan <-chan StepRunnerEvent) {
	ev := <-resultChan
	require.NotNil(t, ev)
	require.Nil(t, ev.Target)
	require.NoError(t, ev.Err)
	_, ok := <-resultChan
	require.False(t, ok)
}

func (s *StepRunnerSuite) TestRunningStep() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	targetsReaction := map[string]error{
		"TSucc": nil,
		"TFail": fmt.Errorf("oops"),
	}

	var mu sync.Mutex
	var obtainedTargets []target.Target
	var obtainedResumeState json.RawMessage

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
			obtainedResumeState = resumeState
			_, err := teststeps.ForEachTarget(stateFullStepName, ctx, ch, func(ctx xcontext.Context, target *target.Target) error {
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
	resultChan, addTarget, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		emitter,
		inputResumeState,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resultChan)

	require.NoError(s.T(), addTarget(ctx, tgt("TSucc")))
	ev, ok := <-resultChan
	require.True(s.T(), ok)
	require.Equal(s.T(), tgt("TSucc"), ev.Target)
	require.NoError(s.T(), ev.Err)

	require.NoError(s.T(), addTarget(ctx, tgt("TFail")))
	ev, ok = <-resultChan
	require.True(s.T(), ok)
	require.Equal(s.T(), tgt("TFail"), ev.Target)
	require.Error(s.T(), ev.Err)

	stepRunner.Stop()
	checkStoppedSuccessfully(s.T(), resultChan)

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
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	const inputTargetID = "input_target_id"

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
			_, err := teststeps.ForEachTarget(stateFullStepName, ctx, ch, func(ctx xcontext.Context, target *target.Target) error {
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

	resultChan, addTarget, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		emitter,
		nil,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resultChan)

	for i := 0; i < 10; i++ {
		require.NoError(s.T(), addTarget(ctx, tgt(inputTargetID)))
		ev := <-resultChan
		require.NotNil(s.T(), ev)
		require.NotNil(s.T(), ev.Target)
		require.Equal(s.T(), inputTargetID, ev.Target.ID)
		require.NoError(s.T(), ev.Err)
	}
	stepRunner.Stop()
	checkStoppedSuccessfully(s.T(), resultChan)
}

func (s *StepRunnerSuite) TestAddTargetReturnsErrorIfFailsToInput() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
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
		func(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
			<-hangCh
			for range ch.In {
				require.Fail(s.T(), "unexpected input")
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

	resultChan, addTarget, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
		emitter,
		nil,
		nil,
	)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resultChan)

	s.Run("input_context_cancelled", func() {
		cancelCtx, cncl := xcontext.WithCancel(ctx)
		cncl()
		require.Error(s.T(), addTarget(cancelCtx, tgt(inputTargetID)))
	})

	s.Run("sopped_during_input", func() {
		go func() {
			<-time.After(time.Millisecond)
			stepRunner.Stop()
		}()
		require.Error(s.T(), addTarget(ctx, tgt(inputTargetID)))
	})

	close(hangCh)
	stepRunner.Stop()
	checkStoppedSuccessfully(s.T(), resultChan)
}

func (s *StepRunnerSuite) TestStepPanics() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
			panic("panic")
		},
		nil,
	)
	require.NoError(s.T(), err)

	stepRunner := NewStepRunner()
	require.NotNil(s.T(), stepRunner)
	defer stepRunner.Stop()

	resultChan, addTarget, err := stepRunner.Run(ctx,
		s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
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
	require.NotNil(s.T(), resultChan)

	// some of AddTarget may succeed as it takes some time for a step to panic
	var gotError error
	for i := 0; i < 10; i++ {
		time.Sleep(time.Millisecond)
		if err := addTarget(ctx, tgt("target-id")); err != nil {
			gotError = err
		}
	}
	var expectedErrType *cerrors.ErrTestStepPaniced
	require.ErrorAs(s.T(), gotError, &expectedErrType)

	ev := <-resultChan
	require.NotNil(s.T(), ev)
	require.Nil(s.T(), ev.Target)

	require.ErrorAs(s.T(), ev.Err, &expectedErrType)
	_, ok := <-resultChan
	require.False(s.T(), ok)
}

func (s *StepRunnerSuite) TestCornerCases() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	err := s.RegisterStateFullStep(
		func(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
			_, err := teststeps.ForEachTarget(stateFullStepName, ctx, ch, func(ctx xcontext.Context, target *target.Target) error {
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

		resultChan, addTarget, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), resultChan)

		stepRunner.Stop()
		require.Error(s.T(), addTarget(ctx, tgt("dummy_target")))
		checkStoppedSuccessfully(s.T(), resultChan)
	})

	s.Run("run_twice", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		resultChan, _, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), resultChan)

		resultChan2, _, err2 := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			emitter,
			nil,
			nil,
		)
		require.Error(s.T(), err2)
		require.Nil(s.T(), resultChan2)
	})

	s.Run("stop_twice", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		resultChan, _, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), resultChan)

		stepRunner.Stop()
		stepRunner.Stop()
		checkStoppedSuccessfully(s.T(), resultChan)
	})

	s.Run("stop_before_run", func() {
		stepRunner := NewStepRunner()
		require.NotNil(s.T(), stepRunner)
		defer stepRunner.Stop()

		stepRunner.Stop()
		resultChan, _, err := stepRunner.Run(ctx,
			s.NewStep(ctx, "test_step_label", stateFullStepName, nil),
			emitter,
			nil,
			nil,
		)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), resultChan)
		checkStoppedSuccessfully(s.T(), resultChan)
	})
}
