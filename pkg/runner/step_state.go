package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// stepState contains state associated with one state of the pipeline in TestRunner.
type stepState struct {
	mu     sync.Mutex
	cancel xcontext.CancelFunc

	stepIndex int                 // Index of this step in the pipeline.
	sb        test.TestStepBundle // The test bundle.

	ev                 testevent.Emitter
	tsv                *testStepsVariables
	stepRunner         *StepRunner
	addTarget          AddTargetToStep
	leftTargetsCounter int // the number of targets that will be assigned to the step, when reaches 0 the stepRunner should be stopped
	stopped            chan struct{}

	resumeState            json.RawMessage         // Resume state passed to and returned by the Run method.
	resumeStateTargets     []target.Target         // Targets that were being processed during pause.
	resumeTargetsNotifiers map[string]ChanNotifier // resumeStateTargets targets results
	runErr                 error                   // Runner error, returned from Run() or an error condition detected by the reader.
	onError                func(err error)
}

func newStepState(
	stepIndex int,
	totalTargets int,
	sb test.TestStepBundle,
	emitter testevent.Emitter,
	tsv *testStepsVariables,
	resumeState json.RawMessage,
	resumeStateTargets []target.Target,
	onError func(err error),
) *stepState {
	return &stepState{
		stepIndex:          stepIndex,
		leftTargetsCounter: totalTargets,
		sb:                 sb,
		ev:                 emitter,
		tsv:                tsv,
		stepRunner:         NewStepRunner(),
		stopped:            make(chan struct{}),
		resumeState:        resumeState,
		resumeStateTargets: resumeStateTargets,
		onError:            onError,
	}
}

func (ss *stepState) Started() bool {
	return ss.stepRunner.Started()
}

func (ss *stepState) WaitResults(ctx context.Context) (stepResult StepResult, err error) {
	// should wait for stepState to process a possible error code from stepRunner
	select {
	case <-ctx.Done():
		// Give priority to success path
		select {
		case <-ss.stopped:
		default:
			return StepResult{}, ctx.Err()
		}
	case <-ss.stopped:
	}
	return ss.stepRunner.WaitResults(ctx)
}

// DecreaseLeftTargets stops step runner when there are no more targets left for it in the scope of a Test
func (ss *stepState) DecreaseLeftTargets() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.leftTargetsCounter--
	if ss.leftTargetsCounter < 0 {
		panic(fmt.Sprintf("leftTargetsCounter should be >= 0 but is '%d'", ss.leftTargetsCounter))
	}
	if ss.leftTargetsCounter == 0 {
		ss.stepRunner.Stop()
	}
}

func (ss *stepState) ForceStop() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.cancel != nil {
		ss.cancel()
	} else {
		close(ss.stopped)
	}
}

func (ss *stepState) GetInitResumeState() json.RawMessage {
	return ss.resumeState
}

func (ss *stepState) GetTestStepLabel() string {
	return ss.sb.TestStepLabel
}

func (ss *stepState) Run(ctx xcontext.Context) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	select {
	case <-ss.stopped:
		return fmt.Errorf("stopped")
	default:
	}

	if ss.stepRunner.Started() {
		return nil
	}

	stepCtx, cancel := xcontext.WithCancel(ctx)
	stepCtx = stepCtx.WithField("step_index", strconv.Itoa(ss.stepIndex))
	stepCtx = stepCtx.WithField("step_label", ss.sb.TestStepLabel)

	addTarget, resumeTargetsNotifiers, stepRunResult, err := ss.stepRunner.Run(
		stepCtx, ss.sb, newStepVariablesAccessor(ss.sb.TestStepLabel, ss.tsv), ss.ev, ss.resumeState,
		ss.resumeStateTargets,
	)
	if err != nil {
		return fmt.Errorf("failed to launch step runner: %v", err)
	}
	ss.cancel = cancel
	ss.addTarget = addTarget
	ss.resumeTargetsNotifiers = make(map[string]ChanNotifier)
	for i := 0; i < len(ss.resumeStateTargets); i++ {
		ss.resumeTargetsNotifiers[ss.resumeStateTargets[i].ID] = resumeTargetsNotifiers[i]
	}

	go func() {
		defer func() {
			close(ss.stopped)
			stepCtx.Debugf("StepRunner fully stopped")
		}()

		select {
		case stepErr := <-stepRunResult.NotifyCh():
			ss.SetError(stepCtx, stepErr)
		case <-stepCtx.Done():
			stepCtx.Debugf("Cancelled step context during waiting for step run result")
		}
	}()
	return nil
}

func (ss *stepState) InjectTarget(ctx xcontext.Context, tgt *target.Target) (ChanNotifier, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if !ss.stepRunner.Started() {
		return nil, fmt.Errorf("step was not started")
	}
	if notifier := ss.resumeTargetsNotifiers[tgt.ID]; notifier != nil {
		return notifier, nil
	}
	return ss.addTarget(ctx, tgt)
}

func (ss *stepState) NotifyStopped() <-chan struct{} {
	return ss.stopped
}

func (ss *stepState) GetError() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	return ss.runErr
}

func (ss *stepState) String() string {
	return fmt.Sprintf("[#%d %s]", ss.stepIndex, ss.sb.TestStepLabel)
}

func (ss *stepState) SetError(ctx xcontext.Context, runErr error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if runErr == nil || ss.runErr != nil {
		return
	}
	ctx.Errorf("Step '%s' failed with error: %v", ss.sb.TestStepLabel, runErr)
	ss.runErr = runErr

	if ss.runErr != xcontext.ErrPaused && ss.runErr != xcontext.ErrCanceled {
		if err := emitEvent(ctx, ss.ev, EventTestError, nil, runErr.Error()); err != nil {
			ctx.Errorf("failed to emit event: %s", err)
		}
	}

	// notify last as callback may use GetError or cancel context
	go func() {
		ss.onError(runErr)
	}()
}
