// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package badtargets

import (
	"encoding/json"
	"fmt"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// Name is the name used to look this plugin up.
const Name = "BadTargets"

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{}

type badTargets struct {
}

// Name returns the name of the Step
func (ts *badTargets) Name() string {
	return Name
}

// Run executes a step that messes up the flow of targets.
func (ts *badTargets) Run(
	ctx xcontext.Context,
	io test.TestStepInputOutput,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	for {
		tgt, err := io.Get(ctx)
		if err != nil {
			return nil, err
		}
		if tgt == nil {
			return nil, nil
		}

		switch tgt.ID {
		case "TDrop":
			// ... crickets ...
		case "TGood":
			if err := io.Report(ctx, *tgt, nil); err != nil {
				return nil, err
			}
		case "TDup":
			if err := io.Report(ctx, *tgt, nil); err != nil {
				return nil, err
			}
			if err := io.Report(ctx, *tgt, nil); err != nil {
				return nil, err
			}
		case "TExtra":
			if err := io.Report(ctx, *tgt, nil); err != nil {
				return nil, err
			}
			if err := io.Report(ctx, target.Target{ID: "TExtra2"}, nil); err != nil {
				return nil, err
			}
		case "T1":
			// Mangle the returned target name.
			if err := io.Report(ctx, target.Target{ID: tgt.ID + "XXX"}, nil); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unexpected target name: %q", tgt.ID)
		}
	}
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *badTargets) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new badTargets step
func New() test.TestStep {
	return &badTargets{}
}
