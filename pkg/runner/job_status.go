// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package runner

import (
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/frameworkevent"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// targetRoutingEvents gather all event names which track the flow of targets
// between TestSteps
var targetRoutingEvents = map[event.Name]struct{}{
	target.EventTargetIn:         {},
	target.EventTargetErr:        {},
	target.EventTargetOut:        {},
	target.EventTargetInErr:      {},
	target.EventTargetAcquireErr: {},
}

// buildTargetStatuses builds a list of TargetStepStatus, which represent the status of Targets within a TestStep
func (jr *JobRunner) buildTargetStatuses(coordinates job.TestStepCoordinates, targetEvents []testevent.Event) ([]job.TargetStatus, error) {
	var targetStatuses []job.TargetStatus
	for _, testEvent := range targetEvents {

		// Update the TargetStatus object associated to the Target. If there is no TargetStatus associated yet, append it
		var targetStatus *job.TargetStatus
		for index, candidateStatus := range targetStatuses {
			if candidateStatus.Target.ID == testEvent.Data.Target.ID {
				targetStatus = &targetStatuses[index]
				break
			}
		}

		if targetStatus == nil {
			// There is no TargetStatus associated with this Target, create one
			targetStatuses = append(targetStatuses, job.TargetStatus{TestStepCoordinates: coordinates, Target: testEvent.Data.Target})
			targetStatus = &targetStatuses[len(targetStatuses)-1]
		}
		// append non-routing events
		if _, isRoutingEvent := targetRoutingEvents[testEvent.Data.EventName]; !isRoutingEvent {
			targetStatus.Events = append(targetStatus.Events, testEvent)
		}

		evName := testEvent.Data.EventName
		if evName == target.EventTargetIn {
			targetStatus.InTime = testEvent.EmitTime
		} else if evName == target.EventTargetOut {
			targetStatus.OutTime = testEvent.EmitTime
		} else if evName == target.EventTargetErr {
			targetStatus.OutTime = testEvent.EmitTime
			errorPayload, err := target.UnmarshalErrPayload(*testEvent.Data.Payload)
			if err != nil {
				targetStatus.Error = fmt.Sprintf("could not unmarshal payload error: %v", err)
			} else {
				targetStatus.Error = errorPayload.Error
			}
		}
	}

	return targetStatuses, nil
}

// buildTestStepStatus builds the status object of a test step belonging to a test
func (jr *JobRunner) buildTestStepStatus(ctx xcontext.Context, coordinates job.TestStepCoordinates) (*job.TestStepStatus, error) {

	testStepStatus := job.TestStepStatus{TestStepCoordinates: coordinates}

	// Fetch all Events associated to this TestStep
	testEvents, err := jr.testEvManager.Fetch(ctx,
		testevent.QueryJobID(coordinates.JobID),
		testevent.QueryRunID(coordinates.RunID),
		testevent.QueryTestName(coordinates.TestName),
		testevent.QueryTestStepLabel(coordinates.TestStepLabel),
	)
	if err != nil {
		return nil, fmt.Errorf("could not fetch events associated to test step %s: %v", coordinates.TestStepLabel, err)
	}

	// Keep only events of the latest attempt
	// Otherwise we will have to make all reporters do the filtering on their side
	var lastAttempt uint32
	for _, ev := range testEvents {
		if ev.Header.TestAttempt > lastAttempt {
			lastAttempt = ev.Header.TestAttempt
		}
	}

	var stepEvents, targetEvents []testevent.Event
	for _, ev := range testEvents {
		if ev.Header.TestAttempt != lastAttempt {
			continue
		}
		if ev.Data.Target == nil {
			// we don't want target routing events in step events, but we want
			// them in target events below
			if _, skip := targetRoutingEvents[ev.Data.EventName]; skip {
				ctx.Warnf("Found routing event '%s' with no target associated, this could indicate a bug", ev.Data.EventName)
				continue
			}
			// this goes into TestStepStatus.Events
			stepEvents = append(stepEvents, ev)
		} else {
			// this goes into TargetStatus.Events
			targetEvents = append(targetEvents, ev)
		}
	}

	testStepStatus.Events = stepEvents
	targetStatuses, err := jr.buildTargetStatuses(coordinates, targetEvents)
	if err != nil {
		return nil, fmt.Errorf("could not build target status for test step %s: %v", coordinates.TestStepLabel, err)
	}
	testStepStatus.TargetStatuses = targetStatuses
	return &testStepStatus, nil
}

// buildTestStatus builds the status of a test belonging to a specific to a test
func (jr *JobRunner) buildTestStatus(ctx xcontext.Context, coordinates job.TestCoordinates, currentJob *job.Job) (*job.TestStatus, error) {

	var currentTest *test.Test
	// Identify the test within the Job for which we are asking to calculate the status
	for _, candidateTest := range currentJob.Tests {
		if candidateTest.Name == coordinates.TestName {
			currentTest = candidateTest
			break
		}
	}

	if currentTest == nil {
		return nil, fmt.Errorf("job with id %d does not include any test named %s", coordinates.JobID, coordinates.TestName)
	}
	testStatus := job.TestStatus{
		TestCoordinates:  coordinates,
		TestStepStatuses: make([]job.TestStepStatus, len(currentTest.TestStepsBundles)),
	}

	// Build a TestStepStatus object for each TestStep
	for index, bundle := range currentTest.TestStepsBundles {
		testStepCoordinates := job.TestStepCoordinates{
			TestCoordinates: coordinates,
			TestStepName:    bundle.TestStep.Name(),
			TestStepLabel:   bundle.TestStepLabel,
		}
		testStepStatus, err := jr.buildTestStepStatus(ctx, testStepCoordinates)
		if err != nil {
			return nil, fmt.Errorf("could not build TestStatus for test %s: %v", bundle.TestStep.Name(), err)
		}
		testStatus.TestStepStatuses[index] = *testStepStatus
	}

	// Calculate the overall status of the Targets which corresponds to the last TargetStatus
	// object recorded for each Target.

	// Fetch all events signaling that a Target has been acquired. This is the source of truth
	// indicating which Targets belong to a Test.
	targetAcquiredEvents, err := jr.testEvManager.Fetch(ctx,
		testevent.QueryJobID(coordinates.JobID),
		testevent.QueryRunID(coordinates.RunID),
		testevent.QueryTestName(coordinates.TestName),
		testevent.QueryEventNames([]event.Name{target.EventTargetAcquired, target.EventTargetAcquireErr}),
	)
	if err != nil {
		return nil, fmt.Errorf("could not fetch events associated to target acquisition")
	}

	var lastAttempt uint32
	for _, ev := range targetAcquiredEvents {
		if ev.Header.TestAttempt > lastAttempt {
			lastAttempt = ev.Header.TestAttempt
		}
	}

	var targetStatuses []job.TargetStatus
	// Keep track of the last TargetStatus seen for each Target
	targetMap := make(map[string]job.TargetStatus)
	for _, testStepStatus := range testStatus.TestStepStatuses {
		for _, targetStatus := range testStepStatus.TargetStatuses {
			targetMap[targetStatus.Target.ID] = targetStatus
		}
	}

	for _, targetEvent := range targetAcquiredEvents {
		if targetEvent.Header.TestAttempt != lastAttempt {
			continue
		}

		if targetEvent.Data.EventName == target.EventTargetAcquireErr {
			var errMessage string
			if targetEvent.Data.Payload != nil {
				if errorPayload, err := target.UnmarshalErrPayload(*targetEvent.Data.Payload); err != nil {
					errMessage = err.Error()
				} else {
					errMessage = errorPayload.Error
				}
			} else {
				errMessage = "Failed to acquire targets"
			}

			targetStatuses = append(targetStatuses, job.TargetStatus{
				TestStepCoordinates: job.TestStepCoordinates{
					TestCoordinates: coordinates,
				},
				InTime:  targetEvent.EmitTime,
				OutTime: targetEvent.EmitTime,
				Error:   errMessage,
				Events:  []testevent.Event{targetEvent},
			})
			continue
		}

		t := *targetEvent.Data.Target
		if _, ok := targetMap[t.ID]; !ok {
			// This Target is not associated to any TargetStatus, we assume it has not
			// started the test
			targetMap[t.ID] = job.TargetStatus{}
		}
		targetStatuses = append(targetStatuses, targetMap[t.ID])
	}

	testStatus.TargetStatuses = targetStatuses
	return &testStatus, nil
}

// BuildRunStatus builds the status of a run with a job
func (jr *JobRunner) BuildRunStatus(ctx xcontext.Context, coordinates job.RunCoordinates, currentJob *job.Job) (*job.RunStatus, error) {

	runStatus := job.RunStatus{RunCoordinates: coordinates, TestStatuses: make([]job.TestStatus, len(currentJob.Tests))}

	for index, currentTest := range currentJob.Tests {
		testCoordinates := job.TestCoordinates{RunCoordinates: coordinates, TestName: currentTest.Name}
		testStatus, err := jr.buildTestStatus(ctx, testCoordinates, currentJob)
		if err != nil {
			return nil, fmt.Errorf("could not rebuild status for test %s: %v", currentTest.Name, err)
		}
		runStatus.TestStatuses[index] = *testStatus
	}
	return &runStatus, nil
}

// BuildRunStatuses builds the status of all runs belonging to the job
func (jr *JobRunner) BuildRunStatuses(ctx xcontext.Context, currentJob *job.Job) ([]job.RunStatus, error) {

	// Calculate the status only for the runs which effectively were executed
	runStartEvents, err := jr.frameworkEventManager.Fetch(ctx, frameworkevent.QueryEventName(EventRunStarted), frameworkevent.QueryJobID(currentJob.ID))
	if err != nil {
		return nil, fmt.Errorf("could not determine how many runs were executed: %v", err)
	}
	if len(runStartEvents) == 0 {
		return nil, nil
	}

	numRuns := types.RunID(0)
	for _, runStartEvent := range runStartEvents {
		payload, err := runStartEvent.Payload.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("could not extract JSON payload from RunStart event: %v", err)
		}

		payloadUnmarshaled := RunStartedPayload{}
		if err := json.Unmarshal(payload, &payloadUnmarshaled); err != nil {
			return nil, fmt.Errorf("could not unmarshal RunStarted event payload")
		}

		if payloadUnmarshaled.RunID > numRuns {
			numRuns = payloadUnmarshaled.RunID
		}
	}

	var runStatuses []job.RunStatus
	for runID := types.RunID(1); runID <= numRuns; runID++ {
		runCoordinates := job.RunCoordinates{JobID: currentJob.ID, RunID: runID}
		runStatus, err := jr.BuildRunStatus(ctx, runCoordinates, currentJob)
		if err != nil {
			return nil, fmt.Errorf("could not rebuild run status for run %d: %v", runID, err)
		}
		runStatuses = append(runStatuses, *runStatus)
	}
	return runStatuses, nil
}
