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

type OnTargetResult func(res error)
type OnStepRunnerStopped func(err error)

type StepRunner struct {
	ctx          xcontext.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	stopCallback OnStepRunnerStopped

	stepIn            chan *target.Target
	addedTargets      map[string]OnTargetResult
	runningLoopActive bool

	stepStopped       chan struct{}
	resultErr         error
	resultResumeState json.RawMessage
}

func (sr *StepRunner) AddTarget(tgt *target.Target, callback OnTargetResult) error {
	if callback == nil {
		return fmt.Errorf("callback should not be nil")
	}
	err := func(tgt *target.Target, callback OnTargetResult) error {
		sr.mu.Lock()
		defer sr.mu.Unlock()

		if sr.ctx.Err() != nil {
			return sr.ctx.Err()
		}

		if sr.stepIn == nil {
			return fmt.Errorf("step runner is stepStopped")
		}

		existingCb := sr.addedTargets[tgt.ID]
		if existingCb != nil {
			return fmt.Errorf("existing target")
		}
		sr.addedTargets[tgt.ID] = callback
		return nil
	}(tgt, callback)
	if err != nil {
		return err
	}

	// check if running
	select {
	case sr.stepIn <- tgt:
	case <-sr.ctx.Until(xcontext.ErrPaused):
		return xcontext.ErrPaused
	case <-sr.ctx.Done():
		return sr.ctx.Err()
	}
	return nil
}

func (sr *StepRunner) IsRunning() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.stepStopped != nil
}

func (sr *StepRunner) WaitResults(ctx context.Context) (json.RawMessage, error) {
	sr.mu.Lock()
	resultErr := sr.resultErr
	resultResumeState := sr.resultResumeState
	stepStopped := sr.stepStopped
	sr.mu.Unlock()

	if resultErr != nil {
		return resultResumeState, resultErr
	}

	if stepStopped != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-stepStopped:
		}
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.resultResumeState, sr.resultErr
}

// Stop triggers TestStep to stop running by closing input channel
func (sr *StepRunner) Stop() {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.stepIn != nil {
		sr.ctx.Debugf("Close input channel")
		close(sr.stepIn)
		sr.stepIn = nil
	}
}

func (sr *StepRunner) readingLoop(stepOut chan test.TestStepResult, testStepLabel string) {
	invokePanicSafe := func(callback OnTargetResult, res error) {
		if r := recover(); r != nil {
			sr.ctx.Errorf("Callback panic, stack: %s", debug.Stack())
		}
		callback(res)
	}

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

			sr.mu.Lock()
			callback, found := sr.addedTargets[res.Target.ID]
			sr.addedTargets[res.Target.ID] = nil
			sr.mu.Unlock()

			if !found {
				sr.setErr(&cerrors.ErrTestStepReturnedUnexpectedResult{StepName: testStepLabel, Target: res.Target.ID})
				return
			}
			if callback == nil {
				sr.setErr(&cerrors.ErrTestStepReturnedDuplicateResult{StepName: testStepLabel, Target: res.Target.ID})
				return
			}
			invokePanicSafe(callback, res.Err)

		case <-sr.ctx.Done():
			sr.ctx.Debugf("canceled readingLoop")
			return
		}
	}
}

func (sr *StepRunner) runningLoop(
	stepIn chan *target.Target, stepOut chan test.TestStepResult,
	bundle test.TestStepBundle, ev testevent.Emitter, resumeState json.RawMessage,
) {
	defer func() {
		sr.mu.Lock()
		sr.runningLoopActive = false
		sr.mu.Unlock()

		if recoverOccurred := safeCloseOutCh(stepOut); recoverOccurred {
			sr.setErr(&cerrors.ErrTestStepClosedChannels{StepName: bundle.TestStepLabel})
		}
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

	inChannels := test.TestStepChannels{In: stepIn, Out: stepOut}
	resultResumeState, err := bundle.TestStep.Run(sr.ctx, inChannels, bundle.Parameters, ev, resumeState)
	sr.ctx.Debugf("Step runner finished '%v', rs %s", err, string(resultResumeState))

	sr.mu.Lock()
	sr.setErrLocked(err)
	sr.resultResumeState = resultResumeState
	sr.notifyStoppedLocked(err)
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
	bundle test.TestStepBundle,
	ev testevent.Emitter,
	resumeState json.RawMessage,
	stoppedCallback OnStepRunnerStopped,
) *StepRunner {
	stepIn := make(chan *target.Target)
	stepOut := make(chan test.TestStepResult)

	srCrx, cancel := xcontext.WithCancel(ctx)
	sr := &StepRunner{
		ctx:               srCrx,
		cancel:            cancel,
		stepIn:            stepIn,
		addedTargets:      make(map[string]OnTargetResult),
		runningLoopActive: true,
		stepStopped:       make(chan struct{}),
		stopCallback:      stoppedCallback,
	}

	var activeLoopsCount int32 = 2
	onFinished := func() {
		if atomic.AddInt32(&activeLoopsCount, -1) != 0 {
			return
		}
		sr.mu.Lock()
		defer sr.mu.Unlock()

		close(sr.stepStopped)
		sr.stepStopped = nil
	}

	go func() {
		defer onFinished()
		sr.runningLoop(stepIn, stepOut, bundle, ev, resumeState)
	}()

	go func() {
		defer onFinished()
		sr.readingLoop(stepOut, bundle.TestStepLabel)
	}()
	return sr
}
