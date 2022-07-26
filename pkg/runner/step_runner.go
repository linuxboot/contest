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

type ResultNotifier interface {
	ResultCh() <-chan error
}

type AddTargetToStep func(ctx xcontext.Context, tgt *target.Target) (ResultNotifier, error)

type StepResult struct {
	Err         error
	ResumeState json.RawMessage
}

type StepRunner struct {
	mu sync.Mutex

	input         chan *target.Target
	inputWg       sync.WaitGroup
	activeTargets map[string]*stepTargetInfo

	started           bool
	stopped           chan struct{}
	finishedCh        chan struct{}
	runningLoopActive bool
	notifyStopped     *resultNotifier

	resultErr         error
	resultResumeState json.RawMessage
}

type resultNotifier struct {
	resultCh chan error
}

func newResultNotifier() *resultNotifier {
	return &resultNotifier{
		resultCh: make(chan error, 1),
	}
}

func (str *resultNotifier) ResultCh() <-chan error {
	return str.resultCh
}

func (str *resultNotifier) addResult(err error) {
	str.resultCh <- err
	close(str.resultCh)
}

type stepTargetInfo struct {
	targetInEmitted bool
	result          *resultNotifier
}

func (sti *stepTargetInfo) acquireTargetInEmission() bool {
	if sti.targetInEmitted {
		return false
	}
	sti.targetInEmitted = true
	return true
}

// NewStepRunner creates a new StepRunner object
func NewStepRunner() *StepRunner {
	return &StepRunner{
		input:         make(chan *target.Target),
		activeTargets: make(map[string]*stepTargetInfo),
		notifyStopped: newResultNotifier(),
		stopped:       make(chan struct{}),
		finishedCh:    make(chan struct{}),
	}
}

func (sr *StepRunner) Run(
	ctx xcontext.Context,
	bundle test.TestStepBundle,
	ev testevent.Emitter,
	resumeState json.RawMessage,
	resumeStateTargets []target.Target,
) (AddTargetToStep, []ResultNotifier, ResultNotifier, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.started {
		return nil, nil, nil, &cerrors.ErrAlreadyDone{}
	}

	var resumedTargetsResults []ResultNotifier
	for _, resumeTarget := range resumeStateTargets {
		targetInfo := &stepTargetInfo{
			targetInEmitted: true,
			result:          newResultNotifier(),
		}
		sr.activeTargets[resumeTarget.ID] = targetInfo
		resumedTargetsResults = append(resumedTargetsResults, targetInfo.result)
	}

	var activeLoopsCount int32 = 2
	finish := func() {
		if atomic.AddInt32(&activeLoopsCount, -1) != 0 {
			return
		}

		sr.mu.Lock()
		close(sr.finishedCh)
		// if an error occurred we already sent notification
		if sr.resultErr == nil {
			sr.notifyStopped.addResult(nil)
		}
		sr.mu.Unlock()

		ctx.Debugf("StepRunner finished")
		sr.Stop()
	}

	stepOut := make(chan test.TestStepResult)
	go func() {
		defer finish()
		sr.runningLoop(ctx, sr.input, stepOut, bundle, ev, resumeState)
		ctx.Debugf("Running loop finished")
	}()

	go func() {
		defer finish()
		sr.outputLoop(ctx, stepOut, ev, bundle.TestStepLabel)
		ctx.Debugf("Reading loop finished")
	}()

	sr.started = true
	return func(ctx xcontext.Context, tgt *target.Target) (ResultNotifier, error) {
		return sr.addTarget(ctx, bundle, ev, tgt)
	}, resumedTargetsResults, sr.notifyStopped, nil
}

