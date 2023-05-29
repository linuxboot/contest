// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package noop

import (
	"context"
	"encoding/json"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"

	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "Noop"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

type noop struct {
}

// Name returns the name of the Step
func (ts *noop) Name() string {
	return Name
}

// Run executes a step that does nothing and returns targets with success.
func (ts *noop) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	return teststeps.ForEachTarget(Name, ctx, ch, func(ctx context.Context, t *target.Target) error {
		return nil
	})
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *noop) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new noop step
func New() test.TestStep {
	return &noop{}
}
