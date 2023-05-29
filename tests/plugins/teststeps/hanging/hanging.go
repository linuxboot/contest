// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package hanging

import (
	"context"
	"encoding/json"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"
)

// Name is the name used to look this plugin up.
var Name = "Hanging"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

type hanging struct {
}

// Name returns the name of the Step
func (ts *hanging) Name() string {
	return Name
}

// Run executes a step that does not process any targets and never returns.
func (ts *hanging) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	channel := make(chan struct{})
	<-channel
	return nil, nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *hanging) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new hanging step
func New() test.TestStep {
	return &hanging{}
}
