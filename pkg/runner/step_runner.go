package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/linuxboot/contest/pkg/cerrors"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type OnTargetResult func(tgt *target.Target, res error)
type OnStepRunnerStopped func(err error)

type StepResult struct {
	Err         error
	ResumeState json.RawMessage
}

type StepRunner struct {
	mu sync.Mutex

	stepIn          chan *target.Target
	reportedTargets map[string]struct{}

	started           bool
	runningLoopActive bool
	stopOnce          sync.Once
	finishedCh        chan struct{}

	resultErr         error
	resultResumeState json.RawMessage
	stopCallback      OnStepRunnerStopped
}

func (sr *StepRunner) AddTarget(ctx xcontext.Context, tgt *target.Target) error {
	select {
	case sr.stepIn <- tgt:
	case <-ctx.Until(xcontext.ErrPaused):
		return xcontext.ErrPaused
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (sr *StepRunner) Run(
	ctx xcontext.Context,
	bundle test.TestStepBundle,
	ev testevent.Emitter,
	resumeState json.RawMessage,
	targetCallback OnTargetResult,
	stopCallback OnStepRunnerStopped,
) error {
	if targetCallback == nil {
		return fmt.Errorf("target callback should not be nil")
	}

	err := func() error {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		if sr.started {
			return &cerrors.ErrAlreadyDone{}
		}
		sr.started = true
		return nil
	}()
	if err != nil {
		return err
	}

	srCtx, _ := xcontext.WithCancel(ctx)
	sr.stopCallback = stopCallback

	var activeLoopsCount int32 = 2
	finish := func() {
		if atomic.AddInt32(&activeLoopsCount, -1) != 0 {
			return
		}
		sr.mu.Lock()
		defer sr.mu.Unlock()

		close(sr.finishedCh)
		sr.finishedCh = nil

		// if an error occurred, this callback was invoked early
		sr.notifyStoppedLocked(nil)
		srCtx.Debugf("StepRunner finished")
	}

	stepOut := make(chan test.TestStepResult)
	go func() {
		defer finish()
		sr.runningLoop(ctx, stepOut, bundle, ev, resumeState)
		srCtx.Debugf("Running loop finished")
	}()

	go func() {
		defer finish()
		sr.readingLoop(ctx, stepOut, bundle.TestStepLabel, targetCallback)
		srCtx.Debugf("Reading loop finished")
	}()
	return nil
}

func (sr *StepRunner) Started() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.started
}

func (sr *StepRunner) Running() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.started && sr.finishedCh != nil
}

