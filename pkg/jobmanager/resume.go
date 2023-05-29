// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package jobmanager

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event/frameworkevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/types"
)

func (jm *JobManager) resumeJobs(ctx context.Context, serverID string) error {
	pausedJobs, err := jm.listMyJobs(ctx, serverID, job.JobStatePaused)
	if err != nil {
		return fmt.Errorf("failed to list paused jobs: %w", err)
	}
	logging.Infof(ctx, "Found %d paused jobs for %s/%s", len(pausedJobs), jm.config.instanceTag, serverID)
	for _, jobID := range pausedJobs {
		if err := jm.resumeJob(ctx, jobID); err != nil {
			logging.Errorf(ctx, "failed to resume job %d: %v, failing it", jobID, err)
			if err = jm.emitErrEvent(ctx, jobID, job.EventJobFailed, fmt.Errorf("failed to resume job %d: %w", jobID, err)); err != nil {
				logging.Warnf(ctx, "Failed to emit event for %d: %v", jobID, err)
			}
		}
	}
	return nil
}

func (jm *JobManager) resumeJob(ctx context.Context, jobID types.JobID) error {
	logging.Debugf(ctx, "attempting to resume job %d", jobID)
	results, err := jm.frameworkEvManager.Fetch(
		ctx,
		frameworkevent.QueryJobID(jobID),
		frameworkevent.QueryEventName(job.EventJobPaused),
	)
	if err != nil {
		return fmt.Errorf("failed to query resume state for job %d: %w", jobID, err)
	}
	if len(results) == 0 {
		return fmt.Errorf("no resume state found for job %d", jobID)
	}

	// get the latest event by id
	var lastEventIdx int
	for idx, ev := range results {
		if ev.SequenceID > results[lastEventIdx].SequenceID {
			lastEventIdx = idx
		}
	}
	var resumeState job.PauseEventPayload
	if results[lastEventIdx].Payload == nil {
		return fmt.Errorf("invald resume state for job %d: %+v", jobID, results[0])
	}
	if err := json.Unmarshal(*results[lastEventIdx].Payload, &resumeState); err != nil {
		return fmt.Errorf("invald resume state for job %d: %w", jobID, err)
	}
	if resumeState.Version != job.CurrentPauseEventPayloadVersion {
		return fmt.Errorf("incompatible resume state version (want %d, got %d)",
			job.CurrentPauseEventPayloadVersion, resumeState.Version)
	}
	req, err := jm.jsm.GetJobRequest(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to retrieve job descriptor for %d: %w", jobID, err)
	}
	j, err := NewJobFromExtendedDescriptor(ctx, jm.pluginRegistry, req.ExtendedDescriptor)
	if err != nil {
		return fmt.Errorf("failed to create job %d: %w", jobID, err)
	}
	j.ID = jobID
	logging.Debugf(ctx, "running resumed job %d", j.ID)
	jm.startJob(ctx, j, &resumeState)
	return nil
}
