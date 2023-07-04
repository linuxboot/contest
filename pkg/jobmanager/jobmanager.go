// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package jobmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/beltctx"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/insomniacslk/xjson"

	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/frameworkevent"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/runner"
	"github.com/linuxboot/contest/pkg/signaling"
	"github.com/linuxboot/contest/pkg/signals"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/types"
)

// ErrorEventPayload represents the payload carried by a failure event (e.g. JobStateFailed, JobStateCancelled, etc.)
type ErrorEventPayload struct {
	Err xjson.Error
}

// JobManager is the core component for the long-running job management service.
// It handles API requests, test fetching, target fetching, and jobs lifecycle.
//
// In more detail, it is responsible for:
// * spawning the API listener, and handling the incoming requests
// * fetching targets, via target managers
// * fetching test definitions, via test fetchers
// * enqueuing new job requests, and handling their status
// * starting, stopping, and retrying jobs
type JobManager struct {
	config

	jobs      map[types.JobID]*jobInfo
	jobRunner *runner.JobRunner

	jobsMu sync.Mutex

	jsm storage.JobStorageManager

	frameworkEvManager frameworkevent.EmitterFetcher
	testEvManager      testevent.Fetcher

	apiListener    api.Listener
	pluginRegistry *pluginregistry.PluginRegistry

	apiCancel context.CancelFunc

	msgCounter int
}

type jobInfo struct {
	job           *job.Job
	pause, cancel func()
}

// New initializes and returns a new JobManager with the given API listener.
func New(l api.Listener, pr *pluginregistry.PluginRegistry, storageEngineVault storage.EngineVault, opts ...Option) (*JobManager, error) {
	if pr == nil {
		return nil, errors.New("plugin registry cannot be nil")
	}
	jsm := storage.NewJobStorageManager(storageEngineVault)

	frameworkEvManager := storage.NewFrameworkEventEmitterFetcher(storageEngineVault)
	testEvManager := storage.NewTestEventFetcher(storageEngineVault)

	cfg := getConfig(opts...)
	if cfg.instanceTag != "" {
		if err := job.IsValidTag(cfg.instanceTag, true /* allowInternal */); err != nil {
			return nil, fmt.Errorf("invalid instaceTag: %w", err)
		}
		if !job.IsInternalTag(cfg.instanceTag) {
			return nil, fmt.Errorf("instaceTag must be an internal tag (start with %q)", job.InternalTagPrefix)
		}
	}

	jm := JobManager{
		config:             cfg,
		apiListener:        l,
		pluginRegistry:     pr,
		jobs:               make(map[types.JobID]*jobInfo),
		jsm:                jsm,
		frameworkEvManager: frameworkEvManager,
		testEvManager:      testEvManager,
	}
	jm.jobRunner = runner.NewJobRunner(jsm, storageEngineVault, cfg.clock, cfg.targetLockDuration)
	return &jm, nil
}

func (jm *JobManager) handleEvent(ev *api.Event) {
	var resp *api.EventResponse

	switch ev.Type {
	case api.EventTypeStart:
		resp = jm.start(ev)
	case api.EventTypeStatus:
		resp = jm.status(ev)
	case api.EventTypeStop:
		resp = jm.stop(ev)
	case api.EventTypeRetry:
		resp = jm.retry(ev)
	case api.EventTypeList:
		resp = jm.list(ev)
	default:
		resp = &api.EventResponse{
			Requestor: ev.Msg.Requestor(),
			Err:       fmt.Errorf("invalid event type: %v", ev.Type),
		}
	}

	logging.Debugf(ev.Context, "Sending response %+v", resp)
	// time to wait before printing an error if the response is not received.
	sendEventTimeout := 3 * time.Second

	select {
	case ev.RespCh <- resp:
	case <-time.After(sendEventTimeout):
		// TODO send failure event once we have the event infra
		// TODO determine whether the server should shut down if there
		//      are too many errors
		logging.Panicf(ev.Context, "timed out after %v trying to send a response event", sendEventTimeout)
	}
}

