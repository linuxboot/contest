// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package pluginregistry

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"

	"github.com/facebookincubator/go-belt/beltctx"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"

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
func (e AStep) ValidateParameters(ctx context.Context, params test.TestStepParameters) error {
	return nil
}

// Name returns the name of the AStep
func (e AStep) Name() string {
	return "AStep"
}

// Run executes the AStep
func (e AStep) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	return nil, nil
}

func TestRegisterTestStep(t *testing.T) {
	ctx, cancel := context.WithCancel(logger.CtxWithLogger(context.Background(), logrus.Default()))
	defer beltctx.Flush(ctx)
	defer cancel()
	pr := NewPluginRegistry(ctx)
	err := pr.RegisterTestStep("AStep", NewAStep, []event.Name{"AStepEventName"})
	require.NoError(t, err)
}

func TestRegisterTestStepDoesNotValidate(t *testing.T) {
	ctx, cancel := context.WithCancel(logger.CtxWithLogger(context.Background(), logrus.Default()))
	defer beltctx.Flush(ctx)
	defer cancel()
	pr := NewPluginRegistry(ctx)
	err := pr.RegisterTestStep("AStep", NewAStep, []event.Name{"Event which does not validate"})
	require.Error(t, err)
}
