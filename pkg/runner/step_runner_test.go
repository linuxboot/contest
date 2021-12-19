package runner

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

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

	require.NoError(s.T(), addTarget(tgt("TSucc")))
	ev, ok := <-resultChan
	require.True(s.T(), ok)
	require.Equal(s.T(), tgt("TSucc"), ev.Target)
	require.NoError(s.T(), ev.Err)

	require.NoError(s.T(), addTarget(tgt("TFail")))
	ev, ok = <-resultChan
	require.True(s.T(), ok)
	require.Equal(s.T(), tgt("TFail"), ev.Target)
	require.Error(s.T(), ev.Err)

	stepRunner.Stop()

	ev, ok = <-resultChan
	require.True(s.T(), ok)
	require.Nil(s.T(), ev.Target)
	require.NoError(s.T(), ev.Err)

	ev, ok = <-resultChan
	require.False(s.T(), ok)

	closedCtx, closedCtxCancel := xcontext.WithCancel(ctx)
	closedCtxCancel()

	// if step runner has results, it should return them even if input context is closed
	res, err := stepRunner.WaitResults(closedCtx)
	require.NoError(s.T(), err)

	require.Equal(s.T(), json.RawMessage("{\"output\": true}"), res.ResumeState)
	require.NoError(s.T(), res.Err)

	require.Equal(s.T(), inputResumeState, obtainedResumeState)
}