// Run is responsible for starting the API listener and responding to incoming events.
func (jm *JobManager) Run(ctx context.Context, resumeJobs bool) error {
	jm.jobRunner.StartLockRefresh()
	defer jm.jobRunner.StopLockRefresh()

	a, err := api.New(jm.config.apiOptions...)
	if err != nil {
		return fmt.Errorf("Cannot start API: %w", err)
	}

	// Deal with zombieed jobs (fail them).
	if err := jm.failZombieJobs(ctx, a.ServerID()); err != nil {
		logging.Errorf(ctx, "failed to fail jobs: %v", err)
	}

	// First, resume paused jobs.
	if resumeJobs {
		if err := jm.resumeJobs(ctx, a.ServerID()); err != nil {
			return fmt.Errorf("failed to resume jobs: %w", err)
		}
	}

	apiCtx, apiCancel := context.WithCancel(ctx)
	jm.apiCancel = func() {
		logging.Debugf(ctx, "cancelling API context")
		apiCancel()
	}

	errCh := make(chan error, 1)
	go func() {
		lErr := jm.apiListener.Serve(apiCtx, a)
		logging.Infof(ctx, "Listener shut down successfully.")
		errCh <- lErr
		close(errCh)
	}()

	var handlerWg sync.WaitGroup
loop:
	for {
		select {
		// handle events from the API
		case ev := <-a.Events:
			logging.Debugf(ev.Context, "Handling event %+v", ev)
			handlerWg.Add(1)
			go func() {
				defer func() {
					errmon.ObserveRecoverCtx(ev.Context, recover())
				}()
				defer handlerWg.Done()
				jm.handleEvent(ev)
			}()
		// check for errors or premature termination from the listener.
		case err := <-errCh:
			if err != nil {
				logging.Infof(ctx, "JobManager: API listener failed (%v)", err)
			}
			break loop
		case <-signaling.Until(ctx, signals.Paused):
			logging.Infof(ctx, "Paused")
			jm.PauseAll(ctx)
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	// Stop the API (if not already)
	jm.StopAPI()
	<-errCh
	// Wait for event handler completion
	handlerWg.Wait()
	// Wait for jobs to complete or for cancellation signal.
	doneCh := ctx.Done()
	pausedCh := signaling.Until(ctx, signals.Paused)
	for !jm.checkIdle(ctx) {
		select {
		case <-pausedCh:
			logging.Infof(ctx, "Paused")
			jm.PauseAll(ctx)
			pausedCh = nil
		case <-doneCh:
			logging.Infof(ctx, "Canceled")
			jm.CancelAll(ctx)
			// Note that we do not break out of the loop here, we expect runner to wind down and exit.
			doneCh = nil
		case <-time.After(50 * time.Millisecond):
		}
	}
	// Refresh locks one last time for jobs that were paused.
	jm.jobRunner.RefreshLocks()
	return nil
}

func (jm *JobManager) failZombieJobs(ctx context.Context, serverID string) error {
	zombieJobs, err := jm.listMyJobs(ctx, serverID, job.JobStateStarted)
	if err != nil {
		return fmt.Errorf("failed to list zombie jobs: %w", err)
	}
	logging.Infof(ctx, "Found %d zombie jobs for %s/%s", len(zombieJobs), jm.config.instanceTag, serverID)
	for _, jobID := range zombieJobs {
		// Log a line with job id so there's something in the job log to tell what happened.
		jobCtx := beltctx.WithField(ctx, "job_id", jobID)
		logging.Errorf(jobCtx, "This became a zombie, most likely the previous server instance was killed ungracefully")
		if err = jm.emitErrEvent(ctx, jobID, job.EventJobFailed, fmt.Errorf("Job %d was zombieed", jobID)); err != nil {
			logging.Errorf(ctx, "Failed to emit event: %v", err)
		}
	}
	return nil
}

func (jm *JobManager) listMyJobs(ctx context.Context, serverID string, jobState job.State) ([]types.JobID, error) {
	queryFields := []storage.JobQueryField{
		storage.QueryJobServerID(serverID),
		storage.QueryJobStates(jobState),
	}
	if jm.config.instanceTag != "" {
		queryFields = append(queryFields, storage.QueryJobTags(jm.config.instanceTag))
	}
	q, err := storage.BuildJobQuery(queryFields...)
	if err != nil {
		return nil, fmt.Errorf("failed to build job query: %w", err)
	}
	jobs, err := jm.jsm.ListJobs(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}
	return jobs, nil
}

func (jm *JobManager) checkIdle(ctx context.Context) bool {
	jm.jobsMu.Lock()
	defer jm.jobsMu.Unlock()
	if len(jm.jobs) == 0 {
		return true
	}
	if jm.msgCounter%20 == 0 {
		logging.Infof(ctx, "Waiting for %d jobs", len(jm.jobs))
	}
	jm.msgCounter++
	return false
}

// CancelJob sends a cancellation request to a specific job.
func (jm *JobManager) CancelJob(jobID types.JobID) error {
	jm.jobsMu.Lock()
	defer jm.jobsMu.Unlock()
	// get the job from the local cache rather than the storage layer. We can
	// only cancel jobs that we are actively handling.
	ji, ok := jm.jobs[jobID]
	if !ok {
		return fmt.Errorf("unknown job ID: %d", jobID)
	}
	ji.cancel()
	return nil
}

// StopAPI stops accepting new requests.
func (jm *JobManager) StopAPI() {
	jm.apiCancel()
}

// CancelAll cancels all running jobs.
func (jm *JobManager) CancelAll(ctx context.Context) {
	jm.jobsMu.Lock()
	defer jm.jobsMu.Unlock()
	for jobID, ji := range jm.jobs {
		logging.Debugf(ctx, "JobManager: cancelling job %d", jobID)
		ji.cancel()
	}
}

// CancelAll pauses all running jobs.
func (jm *JobManager) PauseAll(ctx context.Context) {
	jm.jobsMu.Lock()
	defer jm.jobsMu.Unlock()
	for jobID, ji := range jm.jobs {
		logging.Debugf(ctx, "JobManager: pausing job %d", jobID)
		ji.pause()
	}
}

func (jm *JobManager) emitEventPayload(ctx context.Context, jobID types.JobID, eventName event.Name, payload interface{}) error {
	var payloadJSON json.RawMessage
	if payload != nil {
		if p, err := json.Marshal(payload); err == nil {
			payloadJSON = json.RawMessage(p)
		} else {
			return fmt.Errorf("Could not serialize payload for event %s: %v", eventName, err)
		}
	}
	ev := frameworkevent.Event{
		JobID:     jobID,
		EventName: eventName,
		Payload:   &payloadJSON,
		EmitTime:  time.Now(),
	}
	if err := jm.frameworkEvManager.Emit(ctx, ev); err != nil {
		logging.Warnf(ctx, "Could not emit event %s for job %d: %v", eventName, jobID, err)
		return err
	}
	return nil
}

func (jm *JobManager) emitErrEvent(ctx context.Context, jobID types.JobID, eventName event.Name, err error) error {
	if err != nil {
		logging.Errorf(ctx, err.Error())
		return jm.emitEventPayload(ctx, jobID, eventName, &ErrorEventPayload{Err: *xjson.NewError(err)})
	}
	return jm.emitEventPayload(ctx, jobID, eventName, nil)
}

func (jm *JobManager) emitEvent(ctx context.Context, jobID types.JobID, eventName event.Name) error {
	return jm.emitErrEvent(ctx, jobID, eventName, nil)
}
