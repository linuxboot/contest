// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package storage

import (
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// JobStorage defines the interface that implements persistence for job
// related information
type JobStorage interface {
	// Job request interface
	StoreJobRequest(ctx xcontext.Context, request *job.Request) (types.JobID, error)
	GetJobRequest(ctx xcontext.Context, jobID types.JobID) (*job.Request, error)

	// Job report interface
	StoreReport(ctx xcontext.Context, report *job.Report) error
	GetJobReport(ctx xcontext.Context, jobID types.JobID) (*job.JobReport, error)

	// Job enumeration interface
	ListJobs(ctx xcontext.Context, query *JobQuery) ([]types.JobID, error)
}

// JobStorageManager implements JobStorage interface
type JobStorageManager struct {
	vault EngineVault
}

// StoreJobRequest submits a job request to the storage layer
func (jsm JobStorageManager) StoreJobRequest(ctx xcontext.Context, request *job.Request) (types.JobID, error) {
	storage, err := jsm.vault.GetEngine(SyncEngine)
	if err != nil {
		return 0, err
	}

	return storage.StoreJobRequest(ctx, request)
}

// GetJobRequest fetches a job request from the storage layer
func (jsm JobStorageManager) GetJobRequest(ctx xcontext.Context, jobID types.JobID) (*job.Request, error) {
	engineType := SyncEngine
	if !isStronglyConsistent(ctx) {
		engineType = AsyncEngine
	}
	storage, err := jsm.vault.GetEngine(engineType)
	if err != nil {
		return nil, err
	}

	return storage.GetJobRequest(ctx, jobID)
}

// StoreReport submits a job run or final report to the storage layer
func (jsm JobStorageManager) StoreReport(ctx xcontext.Context, report *job.Report) error {
	storage, err := jsm.vault.GetEngine(SyncEngine)
	if err != nil {
		return err
	}

	return storage.StoreReport(ctx, report)
}

// GetJobReport fetches a job report from the storage layer
func (jsm JobStorageManager) GetJobReport(ctx xcontext.Context, jobID types.JobID) (*job.JobReport, error) {
	engineType := SyncEngine
	if !isStronglyConsistent(ctx) {
		engineType = AsyncEngine
	}
	storage, err := jsm.vault.GetEngine(engineType)
	if err != nil {
		return nil, err
	}

	return storage.GetJobReport(ctx, jobID)
}

// ListJobs returns list of job IDs matching the query
func (jsm JobStorageManager) ListJobs(ctx xcontext.Context, query *JobQuery) ([]types.JobID, error) {
	engineType := SyncEngine
	if !isStronglyConsistent(ctx) {
		engineType = AsyncEngine
	}
	storage, err := jsm.vault.GetEngine(engineType)
	if err != nil {
		return nil, err
	}

	return storage.ListJobs(ctx, query)
}

// NewJobStorageManager creates a new JobStorageManager object
func NewJobStorageManager(vault EngineVault) JobStorageManager {
	return JobStorageManager{vault: vault}
}
