// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package slowecho

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/benbjohnson/clock"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"

	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "SlowEcho"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

var Clock clock.Clock

// Step implements an echo-style printing plugin.
type Step struct {
}

// New initializes and returns a new EchoStep. It implements the TestStepFactory
// interface.
func New() test.TestStep {
	return &Step{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}

// Name returns the name of the Step
func (e *Step) Name() string {
	return Name
}

func sleepTime(secStr string) (time.Duration, error) {
	seconds, err := strconv.ParseFloat(secStr, 64)
	if err != nil {
		return 0, err
	}
	if seconds < 0 {
		return 0, errors.New("seconds cannot be negative in slowecho parameters")
	}
	return time.Duration(seconds*1000) * time.Millisecond, nil

}

// ValidateParameters validates the parameters that will be passed to the Run
// and Resume methods of the test step.
func (e *Step) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	if t := params.GetOne("text"); t.IsEmpty() {
		return errors.New("missing 'text' field in slowecho parameters")
	}
	secStr := params.GetOne("sleep")
	if secStr.IsEmpty() {
		return errors.New("missing 'sleep' field in slowecho parameters")
	}

	_, err := sleepTime(secStr.String())
	if err != nil {
		return err
	}
	return nil
}

// Run executes the step
func (e *Step) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	sleep, err := sleepTime(params.GetOne("sleep").String())
	if err != nil {
		return nil, err
	}
	clk := Clock
	if clk == nil {
		clk = clock.New()
	}
	f := func(ctx context.Context, t *target.Target) error {
		logging.Infof(ctx, "Waiting %v for target %s", sleep, t.ID)
		select {
		case <-clk.After(sleep):
		case <-ctx.Done():
			logging.Infof(ctx, "Returning because cancellation is requested")
			return context.Canceled
		}
		logging.Infof(ctx, "target %s: %s", t, params.GetOne("text"))
		return nil
	}
	return teststeps.ForEachTarget(Name, ctx, ch, f)
}
