package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/linuxboot/contest/pkg/cerrors"
	"github.com/linuxboot/contest/pkg/event"
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
	case <-sr.finishedCh:
		return fmt.Errorf("step runner is already stopped")
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
	resumeStateTargets []target.Target,
) (<-chan StepRunnerEvent, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.resultsChan != nil {
		return nil, &cerrors.ErrAlreadyDone{}
	}

	resultsChan := make(chan StepRunnerEvent, 1)
	sr.resultsChan = resultsChan

	var activeLoopsCount int32 = 2
	finish := func() {
		if atomic.AddInt32(&activeLoopsCount, -1) != 0 {
			return
		}

		sr.mu.Lock()
		close(sr.finishedCh)
		sr.mu.Unlock()

		// if an error occurred we already sent notification
		sr.notifyStopped(ctx, nil)
		close(sr.resultsChan)
		ctx.Debugf("StepRunner finished")
	}

	stepIn := make(chan *target.Target)
	stepOut := make(chan test.TestStepResult)
	go func() {
		defer finish()
		sr.runningLoop(ctx, stepIn, stepOut, bundle, ev, resumeState)
		ctx.Debugf("Running loop finished")
	}()

	go func() {
		defer finish()
		sr.ioLoop(ctx, stepIn, stepOut, ev, bundle.TestStepLabel, resumeStateTargets)
		ctx.Debugf("Reading loop finished")
	}()

	return resultsChan, nil
}

func (sr *StepRunner) Started() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.resultsChan != nil
}

// WaitResults returns TestStep.Run() output
// It returns an error if and only if waiting was terminated by input ctx argument and returns ctx.Err()
func (sr *StepRunner) WaitResults(ctx context.Context) (stepResult StepResult, err error) {
	sr.mu.Lock()
	resultErr := sr.resultErr
	resultResumeState := sr.resultResumeState
	sr.mu.Unlock()

	// StepRunner either finished with error or behaved incorrectly
	// it makes no sense to wait while it finishes, return what we have
	if resultErr != nil {
		return StepResult{
			Err:         resultErr,
			ResumeState: resultResumeState,
		}, nil
	}

	select {
	case <-ctx.Done():
		// give priority to returning results
		select {
		case <-sr.finishedCh:
		default:
			return StepResult{}, ctx.Err()
		}
	case <-sr.finishedCh:
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

func (sr *StepRunner) ioLoop(
	ctx xcontext.Context,
	stepIn chan<- *target.Target,
	stepOut chan test.TestStepResult,
	ev testevent.Emitter,
	testStepLabel string,
	resumeStateTargets []target.Target,
) {
	defer func() {
		if stepIn != nil {
			close(stepIn)
		}
	}()

	targetsInfo := make(map[string]int)
	for _, resumeTarget := range resumeStateTargets {
		targetsInfo[resumeTarget.ID]++
	}

	input := sr.input
	var stepInTmp chan<- *target.Target
	var inputTargets []*target.Target
	var curInputTarget *target.Target

	closeStepIn := func() {
		stepInTmp = nil
		curInputTarget = nil
		if input == nil {
			ctx.Debugf("Close step input channel")
			close(stepIn)
			stepIn = nil
			inputTargets = nil
		}
	}

	for {
		select {
		case tgt, ok := <-input:
			if !ok {
				input = nil
				if len(inputTargets) == 0 {
					closeStepIn()
				}
			} else {
				stepInTmp = stepIn
				inputTargets = append(inputTargets, tgt)
				curInputTarget = inputTargets[len(inputTargets)-1]
			}
		case stepInTmp <- curInputTarget:
			targetsInfo[curInputTarget.ID]++
			if err := emitEvent(ctx, ev, target.EventTargetIn, curInputTarget, nil); err != nil {
				sr.setErrLocked(ctx, fmt.Errorf("failed to report target injection: %w", err))
			}

			inputTargets = inputTargets[:len(inputTargets)-1]
			if len(inputTargets) > 0 {
				curInputTarget = inputTargets[len(inputTargets)-1]
			} else {
				closeStepIn()
			}
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

			cnt, found := targetsInfo[res.Target.ID]
			if !found {
				sr.setErr(ctx, &cerrors.ErrTestStepReturnedUnexpectedResult{
					StepName: testStepLabel,
					Target:   res.Target.ID,
				})
				return
			}
			if cnt <= 0 {
				sr.setErr(ctx, &cerrors.ErrTestStepReturnedDuplicateResult{StepName: testStepLabel, Target: res.Target.ID})
				return
			}
			targetsInfo[res.Target.ID] = cnt - 1

			var err error
			if res.Err == nil {
				err = emitEvent(ctx, ev, target.EventTargetOut, res.Target, nil)
			} else {
				err = emitEvent(ctx, ev, target.EventTargetErr, res.Target, target.ErrPayload{Error: res.Err.Error()})
			}
			if err != nil {
				ctx.Errorf("failed to emit event: %s", err)
				sr.setErr(ctx, err)
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

// emitEvent emits the specified event with the specified JSON payload (if any).
func emitEvent(ctx xcontext.Context, ev testevent.Emitter, name event.Name, tgt *target.Target, payload interface{}) error {
	var payloadJSON *json.RawMessage
	if payload != nil {
		payloadBytes, jmErr := json.Marshal(payload)
		if jmErr != nil {
			return fmt.Errorf("failed to marshal event: %w", jmErr)
		}
		pj := json.RawMessage(payloadBytes)
		payloadJSON = &pj
	}
	errEv := testevent.Data{
		EventName: name,
		Target:    tgt,
		Payload:   payloadJSON,
	}
	return ev.Emit(ctx, errEv)
}

// NewStepRunner creates a new StepRunner object
func NewStepRunner() *StepRunner {
	return &StepRunner{
		input:      make(chan *target.Target, 1),
		finishedCh: make(chan struct{}),
	}
}
