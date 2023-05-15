// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package types

import (
	"strconv"

	"github.com/linuxboot/contest/pkg/xcontext"
)

// JobID represents a unique job identifier
type JobID uint64

// RunID represents the id of a run within the Job
type RunID uint64

// JobOwnerID represents a unique owner identifier of a given job
type JobOwnerID uint64

func (v JobID) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

func (v RunID) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

func (v JobOwnerID) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

type key string

const (
	KeyJobID = key("job_id")
	KeyRunID = key("run_id")
	KeyJobOwnerID = key("job_owner_id")
)

// JobIDFromContext is a helper to get the JobID, this is useful
// for plugins which need to know which job they are running.
// Not all context object everywhere have this set, but this is
// guaranteed to work in TargetManagers, TestSteps and Reporters
func JobIDFromContext(ctx xcontext.Context) (JobID, bool) {
	v, ok := ctx.Value(KeyJobID).(JobID)
	return v, ok
}

// RunIDFromContext is a helper to get the RunID.
// Not all context object everywhere have this set, but this is
// guaranteed to work in TargetManagers, TestSteps and RunReporters
func RunIDFromContext(ctx xcontext.Context) (RunID, bool) {
	v, ok := ctx.Value(KeyRunID).(RunID)
	return v, ok
}

// JobOwnerIDFromContext is a helper to get the JobOwnerID.
// Not all context object everywhere have this set, but this is
// guaranteed to work in TargetManagers, TestSteps and RunReporters
func JobOwnerIDFromContext(ctx xcontext.Context) (JobOwnerID, bool) {
	v, ok := ctx.Value(KeyJobOwnerID).(JobOwnerID)
	return v, ok
}