func (sr *StepRunner) addTarget(
	ctx xcontext.Context,
	bundle test.TestStepBundle,
	ev testevent.Emitter,
	tgt *target.Target,
) (ResultNotifier, error) {
	if tgt == nil {
		return nil, fmt.Errorf("target should not be nil")
	}

	sr.mu.Lock()
	stopped := sr.stopped
	sr.mu.Unlock()
	if stopped == nil {
		if err := sr.getErr(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("step runner was stopped")
	}

	targetInfo, err := func() (*stepTargetInfo, error) {
		targetInfo, err := func() (*stepTargetInfo, error) {
			sr.mu.Lock()
			defer sr.mu.Unlock()

			if targetInfo := sr.activeTargets[tgt.ID]; targetInfo != nil {
				return nil, fmt.Errorf("target is already added")
			}
			targetInfo := &stepTargetInfo{
				result: newResultNotifier(),
			}
			sr.activeTargets[tgt.ID] = targetInfo
			sr.inputWg.Add(1)
			return targetInfo, nil
		}()
		if err != nil {
			return nil, err
		}

		defer sr.inputWg.Done()
		select {
		case sr.input <- tgt:
			// we should always emit TargetIn before TargetOut or TargetError
			// we have a race condition that outputLoop may receive result for this target first
			// in that case we will emit TargetIn in outputLoop and should not emit it here
			sr.mu.Lock()
			if targetInfo.acquireTargetInEmission() {
				if err := emitEvent(ctx, ev, target.EventTargetIn, tgt, nil); err != nil {
					sr.setErrLocked(ctx, fmt.Errorf("failed to report target injection: %w", err))
				}
			}
			sr.mu.Unlock()
			return targetInfo, nil
		case <-stopped:
			return nil, fmt.Errorf("step runner was stopped")
		case <-ctx.Until(xcontext.ErrPaused):
			return nil, xcontext.ErrPaused
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}()

	if err != nil {
		sr.mu.Lock()
		if sr.activeTargets[tgt.ID] == nil {
			sr.setErrLocked(ctx,
				&cerrors.ErrTestStepReturnedDuplicateResult{StepName: bundle.TestStepLabel, Target: tgt.ID})
		}
		sr.activeTargets[tgt.ID] = nil
		if sr.resultErr != nil {
			err = sr.resultErr
		}
		sr.mu.Unlock()
		return nil, err
	}
	return targetInfo.result, nil
}

func (sr *StepRunner) Started() bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	return sr.started
}

// WaitResults returns TestStep.Run() output
// It returns an error if and only if waiting was terminated by inputQueue ctx argument and returns ctx.Err()
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

// Stop triggers TestStep to stop running
func (sr *StepRunner) Stop() {
	alreadyStopped := func() bool {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		if sr.stopped == nil {
			return true
		}
		close(sr.stopped)
		sr.stopped = nil
		return false
	}()
	if alreadyStopped {
		return
	}

	sr.inputWg.Wait()
	close(sr.input)
}

func (sr *StepRunner) outputLoop(
	ctx xcontext.Context,
	stepOut chan test.TestStepResult,
	ev testevent.Emitter,
	testStepLabel string,
) {
	for {
		select {
		case res, ok := <-stepOut:
			if !ok {
				ctx.Debugf("Output channel closed")
				func() {
					sr.mu.Lock()
					defer sr.mu.Unlock()

					for tgtID, info := range sr.activeTargets {
						if info != nil {
							sr.setErrLocked(ctx, &cerrors.ErrTestStepLostTargets{
								StepName: testStepLabel,
								Targets:  []string{tgtID},
							})
						}
					}

					if sr.runningLoopActive {
						// This means that plugin closed its channels before leaving.
						sr.setErrLocked(ctx, &cerrors.ErrTestStepClosedChannels{StepName: testStepLabel})
					}
				}()
				return
			}

			if res.Target == nil {
				sr.setErr(ctx, &cerrors.ErrTestStepReturnedNoTarget{StepName: testStepLabel})
				return
			}
			ctx.Infof("Obtained '%v' for target '%s'", res, res.Target.ID)

			shouldEmitTargetIn, targetResult, err := func() (bool, *resultNotifier, error) {
				sr.mu.Lock()
				defer sr.mu.Unlock()

				info, found := sr.activeTargets[res.Target.ID]
				if !found {
					return false, nil, &cerrors.ErrTestStepReturnedUnexpectedResult{
						StepName: testStepLabel,
						Target:   res.Target.ID,
					}
				}
				if info == nil {
					return false, nil, &cerrors.ErrTestStepReturnedDuplicateResult{StepName: testStepLabel, Target: res.Target.ID}
				}
				sr.activeTargets[res.Target.ID] = nil

				shouldEmitTargetIn := info.acquireTargetInEmission()
				return shouldEmitTargetIn, info.result, nil
			}()
			if err != nil {
				sr.setErr(ctx, err)
				return
			}

			if shouldEmitTargetIn {
				if err := emitEvent(ctx, ev, target.EventTargetIn, res.Target, nil); err != nil {
					sr.setErr(ctx, fmt.Errorf("failed to report target injection: %w", err))
					return
				}
			}

			if res.Err == nil {
				err = emitEvent(ctx, ev, target.EventTargetOut, res.Target, nil)
			} else {
				err = emitEvent(ctx, ev, target.EventTargetErr, res.Target, target.ErrPayload{Error: res.Err.Error()})
			}
			if err != nil {
				ctx.Errorf("failed to emit event: %s", err)
				sr.setErr(ctx, err)
				return
			}
			targetResult.addResult(res.Err)
		case <-ctx.Done():
			ctx.Debugf("IO loop detected context canceled")
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

	sr.notifyStopped.addResult(err)
}

func (sr *StepRunner) getErr() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.resultErr
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
