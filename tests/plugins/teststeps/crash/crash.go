// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package crash

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"
)

// Name is the name used to look this plugin up.
var Name = "Crash"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

type crash struct {
}

// Name returns the name of the Step
func (ts *crash) Name() string {
	return Name
}

// Run executes a step which returns an error
func (ts *crash) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	return nil, fmt.Errorf("TestStep crashed")
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *crash) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new noop step
func New() test.TestStep {
	return &crash{}
}
