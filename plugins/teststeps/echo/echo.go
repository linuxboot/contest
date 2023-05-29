// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package echo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"
)

// Name is the name used to look this plugin up.
var Name = "Echo"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

// Step implements an echo-style printing plugin.
type Step struct{}

// New initializes and returns a new EchoStep. It implements the TestStepFactory
// interface.
func New() test.TestStep {
	return &Step{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}

// ValidateParameters validates the parameters that will be passed to the Run
// and Resume methods of the test step.
func (e Step) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	if t := params.GetOne("text"); t.IsEmpty() {
		return errors.New("Missing 'text' field in echo parameters")
	}
	return nil
}

// Name returns the name of the Step
func (e Step) Name() string {
	return Name
}

// Run executes the step
func (e Step) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	for {
		select {
		case target, ok := <-ch.In:
			if !ok {
				return nil, nil
			}
			output, err := params.GetOne("text").Expand(target, stepsVars)
			if err != nil {
				return nil, err
			}
			// guaranteed to work here
			jobID, _ := types.JobIDFromContext(ctx)
			runID, _ := types.RunIDFromContext(ctx)
			logging.Infof(ctx, "This is job %d, run %d on target %s with text '%s'", jobID, runID, target.ID, output)
			ch.Out <- test.TestStepResult{Target: target}
		case <-ctx.Done():
			return nil, nil
		}
	}
}