// WaitResults returns TestStep.Run() output
// It returns an error if and only if waiting was terminated by input ctx argument and returns ctx.Err()
func (sr *StepRunner) WaitResults(ctx context.Context) (stepResult StepResult, err error) {
	sr.mu.Lock()
	resultErr := sr.resultErr
	resultResumeState := sr.resultResumeState
	finishedCh := sr.finishedCh
	sr.mu.Unlock()

	// StepRunner either finished with error or behaved incorrectly
	// it makes no sense to wait while it finishes, return what we have
	if resultErr != nil {
		return StepResult{
			Err:         resultErr,
			ResumeState: resultResumeState,
		}, nil
	}

	if finishedCh != nil {
		select {
		case <-ctx.Done():
			return StepResult{}, ctx.Err()
		case <-finishedCh:
		}
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()
	return StepResult{
		Err:         resultErr,
		ResumeState: resultResumeState,
	}, nil
}

// Stop triggers TestStep to stop running by closing input channel
func (sr *StepRunner) Stop() {
	sr.stopOnce.Do(func() {
		close(sr.stepIn)
	})
}

func (sr *StepRunner) readingLoop(
	ctx xcontext.Context,
	stepOut chan test.TestStepResult,
	testStepLabel string,
	targetCallback OnTargetResult,
) {

	cancelCh := ctx.Done()
	for {
		select {
		case res, ok := <-stepOut:
			if !ok {
				ctx.Debugf("Output channel closed")

				sr.mu.Lock()
				if sr.runningLoopActive {
					// This means that plugin closed its channels before leaving.
					sr.setErrLocked(ctx, &cerrors.ErrTestStepClosedChannels{StepName: testStepLabel})
				}
				sr.mu.Unlock()
				return
			}

			if res.Target == nil {
				sr.setErr(ctx, &cerrors.ErrTestStepReturnedNoTarget{StepName: testStepLabel})
				return
			}
			ctx.Infof("Obtained '%v' for target '%s'", res, res.Target.ID)

			sr.mu.Lock()
			_, found := sr.reportedTargets[res.Target.ID]
			sr.reportedTargets[res.Target.ID] = struct{}{}
			sr.mu.Unlock()

			if found {
				sr.setErr(ctx, &cerrors.ErrTestStepReturnedDuplicateResult{StepName: testStepLabel, Target: res.Target.ID})
				return
			}
			targetCallback(res.Target, res.Err)

		case <-cancelCh:
			ctx.Debugf("reading loop detected context canceled")
			return
		}
	}
}

func (sr *StepRunner) runningLoop(
	ctx xcontext.Context,
	stepOut chan test.TestStepResult,
	bundle test.TestStepBundle,
	ev testevent.Emitter,
	resumeState json.RawMessage,
) {
	defer func() {
		sr.mu.Lock()
		sr.runningLoopActive = false
		sr.mu.Unlock()

		if recoverOccurred := safeCloseOutCh(stepOut); recoverOccurred {
			sr.setErr(ctx, &cerrors.ErrTestStepClosedChannels{StepName: bundle.TestStepLabel})
		}
		ctx.Debugf("output channel closed")
	}()

	sr.mu.Lock()
	sr.runningLoopActive = true
	sr.mu.Unlock()

	resultResumeState, err := func() (json.RawMessage, error) {
		defer func() {
			if r := recover(); r != nil {
				sr.mu.Lock()
				sr.setErrLocked(ctx, &cerrors.ErrTestStepPaniced{
					StepName:   bundle.TestStepLabel,
					StackTrace: fmt.Sprintf("%s / %s", r, debug.Stack()),
				})
				sr.mu.Unlock()
			}
		}()

		inChannels := test.TestStepChannels{In: sr.stepIn, Out: stepOut}
		return bundle.TestStep.Run(ctx, inChannels, bundle.Parameters, ev, resumeState)
	}()
	ctx.Debugf("TestStep finished '%v', rs %s", err, string(resultResumeState))

	sr.mu.Lock()
	sr.setErrLocked(ctx, err)
	sr.resultResumeState = resultResumeState
	sr.mu.Unlock()
}

// setErr sets step runner error unless already set.
func (sr *StepRunner) setErr(ctx xcontext.Context, err error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.setErrLocked(ctx, err)
}

func (sr *StepRunner) setErrLocked(ctx xcontext.Context, err error) {
	if err == nil || sr.resultErr != nil {
		return
	}
	ctx.Errorf("err: %v", err)
	sr.resultErr = err
	sr.notifyStoppedLocked(sr.resultErr)
}

func (sr *StepRunner) notifyStoppedLocked(err error) {
	if sr.stopCallback == nil {
		return
	}
	stopCallback := sr.stopCallback
	sr.stopCallback = nil

	go func() {
		stopCallback(err)
	}()
}

func safeCloseOutCh(ch chan test.TestStepResult) (recoverOccurred bool) {
	recoverOccurred = false
	defer func() {
		if r := recover(); r != nil {
			recoverOccurred = true
		}
	}()
	close(ch)
	return
}

// NewStepRunner creates a new StepRunner object
func NewStepRunner() *StepRunner {
	return &StepRunner{
		stepIn:            make(chan *target.Target),
		reportedTargets:   make(map[string]struct{}),
		runningLoopActive: true,
		finishedCh:        make(chan struct{}),
	}
}
