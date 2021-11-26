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

type StepRunner struct {
	ctx    xcontext.Context
	cancel context.CancelFunc
	mu     sync.Mutex

	targetCallback OnTargetResult
	stopCallback   OnStepRunnerStopped

	stepIn          chan *target.Target
	reportedTargets map[string]struct{}

	started           uint32
	runningLoopActive bool
	stopOnce          sync.Once
	finishedCh        chan struct{}

	resultErr         error
	resultResumeState json.RawMessage
}

func (sr *StepRunner) AddTarget(tgt *target.Target) error {
	select {
	case sr.stepIn <- tgt:
	case <-sr.ctx.Until(xcontext.ErrPaused):
		return xcontext.ErrPaused
	case <-sr.ctx.Done():
		return sr.ctx.Err()
	}
	return nil
}

func (sr *StepRunner) Run(
	bundle test.TestStepBundle,
	ev testevent.Emitter,
	resumeState json.RawMessage,
) {
	if !atomic.CompareAndSwapUint32(&sr.started, 0, 1) {
		return
	}

	var activeLoopsCount int32 = 2
	onFinished := func() {
		if atomic.AddInt32(&activeLoopsCount, -1) != 0 {
			return
		}
		sr.mu.Lock()
		defer sr.mu.Unlock()

		close(sr.finishedCh)
		sr.finishedCh = nil

		// if an error occurred, this callback was invoked early
		sr.notifyStoppedLocked(nil)
		sr.ctx.Debugf("StepRunner finished")
	}

	stepOut := make(chan test.TestStepResult)
	go func() {
		defer onFinished()
		sr.runningLoop(stepOut, bundle, ev, resumeState)
		sr.ctx.Debugf("Running loop finished")
	}()

	go func() {
		defer onFinished()
		sr.readingLoop(stepOut, bundle.TestStepLabel)
		sr.ctx.Debugf("Reading loop finished")
	}()
}

func (sr *StepRunner) Started() bool {
	return atomic.LoadUint32(&sr.started) == 1
}

func (sr *StepRunner) Running() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.Started() && sr.finishedCh != nil
}

func (sr *StepRunner) WaitResults(ctx context.Context) (json.RawMessage, error) {
	sr.mu.Lock()
	resultErr := sr.resultErr
	resultResumeState := sr.resultResumeState
	stepFinished := sr.finishedCh
	sr.mu.Unlock()

	if resultErr != nil {
		return resultResumeState, resultErr
	}

	if stepFinished != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-stepFinished:
		}
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.resultResumeState, sr.resultErr
}

// Stop triggers TestStep to stop running by closing input channel
func (sr *StepRunner) Stop() {
	sr.stopOnce.Do(func() {
		close(sr.stepIn)
		sr.ctx.Debugf("Input channel was closed")
	})
}

func (sr *StepRunner) targetCallbackPanicSafe(tgt *target.Target, res error) {
	defer func() {
		if r := recover(); r != nil {
			sr.ctx.Errorf("Callback panic, stack: %s", debug.Stack())
		}
	}()
	sr.targetCallback(tgt, res)
}

func (sr *StepRunner) readingLoop(stepOut chan test.TestStepResult, testStepLabel string) {
	for {
		select {
		case res, ok := <-stepOut:
			if !ok {
				sr.ctx.Debugf("Output channel closed")

				sr.mu.Lock()
				if sr.runningLoopActive {
					// This means that plugin closed its channels before leaving.
					sr.setErrLocked(&cerrors.ErrTestStepClosedChannels{StepName: testStepLabel})
				}
				sr.mu.Unlock()
				return
			}

			if res.Target == nil {
				sr.setErr(&cerrors.ErrTestStepReturnedNoTarget{StepName: testStepLabel})
				return
			}
			sr.ctx.Infof("Obtained '%v' for target '%s'", res, res.Target.ID)

			sr.mu.Lock()
			_, found := sr.reportedTargets[res.Target.ID]
			sr.reportedTargets[res.Target.ID] = struct{}{}
			sr.mu.Unlock()

			if found {
				sr.setErr(&cerrors.ErrTestStepReturnedDuplicateResult{StepName: testStepLabel, Target: res.Target.ID})
				return
			}
			sr.targetCallbackPanicSafe(res.Target, res.Err)

		case <-sr.ctx.Done():
			sr.ctx.Debugf("canceled readingLoop")
			return
		}
	}
}

func (sr *StepRunner) runningLoop(
	stepOut chan test.TestStepResult,
	bundle test.TestStepBundle, ev testevent.Emitter, resumeState json.RawMessage,
) {
	defer func() {
		sr.mu.Lock()
		sr.runningLoopActive = false
		sr.mu.Unlock()

		if recoverOccurred := safeCloseOutCh(stepOut); recoverOccurred {
			sr.setErr(&cerrors.ErrTestStepClosedChannels{StepName: bundle.TestStepLabel})
		}
		sr.ctx.Debugf("output channel closed")
	}()

	defer func() {
		if r := recover(); r != nil {
			sr.mu.Lock()
			sr.setErrLocked(&cerrors.ErrTestStepPaniced{
				StepName:   bundle.TestStepLabel,
				StackTrace: fmt.Sprintf("%s / %s", r, debug.Stack()),
			})
			sr.mu.Unlock()
		}
	}()

	sr.mu.Lock()
	sr.runningLoopActive = true
	sr.mu.Unlock()

	inChannels := test.TestStepChannels{In: sr.stepIn, Out: stepOut}
	resultResumeState, err := bundle.TestStep.Run(sr.ctx, inChannels, bundle.Parameters, ev, resumeState)
	sr.ctx.Debugf("TestStep finished '%v', rs %s", err, string(resultResumeState))

	sr.mu.Lock()
	sr.setErrLocked(err)
	sr.resultResumeState = resultResumeState
	sr.mu.Unlock()
}

// setErr sets step runner error unless already set.
func (sr *StepRunner) setErr(err error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.setErrLocked(err)
}

func (sr *StepRunner) setErrLocked(err error) {
	if err == nil || sr.resultErr != nil {
		return
	}
	sr.ctx.Errorf("err: %v", err)
	sr.resultErr = err
	sr.notifyStoppedLocked(sr.resultErr)
}

func (sr *StepRunner) notifyStoppedLocked(err error) {
	sr.cancel()
	if sr.stopCallback == nil {
		return
	}
	stopCallback := sr.stopCallback
	sr.stopCallback = nil

	go func() {
		if r := recover(); r != nil {
			sr.ctx.Errorf("Stop callback panic, stack: %s", debug.Stack())
		}
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
func NewStepRunner(
	ctx xcontext.Context,
	targetCallback OnTargetResult,
	stoppedCallback OnStepRunnerStopped,
) *StepRunner {
	if targetCallback == nil {
		panic("target callback should not be nil")
	}
	stepIn := make(chan *target.Target)

	srCrx, cancel := xcontext.WithCancel(ctx)
	sr := &StepRunner{
		ctx:               srCrx,
		cancel:            cancel,
		stepIn:            stepIn,
		targetCallback:    targetCallback,
		stopCallback:      stoppedCallback,
		reportedTargets:   make(map[string]struct{}),
		runningLoopActive: true,
		finishedCh:        make(chan struct{}),
	}
	return sr
}
