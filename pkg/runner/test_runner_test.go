// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package runner

import (
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/linuxboot/contest/pkg/cerrors"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
	"github.com/linuxboot/contest/tests/common"
	"github.com/linuxboot/contest/tests/common/goroutine_leak_check"
	"github.com/linuxboot/contest/tests/plugins/teststeps/badtargets"
	"github.com/linuxboot/contest/tests/plugins/teststeps/channels"
	"github.com/linuxboot/contest/tests/plugins/teststeps/hanging"
	"github.com/linuxboot/contest/tests/plugins/teststeps/noreturn"
	"github.com/linuxboot/contest/tests/plugins/teststeps/panicstep"
	"github.com/linuxboot/contest/tests/plugins/teststeps/teststep"
)

const (
	testName        = "SimpleTest"
	shutdownTimeout = 3 * time.Second
)

func TestMain(m *testing.M) {
	flag.Parse()
	goroutine_leak_check.LeakCheckingTestMain(m,
		// We expect these to leak.
		"github.com/linuxboot/contest/tests/plugins/teststeps/hanging.(*hanging).Run",
		"github.com/linuxboot/contest/tests/plugins/teststeps/noreturn.(*noreturnStep).Run",

		// No leak in contexts checked with itsown unit-tests
		"github.com/linuxboot/contest/pkg/xcontext.(*ctxValue).cloneWithStdContext.func2",
	)
}

func newTestRunner() *TestRunner {
	return NewTestRunnerWithTimeouts(shutdownTimeout)
}

func tgt(id string) *target.Target {
	return &target.Target{ID: id}
}

type runRes struct {
	resume         []byte
	targetsResults map[string]error
	err            error
}

type TestRunnerSuite struct {
	BaseTestSuite
}

func TestTestRunnerSuite(t *testing.T) {
	suite.Run(t, new(TestRunnerSuite))
}

func (s *TestRunnerSuite) SetupTest() {
	s.BaseTestSuite.SetupTest()

	for _, e := range []struct {
		name    string
		factory test.TestStepFactory
		events  []event.Name
	}{
		{badtargets.Name, badtargets.New, badtargets.Events},
		{channels.Name, channels.New, channels.Events},
		{hanging.Name, hanging.New, hanging.Events},
		{noreturn.Name, noreturn.New, noreturn.Events},
		{panicstep.Name, panicstep.New, panicstep.Events},
		{teststep.Name, teststep.New, teststep.Events},
	} {
		if err := s.PluginRegistry.RegisterTestStep(e.name, e.factory, e.events); err != nil {
			panic(fmt.Sprintf("could not register TestStep: %v", err))
		}
	}
}

func (s *TestRunnerSuite) newTestStep(ctx xcontext.Context, label string, failPct int, failTargets string, delayTargets string) test.TestStepBundle {
	return s.NewStep(ctx, label, teststep.Name, test.TestStepParameters{
		teststep.FailPctParam:      []test.Param{*test.NewParam(fmt.Sprintf("%d", failPct))},
		teststep.FailTargetsParam:  []test.Param{*test.NewParam(failTargets)},
		teststep.DelayTargetsParam: []test.Param{*test.NewParam(delayTargets)},
	})
}

func (s *TestRunnerSuite) runWithTimeout(ctx xcontext.Context,
	tr *TestRunner,
	resumeState []byte,
	runID types.RunID,
	timeout time.Duration,
	targets []*target.Target,
	bundles []test.TestStepBundle,
) ([]byte, map[string]error, error) {
	newCtx, cancel := xcontext.WithCancel(ctx)
	test := &test.Test{
		Name:             testName,
		TestStepsBundles: bundles,
	}
	emitterFactory := NewTestStepEventsEmitterFactory(s.MemoryStorage.StorageEngineVault, 1, runID, test.Name, 0)
	resCh := make(chan runRes)
	go func() {
		res, targetsResults, err := tr.Run(newCtx, test, targets, emitterFactory, resumeState)
		resCh <- runRes{resume: res, targetsResults: targetsResults, err: err}
	}()
	var res runRes
	select {
	case res = <-resCh:
	case <-time.After(timeout):
		cancel()
		assert.FailNow(s.T(), "TestRunner should not time out")
	}
	return res.resume, res.targetsResults, res.err
}

