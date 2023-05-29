package variables

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"

	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
const Name = "variables"

// Events defines the events that a TestStep is allowed to emit
var Events []event.Name

// Variables creates variables that can be used by other test steps
type Variables struct {
}

// Name returns the plugin name.
func (ts *Variables) Name() string {
	return Name
}

// Run executes the cmd step.
func (ts *Variables) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	if err := ts.ValidateParameters(ctx, inputParams); err != nil {
		return nil, err
	}
	return teststeps.ForEachTarget(Name, ctx, ch, func(ctx context.Context, target *target.Target) error {
		for name, ps := range inputParams {
			logging.Debugf(ctx, "add variable %s, value: %s", name, ps[0])
			if err := stepsVars.Add(target.ID, name, ps[0].RawMessage); err != nil {
				return err
			}
		}
		return nil
	})
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *Variables) ValidateParameters(ctx context.Context, params test.TestStepParameters) error {
	for name, ps := range params {
		if err := test.CheckIdentifier(name); err != nil {
			return fmt.Errorf("invalid variable name: '%s': %w", name, err)
		}
		if len(ps) != 1 {
			return fmt.Errorf("invalid number of parameter '%s' values: %d (expected 1)", name, len(ps))
		}

		var res interface{}
		if err := json.Unmarshal(ps[0].RawMessage, &res); err != nil {
			return fmt.Errorf("invalid json '%s': %w", ps[0].RawMessage, err)
		}
	}
	return nil
}

// New initializes and returns a new Variables test step.
func New() test.TestStep {
	return &Variables{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}
