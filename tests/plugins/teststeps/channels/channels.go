// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package channels

import (
	"context"
	"encoding/json"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"
)

// Name is the name used to look this plugin up.
var Name = "Channels"

// Events defines the events that a TestStep is allowed to emit
var Events = []event.Name{}

type channels struct {
}

// Name returns the name of the Step
func (ts *channels) Name() string {
	return Name
}

// Run executes a step that runs fine but closes its output channels on exit.
func (ts *channels) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	for target := range ch.In {
		ch.Out <- test.TestStepResult{Target: target}
	}
	// This is bad, do not do this.
	close(ch.Out)
	return nil, nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *channels) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new Channels step
func New() test.TestStep {
	return &channels{}
}
