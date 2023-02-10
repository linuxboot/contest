// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package transport

import (
	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// Transport abstracts different ways of talking to contest server.
// This interface strictly only uses contest data structures.
type Transport interface {
	Version(ctx xcontext.Context, requestor string) (*api.VersionResponse, error)
	Start(ctx xcontext.Context, requestor string, jobDescriptor string) (*api.StartResponse, error)
	Stop(ctx xcontext.Context, requestor string, jobID types.JobID) (*api.StopResponse, error)
	Status(ctx xcontext.Context, requestor string, jobID types.JobID) (*api.StatusResponse, error)
	Retry(ctx xcontext.Context, requestor string, jobID types.JobID) (*api.RetryResponse, error)
	List(ctx xcontext.Context, requestor string, states []job.State, tags []string) (*api.ListResponse, error)
}
