// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package storage

import (
	"testing"

	"github.com/linuxboot/contest/pkg/event/frameworkevent"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/stretchr/testify/require"
)

type nullStorage struct {
	jobRequestCount   int
	eventRequestCount int
}

func (n *nullStorage) GetJobRequestCount() int {
	return n.jobRequestCount
}

func (n *nullStorage) GetEventRequestCount() int {
	return n.eventRequestCount
}

// jobs interface
func (n *nullStorage) StoreJobRequest(ctx xcontext.Context, request *job.Request) (types.JobID, error) {
	n.jobRequestCount++
	return types.JobID(0), nil
}
func (n *nullStorage) GetJobRequest(ctx xcontext.Context, jobID types.JobID) (*job.Request, error) {
	n.jobRequestCount++
	return nil, nil
}
func (n *nullStorage) StoreReport(ctx xcontext.Context, report *job.Report) error {
	n.jobRequestCount++
	return nil
}
func (n *nullStorage) GetJobReport(ctx xcontext.Context, jobID types.JobID) (*job.JobReport, error) {
	n.jobRequestCount++
	return nil, nil
}
func (n *nullStorage) ListJobs(ctx xcontext.Context, query *JobQuery) ([]types.JobID, error) {
	n.jobRequestCount++
	return nil, nil
}

func (n *nullStorage) GetEngineVault() EngineVault {
	return nil
}

func (n *nullStorage) SetEngineVault(_ EngineVault) {
}

// events interface
func (n *nullStorage) StoreTestEvent(ctx xcontext.Context, event testevent.Event) error {
	n.eventRequestCount++
	return nil
}
func (n *nullStorage) GetTestEvents(ctx xcontext.Context, eventQuery *testevent.Query) ([]testevent.Event, error) {
	n.eventRequestCount++
	return nil, nil
}
func (n *nullStorage) StoreFrameworkEvent(ctx xcontext.Context, event frameworkevent.Event) error {
	n.eventRequestCount++
	return nil
}
func (n *nullStorage) GetFrameworkEvent(ctx xcontext.Context, eventQuery *frameworkevent.Query) ([]frameworkevent.Event, error) {
	n.eventRequestCount++
	return nil, nil
}

func (n *nullStorage) Close() error {
	return nil
}
func (n *nullStorage) Version() (uint64, error) {
	return 0, nil
}

func mockStorage(t *testing.T, vault *SimpleEngineVault) (*nullStorage, *nullStorage) {
	storage := &nullStorage{}
	storageAsync := &nullStorage{}

	// TODO: Add the option of running tests in parallel
	require.NoError(t, vault.StoreEngine(storage, SyncEngine))
	require.NoError(t, vault.StoreEngine(storageAsync, AsyncEngine))
	return storage, storageAsync
}

func TestSetStorage(t *testing.T) {
	require.NoError(t, NewSimpleEngineVault().StoreEngine(&nullStorage{}, SyncEngine))
}

func TestSetAsyncStorage(t *testing.T) {
	require.NoError(t, NewSimpleEngineVault().StoreEngine(&nullStorage{}, AsyncEngine))
}
