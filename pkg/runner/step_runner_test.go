package runner

import (
	"encoding/json"
	"fmt"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/plugins/teststeps/echo"
	"sync"
	"testing"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
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

func (s *StepRunnerSuite) SetupTest() {
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

func (s *StepRunnerSuite) TestRunningStep() {
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
	resultChan, err := stepRunner.Run(s.Ctx, s.NewStep("test_step_label", stateFullStepName, nil), emitter, inputResumeState)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resultChan)

	require.NoError(s.T(), stepRunner.AddTarget(s.Ctx, tgt("TSucc")))
	ev, ok := <-resultChan
	require.True(s.T(), ok)
	require.Equal(s.T(), tgt("TSucc"), ev.Target)
	require.NoError(s.T(), ev.Err)

	require.NoError(s.T(), stepRunner.AddTarget(s.Ctx, tgt("TFail")))
	ev, ok = <-resultChan
	require.True(s.T(), ok)
	require.Equal(s.T(), tgt("TFail"), ev.Target)
	require.Error(s.T(), ev.Err)
	require.True(s.T(), stepRunner.Started())
	require.True(s.T(), stepRunner.Running())
	stepRunner.Stop()

	ev, ok = <-resultChan
	require.True(s.T(), ok)
	require.Nil(s.T(), ev.Target)
	require.NoError(s.T(), ev.Err)

	ev, ok = <-resultChan
	require.False(s.T(), ok)

	closedCtx, cancel := xcontext.WithCancel(s.Ctx)
	cancel()

	// if step runner has results, it should return them even if input context is closed
	res, err := stepRunner.WaitResults(closedCtx)
	require.NoError(s.T(), err)

	require.Equal(s.T(), json.RawMessage("{\"output\": true}"), res.ResumeState)
	require.NoError(s.T(), res.Err)

	require.Equal(s.T(), inputResumeState, obtainedResumeState)
}

func (s *StepRunnerSuite) TestRunningStepTwice() {
	stepRunner := NewStepRunner()
	require.NotNil(s.T(), stepRunner)

	emitterFactory := NewTestStepEventsEmitterFactory(s.MemoryStorage.StorageEngineVault, 1, 1, testName, 0)
	emitter := emitterFactory.New("test_step_label")

	checkResultChan := func(resultChan <-chan StepRunnerEvent, targetsIDs ...string) {
		for _, targetID := range targetsIDs {
			ev := <-resultChan
			require.Equal(s.T(), targetID, ev.Target.ID)
			require.NoError(s.T(), ev.Err)
		}

		ev := <-resultChan
		require.Nil(s.T(), ev.Target)
		require.NoError(s.T(), ev.Err)

		ev, ok := <-resultChan
		require.False(s.T(), ok)
	}

	params := test.TestStepParameters{
		"text": []test.Param{*test.NewParam("Hello world")},
	}
	resultChan, err := stepRunner.Run(s.Ctx, s.NewStep("test_step_label", echo.Name, params), emitter, nil)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resultChan)

	require.NoError(s.T(), stepRunner.AddTarget(s.Ctx, tgt("SomeTarget")))
	require.True(s.T(), stepRunner.Started())
	require.True(s.T(), stepRunner.Running())
	stepRunner.Stop()
	checkResultChan(resultChan, "SomeTarget")

	require.True(s.T(), stepRunner.Started())
	require.False(s.T(), stepRunner.Running())

	resultChan, err = stepRunner.Run(s.Ctx, s.NewStep("test_step_label", echo.Name, params), emitter, nil)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resultChan)

	require.NoError(s.T(), stepRunner.AddTarget(s.Ctx, tgt("SomeTarget2")))
	require.NoError(s.T(), stepRunner.AddTarget(s.Ctx, tgt("SomeTarget3")))
	stepRunner.Stop()
	checkResultChan(resultChan, "SomeTarget2", "SomeTarget3")
}