// Simple case: one target, one step, success.
func (s *TestRunnerSuite) Test1Step1Success() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, targetsResults, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1")},
		[]test.TestStepBundle{
			s.newTestStep(ctx, "Step 1", 0, "", ""),
		},
	)
	require.NoError(s.T(), err)
	require.Equal(s.T(), map[string]error{
		"T1": nil,
	}, targetsResults)

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, ""))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetOut]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
}

// Simple case: one target, one step that blocks for a bit, success.
// We block for longer than the shutdown timeout of the test runner.
func (s *TestRunnerSuite) Test1StepLongerThanShutdown1Success() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := NewTestRunnerWithTimeouts(100 * time.Millisecond)
	_, targetsResults, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1")},
		[]test.TestStepBundle{
			s.newTestStep(ctx, "Step 1", 0, "", "T1=500"),
		},
	)
	require.NoError(s.T(), err)
	require.Equal(s.T(), map[string]error{
		"T1": nil,
	}, targetsResults)

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, ""))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetOut]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
}

// Simple case: one target, one step, failure.
func (s *TestRunnerSuite) Test1Step1Fail() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, targetsResults, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1")},
		[]test.TestStepBundle{
			s.newTestStep(ctx, "Step 1", 100, "", ""),
		},
	)
	require.NoError(s.T(), err)
	require.Len(s.T(), targetsResults, 1)
	require.Error(s.T(), targetsResults["T1"])

	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestFailedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetErr &"{\"Error\":\"target failed\"}"]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
}

// One step pipeline with two targets - one fails, one succeeds.
func (s *TestRunnerSuite) Test1Step1Success1Fail() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1"), tgt("T2")},
		[]test.TestStepBundle{
			s.newTestStep(ctx, "Step 1", 0, "T1", "T2=100"),
		},
	)
	require.NoError(s.T(), err)
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, ""))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestFailedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetErr &"{\"Error\":\"target failed\"}"]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TargetOut]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T2"))
}

// Three-step pipeline, two targets: T1 fails at step 1, T2 fails at step 2,
// step 3 is not reached and not even run.
func (s *TestRunnerSuite) Test3StepsNotReachedStepNotRun() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1"), tgt("T2")},
		[]test.TestStepBundle{
			s.newTestStep(ctx, "Step 1", 0, "T1", ""),
			s.newTestStep(ctx, "Step 2", 0, "T2", ""),
			s.newTestStep(ctx, "Step 3", 0, "", ""),
		},
	)
	require.NoError(s.T(), err)
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 2][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 2][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 2"))
	require.Equal(s.T(), "\n\n", s.MemoryStorage.GetStepEvents(ctx, testName, "Step 3"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestFailedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetErr &"{\"Error\":\"target failed\"}"]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TargetOut]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TargetIn]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TestFailedEvent]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TargetErr &"{\"Error\":\"target failed\"}"]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T2"))
}

// A misbehaving step that fails to shut down properly after processing targets
// and does not return.
func (s *TestRunnerSuite) TestNoReturnStepWithCorrectTargetForwarding() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := NewTestRunnerWithTimeouts(200 * time.Millisecond)
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1")},
		[]test.TestStepBundle{
			s.NewStep(ctx, "Step 1", noreturn.Name, nil),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepsNeverReturned{}, err)
	require.Contains(s.T(), s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"), "step [Step 1] did not return")
}

// A misbehaving step that panics.
func (s *TestRunnerSuite) TestStepPanics() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1")},
		[]test.TestStepBundle{
			s.NewStep(ctx, "Step 1", panicstep.Name, nil),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepPaniced{}, err)
	require.Equal(s.T(), "\n\n", s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
	require.Contains(s.T(), s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"), "step Step 1 paniced")
}

// A misbehaving step that closes its output channel.
func (s *TestRunnerSuite) TestStepClosesChannels() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("T1")},
		[]test.TestStepBundle{
			s.NewStep(ctx, "Step 1", channels.Name, nil),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepClosedChannels{}, err)
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetOut]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestError &"\"test step Step 1 closed output channels (api violation)\""]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"))
}

