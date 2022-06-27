// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package pluginregistry

import (
	"encoding/json"
	"testing"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"

	"github.com/stretchr/testify/require"
)

// Definition of two dummy TestSteps to be used to test the PluginRegistry

// AStep implements a dummy TestStep
type AStep struct{}

// NewAStep initializes a new AStep
func NewAStep() test.TestStep {
	return &AStep{}
}

// ValidateParameters validates the parameters for the AStep
func (e AStep) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	return nil
}

// Name returns the name of the AStep
func (e AStep) Name() string {
	return "AStep"
}

// Run executes the AStep
func (e AStep) Run(
	ctx xcontext.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	return nil, nil
}

func TestRegisterTestStep(t *testing.T) {
	ctx := logrusctx.NewContext(logger.LevelDebug)
	pr := NewPluginRegistry(ctx)
	err := pr.RegisterTestStep("AStep", NewAStep, []event.Name{"AStepEventName"})
	require.NoError(t, err)
}

func TestRegisterTestStepDoesNotValidate(t *testing.T) {
	ctx := logrusctx.NewContext(logger.LevelDebug)
	pr := NewPluginRegistry(ctx)
	err := pr.RegisterTestStep("AStep", NewAStep, []event.Name{"Event which does not validate"})
	require.Error(t, err)
}

func TestNewTestStepBundle(t *testing.T) {
	t.Run("valid_bundle", func(t *testing.T) {
		ctx := logrusctx.NewContext(logger.LevelDebug)
		pr := NewPluginRegistry(ctx)
		err := pr.RegisterTestStep("AStep", NewAStep, []event.Name{"AStepEventName"})
		require.NoError(t, err)

		_, err = pr.NewTestStepBundle(ctx, test.TestStepDescriptor{
			Name:  "AStep",
			Label: "Dummy",
			VariablesMapping: map[string]string{
				"variable": "step_label.var",
			},
		})
		require.NoError(t, err)
	})

	t.Run("invalid_variable_name", func(t *testing.T) {
		ctx := logrusctx.NewContext(logger.LevelDebug)
		pr := NewPluginRegistry(ctx)
		err := pr.RegisterTestStep("AStep", NewAStep, []event.Name{"AStepEventName"})
		require.NoError(t, err)

		_, err = pr.NewTestStepBundle(ctx, test.TestStepDescriptor{
			Name:  "AStep",
			Label: "Dummy",
			VariablesMapping: map[string]string{
				"variable   ": "step_label.var",
			},
		})
		require.Error(t, err)
	})
}
