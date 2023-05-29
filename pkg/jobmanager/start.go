// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package jobmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/beltctx"
	"github.com/facebookincubator/go-belt/tool/experimental/metrics"
	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/metrics/perf"
	"github.com/linuxboot/contest/pkg/signaling"
	"github.com/linuxboot/contest/pkg/signals"
)

func (jm *JobManager) start(ev *api.Event) *api.EventResponse {
	msg := ev.Msg.(api.EventStartMsg)

	var jd job.Descriptor
	if err := json.Unmarshal([]byte(msg.JobDescriptor), &jd); err != nil {
		return &api.EventResponse{Err: err}
	}
	// Check the compatibility of the JobDescriptor
	if err := jd.CheckVersion(); err != nil {
		return &api.EventResponse{Err: err}
	}
	if err := job.CheckTags(jd.Tags, false /* allowInternal */); err != nil {
		return &api.EventResponse{Err: err}
	}
	// Add instance tag, if specified.
	if jm.config.instanceTag != "" {
		jd.Tags = job.AddTags(jd.Tags, jm.config.instanceTag)
	}
	j, err := NewJobFromDescriptor(ev.Context, jm.pluginRegistry, &jd)
	if err != nil {
		return &api.EventResponse{Err: err}
	}
	jdJSON, err := json.MarshalIndent(&jd, "", "    ")
	if err != nil {
		return &api.EventResponse{Err: err}
	}

	// The job descriptor has been validated correctly, now use the JobRequestEmitter
	// interface to obtain a JobRequest object with a valid id
	request := job.Request{
		JobName:            j.Name,
		JobDescriptor:      string(jdJSON),
		ExtendedDescriptor: j.ExtendedDescriptor,
		Requestor:          string(ev.Msg.Requestor()),
		ServerID:           ev.ServerID,
		RequestTime:        time.Now(),
	}
	jobID, err := jm.jsm.StoreJobRequest(ev.Context, &request)
	if err != nil {
		return &api.EventResponse{
			Requestor: ev.Msg.Requestor(),
			Err:       fmt.Errorf("could not create job request: %v", err)}
	}

	j.ID = jobID

	jm.startJob(ev.Context, j, nil)

	return &api.EventResponse{
		JobID:     j.ID,
		Requestor: ev.Msg.Requestor(),
		Err:       nil,
		Status: &job.Status{
			Name:      j.Name,
			State:     string(job.EventJobStarted),
			StartTime: time.Now(),
		},
	}
}

func (jm *JobManager) startJob(ctx context.Context, j *job.Job, resumeState *job.PauseEventPayload) {
	jm.jobsMu.Lock()
	defer jm.jobsMu.Unlock()
	jobCtx, jobCancel := context.WithCancel(ctx)
	jobCtx, jobPause := signaling.WithSignal(jobCtx, signals.Paused)
	jm.jobs[j.ID] = &jobInfo{job: j, pause: jobPause, cancel: jobCancel}
	go jm.runJob(jobCtx, j, resumeState)
}

func (jm *JobManager) runJob(ctx context.Context, j *job.Job, resumeState *job.PauseEventPayload) {
	defer func() {
		jm.jobsMu.Lock()
		delete(jm.jobs, j.ID)
		jm.jobsMu.Unlock()
	}()

	if metrics := metrics.FromCtx(ctx); metrics != nil {
		// reflect the number of running jobs
		metrics.IntGauge(perf.RUNNING_JOBS).Add(1)
		//, when the job is done decrement the counter
		defer metrics.IntGauge(perf.RUNNING_JOBS).Add(-1)
	}

	ctx = beltctx.WithField(ctx, "job_id", j.ID)

	if err := jm.emitEvent(ctx, j.ID, job.EventJobStarted); err != nil {
		logging.Errorf(ctx, "failed to emit event: %v", err)
		return
	}

	start := time.Now()
	resumeState, err := jm.jobRunner.Run(ctx, j, resumeState)
	duration := time.Since(start)
	logging.Debugf(ctx, "Job %d: runner finished, err %v", j.ID, err)
	switch err {
	case context.Canceled:
		_ = jm.emitEvent(ctx, j.ID, job.EventJobCancelled)
		return
	case signals.Paused:
		if err := jm.emitEventPayload(ctx, j.ID, job.EventJobPaused, resumeState); err != nil {
			_ = jm.emitErrEvent(ctx, j.ID, job.EventJobPauseFailed, fmt.Errorf("Job %+v failed pausing: %v", j, err))
		} else {
			logging.Infof(ctx, "Successfully paused job %d (run %d, %d targets)", j.ID, resumeState.RunID, len(resumeState.Targets))
			logging.Debugf(ctx, "Job %d pause state: %+v", j.ID, resumeState)
		}
		return
	}
	select {
	case <-signaling.Until(ctx, signals.Paused):
		// We were asked to pause but failed to do so.
		pauseErr := fmt.Errorf("Job %+v failed pausing: %v", j, err)
		logging.Errorf(ctx, "%v", pauseErr)
		_ = jm.emitErrEvent(ctx, j.ID, job.EventJobPauseFailed, pauseErr)
		return
	default:
	}
	logging.Infof(ctx, "Job %d finished", j.ID)
	// at this point it is safe to emit the job status event. Note: this is
	// checking `err` from the `jm.jobRunner.Run()` call above.
	if err != nil {
		_ = jm.emitErrEvent(ctx, j.ID, job.EventJobFailed, fmt.Errorf("Job %d failed after %s: %w", j.ID, duration, err))
	} else {
		logging.Infof(ctx, "Job %+v completed after %s", j, duration)
		err = jm.emitEvent(ctx, j.ID, job.EventJobCompleted)
		if err != nil {
			logging.Warnf(ctx, "event emission failed: %v", err)
		}
	}
}