// A misbehaving step that yields a result for a target that does not exist.
func (s *TestRunnerSuite) TestStepYieldsResultForNonexistentTarget() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("TExtra")},
		[]test.TestStepBundle{
			s.NewStep(ctx, "Step 1", badtargets.Name, nil),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepReturnedUnexpectedResult{}, err)
	require.Equal(s.T(), "\n\n", s.MemoryStorage.GetTargetEvents(ctx, testName, "TExtra2"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestError &"\"test step Step 1 returned unexpected result for TExtra2\""]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"))
}

// A misbehaving step that yields a duplicate result for a target.
func (s *TestRunnerSuite) TestStepYieldsDuplicateResult() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("TGood"), tgt("TDup")},
		[]test.TestStepBundle{
			// TGood makes it past here unscathed and gets delayed in Step 2,
			// TDup also emerges fine at first but is then returned again, and that's bad.
			s.NewStep(ctx, "Step 1", badtargets.Name, nil),
			s.newTestStep(ctx, "Step 2", 0, "", "TGood=100"),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepReturnedDuplicateResult{}, err)
}

// A misbehaving step that loses targets.
func (s *TestRunnerSuite) TestStepLosesTargets() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("TGood"), tgt("TDrop")},
		[]test.TestStepBundle{
			s.NewStep(ctx, "Step 1", badtargets.Name, nil),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepLostTargets{}, err)
	require.Contains(s.T(), err.Error(), "TDrop")
}

// A misbehaving step that yields a result for a target that does exist
// but is not currently waiting for it.
func (s *TestRunnerSuite) TestStepYieldsResultForUnexpectedTarget() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		[]*target.Target{tgt("TExtra"), tgt("TExtra2")},
		[]test.TestStepBundle{
			// TExtra2 fails here.
			s.newTestStep(ctx, "Step 1", 0, "TExtra2", ""),
			// Yet, a result for it is returned here, which we did not expect.
			s.NewStep(ctx, "Step 2", badtargets.Name, nil),
		},
	)
	require.Error(s.T(), err)
	require.IsType(s.T(), &cerrors.ErrTestStepReturnedUnexpectedResult{}, err)
}

// Larger, randomized test - a number of steps, some targets failing, some succeeding.
func (s *TestRunnerSuite) TestRandomizedMultiStep() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	tr := newTestRunner()
	var targets []*target.Target
	for i := 1; i <= 100; i++ {
		targets = append(targets, tgt(fmt.Sprintf("T%d", i)))
	}
	_, _, err := s.runWithTimeout(ctx, tr, nil, 1, 2*time.Second,
		targets,
		[]test.TestStepBundle{
			s.newTestStep(ctx, "Step 1", 0, "", "*=10"),  // All targets pass the first step, with a slight delay
			s.newTestStep(ctx, "Step 2", 25, "", ""),     // 25% don't make it past the second step
			s.newTestStep(ctx, "Step 3", 25, "", "*=10"), // Another 25% fail at the third step
		},
	)
	require.NoError(s.T(), err)
	// Every target mush have started and finished the first step.
	numFinished := 0
	for _, tgt := range targets {
		s1n := "Step 1"
		require.Equal(s.T(), fmt.Sprintf(`
{[1 1 SimpleTest 0 Step 1][Target{ID: "%s"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "%s"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "%s"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "%s"} TargetOut]}
`, tgt.ID, tgt.ID, tgt.ID, tgt.ID),
			common.GetTestEventsAsString(ctx, s.MemoryStorage.Storage, testName, &tgt.ID, &s1n))
		s3n := "Step 3"
		if strings.Contains(common.GetTestEventsAsString(ctx, s.MemoryStorage.Storage, testName, &tgt.ID, &s3n), "TestFinishedEvent") {
			numFinished++
		}
	}
	// At least some must have finished.
	require.Greater(s.T(), numFinished, 0)
}

