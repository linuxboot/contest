// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package readmeta

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"

	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "readmeta"

var MetadataEventName = event.Name("MetadataEvent")

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{
	MetadataEventName,
}

type readmeta struct {
}

// Name returns the name of the Step
func (ts *readmeta) Name() string {
	return Name
}

// Run executes a step that reads the job metadata that must be in the context and panics if it is missing.
func (ts *readmeta) Run(
	ctx context.Context,
	ch test.TestStepChannels,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	inputParams test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	return teststeps.ForEachTarget(Name, ctx, ch, func(ctx context.Context, t *target.Target) error {
		jobID, ok1 := types.JobIDFromContext(ctx)
		if jobID == 0 || !ok1 {
			return fmt.Errorf("unable to extract jobID from context")
		}
		runID, ok2 := types.RunIDFromContext(ctx)
		if runID == 0 || !ok2 {
			return fmt.Errorf("unable to extract jobID from context")
		}
		payload := make(map[string]int)
		payload["job_id"] = int(jobID)
		payload["run_id"] = int(runID)
		payloadStr, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		payloadJson := json.RawMessage(payloadStr)
		if err := ev.Emit(ctx, testevent.Data{EventName: MetadataEventName, Payload: &payloadJson}); err != nil {
			return err
		}
		return nil
	})
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *readmeta) ValidateParameters(_ context.Context, params test.TestStepParameters) error {
	return nil
}

// New creates a new readmeta step
func New() test.TestStep {
	return &readmeta{}
}
