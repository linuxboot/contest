// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package fail

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"

	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "Fail"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

type fail struct {
}

// Name returns the name of the Step
func (ts *fail) Name() string {
	return Name
}

// Run executes a step that fails all the targets it receives.
func (ts *fail) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	return teststeps.ForEachTarget(Name, ctx, ch, func(ctx context.Context, t *target.Target) error {
		return fmt.Errorf("Integration test failure for %v", t)
	})
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *fail) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new noop step
func New() test.TestStep {
	return &fail{}
}
