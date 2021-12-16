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

type StepRunnerEvent struct {
	// Target if set represents the target for which event was generated
	Target *target.Target
	// Err if Target is not nil refers to this Target result otherwise is execution error
	Err error
}

type StepResult struct {
	Err         error
	ResumeState json.RawMessage
}

type StepRunner struct {
	mu sync.Mutex

	input             chan *target.Target
	stopOnce          sync.Once
	resultsChan       chan<- StepRunnerEvent
	runningLoopActive bool
	finishedCh        chan struct{}

	resultErr         error
	resultResumeState json.RawMessage
	notifyStoppedOnce sync.Once
}

func (sr *StepRunner) AddTarget(ctx xcontext.Context, tgt *target.Target) error {
	if tgt == nil {
		return fmt.Errorf("target should not be nil")
	}

	select {
	case sr.input <- tgt:
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
) (<-chan StepRunnerEvent, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.resultsChan != nil {
		return nil, &cerrors.ErrAlreadyDone{}
	}

	finishedCh := make(chan struct{})
	sr.finishedCh = finishedCh
	resultsChan := make(chan StepRunnerEvent, 1)
	sr.resultsChan = resultsChan

	var activeLoopsCount int32 = 2
	finish := func() {
		if atomic.AddInt32(&activeLoopsCount, -1) != 0 {
			return
		}

		sr.mu.Lock()
		close(sr.finishedCh)
		sr.finishedCh = nil
		sr.mu.Unlock()

		// if an error occurred we already sent notification
		sr.notifyStopped(ctx, nil)
		close(sr.resultsChan)
		ctx.Debugf("StepRunner finished")
	}

	stepIn := sr.input
	stepOut := make(chan test.TestStepResult)
	go func() {
		defer finish()
		sr.runningLoop(ctx, stepIn, stepOut, bundle, ev, resumeState)
		ctx.Debugf("Running loop finished")
	}()

	go func() {
		defer finish()
		sr.readingLoop(ctx, stepOut, bundle.TestStepLabel)
		ctx.Debugf("Reading loop finished")
	}()

	return resultsChan, nil
}

func (sr *StepRunner) Started() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.resultsChan != nil
}

func (sr *StepRunner) Running() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.resultsChan != nil && sr.finishedCh != nil
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
		close(sr.input)
	})
}

func (sr *StepRunner) readingLoop(
	ctx xcontext.Context,
	stepOut chan test.TestStepResult,
	testStepLabel string,
) {
	reportedTargets := make(map[string]struct{})
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

			_, found := reportedTargets[res.Target.ID]
			reportedTargets[res.Target.ID] = struct{}{}

			if found {
				sr.setErr(ctx, &cerrors.ErrTestStepReturnedDuplicateResult{StepName: testStepLabel, Target: res.Target.ID})
				return
			}

			select {
			case sr.resultsChan <- StepRunnerEvent{Target: res.Target, Err: res.Err}:
			case <-ctx.Done():
				ctx.Debugf(
					"reading loop detected context canceled, target '%s' with result: '%v' was not reported",
					res.Target.ID,
					res.Err,
				)
			}
		case <-ctx.Done():
			ctx.Debugf("reading loop detected context canceled")
			return
		}
	}
}

func (sr *StepRunner) runningLoop(
	ctx xcontext.Context,
	stepIn <-chan *target.Target,
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
				sr.setErr(ctx, &cerrors.ErrTestStepPaniced{
					StepName:   bundle.TestStepLabel,
					StackTrace: fmt.Sprintf("%s / %s", r, debug.Stack()),
				})
			}
		}()

		inChannels := test.TestStepChannels{In: stepIn, Out: stepOut}
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

	// notifyStopped is a blocking operation: should release the lock
	sr.mu.Unlock()
	sr.notifyStopped(ctx, err)
	sr.mu.Lock()
}

func (sr *StepRunner) notifyStopped(ctx xcontext.Context, err error) {
	sr.notifyStoppedOnce.Do(func() {
		select {
		case sr.resultsChan <- StepRunnerEvent{Err: err}:
		case <-ctx.Done():
		}
	})
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
		input: make(chan *target.Target),
	}
}
