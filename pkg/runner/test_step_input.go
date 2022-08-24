package runner

import (
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type onTargetResult func(ctx xcontext.Context, tgt target.Target, err error) error

// TestStepChannels represents the input and output  channels used by a TestStep
// to communicate with the TestRunner
type testStepInputOutput struct {
	targetsCh      chan targetInput
	onTargetResult onTargetResult
}

func newTestStepInputOutput(targetsCh chan targetInput, onTargetResult onTargetResult) *testStepInputOutput {
	return &testStepInputOutput{
		targetsCh:      targetsCh,
		onTargetResult: onTargetResult,
	}
}

type targetInput struct {
	tgt        target.Target
	onConsumed func()
}

func (tsi *testStepInputOutput) Get(ctx xcontext.Context) (*target.Target, error) {
	select {
	case in, ok := <-tsi.targetsCh:
		if !ok {
			return nil, nil
		}
		if in.onConsumed != nil {
			in.onConsumed()
		}
		return &in.tgt, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (tsi *testStepInputOutput) Report(ctx xcontext.Context, tgt target.Target, err error) error {
	return tsi.onTargetResult(ctx, tgt, err)
}
