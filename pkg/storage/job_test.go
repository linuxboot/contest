// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package storage

import (
	"testing"

	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
	"github.com/stretchr/testify/require"
)

type testJobStorageManagerFixture struct {
	ctx      xcontext.Context
	jobID    types.JobID
	jobQuery *JobQuery
}

func mockJobStorageManagerData() *testJobStorageManagerFixture {
	query, _ := BuildJobQuery()

	return &testJobStorageManagerFixture{
		ctx:      logrusctx.NewContext(logger.LevelDebug),
		jobID:    types.JobID(0),
		jobQuery: query,
	}
}

func TestJobStorageConsistency(t *testing.T) {
	f := mockJobStorageManagerData()
	vault := NewSimpleEngineVault()
	jsm := NewJobStorageManager(vault)

	var cases = []struct {
		name   string
		getter func(ctx xcontext.Context, jsm *JobStorageManager)
	}{
		{
			"TestGetJobRequest",
			func(ctx xcontext.Context, jsm *JobStorageManager) { _, _ = jsm.GetJobRequest(ctx, f.jobID) },
		},
		{
			"TestGetJobReport",
			func(ctx xcontext.Context, jsm *JobStorageManager) { _, _ = jsm.GetJobReport(ctx, f.jobID) },
		},
		{
			"TestListJobs",
			func(ctx xcontext.Context, jsm *JobStorageManager) { _, _ = jsm.ListJobs(ctx, f.jobQuery) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var storage, storageAsync *nullStorage
			storage, storageAsync = mockStorage(t, vault)

			// test with default context
			tc.getter(f.ctx, &jsm)
			require.Equal(t, storage.GetJobRequestCount(), 1)
			require.Equal(t, storageAsync.GetJobRequestCount(), 0)

			// test with explicit strong consistency
			ctx := WithConsistencyModel(f.ctx, ConsistentReadAfterWrite)
			tc.getter(ctx, &jsm)
			require.Equal(t, storage.GetJobRequestCount(), 2)
			require.Equal(t, storageAsync.GetJobRequestCount(), 0)

			// test with explicit relaxed consistency
			ctx = WithConsistencyModel(ctx, ConsistentEventually)
			tc.getter(ctx, &jsm)
			require.Equal(t, storage.GetJobRequestCount(), 2)
			require.Equal(t, storageAsync.GetJobRequestCount(), 1)
		})
	}
}