// Test pausing/resuming a naive step that does not cooperate.
// In this case we drain input, wait for all targets to emerge and exit gracefully.
func (s *TestRunnerSuite) TestPauseResumeSimple() {
	ctx, cancel := xcontext.WithCancel(logrusctx.NewContext(logger.LevelDebug))
	defer cancel()

	var err error
	var resumeState []byte
	targets := []*target.Target{tgt("T1"), tgt("T2"), tgt("T3")}
	steps := []test.TestStepBundle{
		s.newTestStep(ctx, "Step 1", 0, "T1", ""),
		// T2 and T3 will be paused here, the step will be given time to finish.
		s.newTestStep(ctx, "Step 2", 0, "", "T2=200,T3=200"),
		s.newTestStep(ctx, "Step 3", 0, "", ""),
	}
	{
		tr1 := newTestRunner()
		ctx1, pause := xcontext.WithNotify(ctx, xcontext.ErrPaused)
		ctx1, cancel := xcontext.WithCancel(ctx1)
		defer cancel()
		go func() {
			time.Sleep(100 * time.Millisecond)
			ctx.Infof("TestPauseResumeNaive: pausing")
			pause()
		}()
		resumeState, _, err = s.runWithTimeout(ctx1, tr1, nil, 1, 2*time.Second, targets, steps)
		require.Error(s.T(), err)
		require.IsType(s.T(), xcontext.ErrPaused, err)
		require.NotNil(s.T(), resumeState)
	}
	ctx.Debugf("Resume state: %s", string(resumeState))
	// Make sure that resume state is validated.
	{
		tr := newTestRunner()
		ctx, cancel := xcontext.WithCancel(ctx)
		defer cancel()
		resumeState2, _, err := s.runWithTimeout(
			ctx, tr, []byte("FOO"), 2, 2*time.Second, targets, steps)
		require.Error(s.T(), err)
		require.Contains(s.T(), err.Error(), "invalid resume state")
		require.Nil(s.T(), resumeState2)
	}
	{
		tr := newTestRunner()
		ctx, cancel := xcontext.WithCancel(ctx)
		defer cancel()
		resumeState2 := strings.Replace(string(resumeState), `"V"`, `"XV"`, 1)
		_, _, err := s.runWithTimeout(
			ctx, tr, []byte(resumeState2), 3, 2*time.Second, targets, steps)
		require.Error(s.T(), err)
		require.Contains(s.T(), err.Error(), "incompatible resume state")
	}

	// Finally, resume and finish the job.
	{
		tr2 := newTestRunner()
		ctx2, cancel := xcontext.WithCancel(ctx)
		defer cancel()
		_, _, err := s.runWithTimeout(ctx2, tr2, resumeState, 5, 2*time.Second,
			// Pass exactly the same targets and pipeline to resume properly.
			// Don't use the same pointers ot make sure there is no reliance on that.
			[]*target.Target{tgt("T1"), tgt("T2"), tgt("T3")},
			[]test.TestStepBundle{
				s.newTestStep(ctx, "Step 1", 0, "T1", ""),
				s.newTestStep(ctx, "Step 2", 0, "", "T2=200,T3=200"),
				s.newTestStep(ctx, "Step 3", 0, "", ""),
			},
		)
		require.NoError(s.T(), err)
	}
	// Verify step events.
	// Steps 1 and 2 are executed entirely within the first runner instance
	// and never started in the second.
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 1][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 1"))
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 2][(*Target)(nil) TestStepRunningEvent]}
{[1 1 SimpleTest 0 Step 2][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 2"))
	// Step 3 did not get to start in the first instance and ran in the second.
	require.Equal(s.T(), `
{[1 5 SimpleTest 0 Step 3][(*Target)(nil) TestStepRunningEvent]}
{[1 5 SimpleTest 0 Step 3][(*Target)(nil) TestStepFinishedEvent]}
`, s.MemoryStorage.GetStepEvents(ctx, testName, "Step 3"))
	// T1 failed entirely within the first run.
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TestFailedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T1"} TargetErr &"{\"Error\":\"target failed\"}"]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T1"))
	// T2 and T3 ran in both.
	require.Equal(s.T(), `
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TargetIn]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 1][Target{ID: "T2"} TargetOut]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TargetIn]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TestStartedEvent]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TestFinishedEvent]}
{[1 1 SimpleTest 0 Step 2][Target{ID: "T2"} TargetOut]}
{[1 5 SimpleTest 0 Step 3][Target{ID: "T2"} TargetIn]}
{[1 5 SimpleTest 0 Step 3][Target{ID: "T2"} TestStartedEvent]}
{[1 5 SimpleTest 0 Step 3][Target{ID: "T2"} TestFinishedEvent]}
{[1 5 SimpleTest 0 Step 3][Target{ID: "T2"} TargetOut]}
`, s.MemoryStorage.GetTargetEvents(ctx, testName, "T2"))
}
