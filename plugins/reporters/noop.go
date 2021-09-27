// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package reporter

import (
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// Noop is a report from a reporter
type NoopReport struct {
	report string
}

var _ Report = &NoopReport{}

func (n *NoopReport) String() string {
	return n.report
}

func (n *NoopReport) OK() bool {
	return true
}

// Noop is a reporter that does nothing. Probably only useful for testing.
type Noop struct{}

var _ Reporter = &Noop{}

// Name implements Name
func (n *Noop) Name() string {
	return "Noop"
}

// ValidateRunParameters implements ValidateRunParameters
func (n *Noop) ValidateRunParameters(params []byte) (Report, error) {
	return &NoopReport{}, nil
}

// ValidateFinalParameters validates the parameters for the final reporter
func (n *Noop) ValidateFinalParameters(params []byte) (Report, error) {
	return &NoopReport{}, nil
}

// RunReport calculates the report to be associated with a job run.
func (n *Noop) RunReport(ctx xcontext.Context, parameters Parameters, runStatus *job.RunStatus, ev testevent.Fetcher) ([]Report, error) {
	return []Report{&NoopReport{"I did nothing"}}, nil
}

// FinalReport calculates the final report to be associated to a job.
func (n *Noop) FinalReport(ctx xcontext.Context, parameters Parameters, runStatuses []job.RunStatus, ev testevent.Fetcher) ([]Report, error) {
	return []Report{&NoopReport{"I did nothing at the end, all good"}}, nil
}

func (*Noop) OK() bool {
	return true
}

// New builds a new TargetSuccessReporter
func NewNoop() Reporter {
	return &Noop{}
}

// register thyself.
func init() {
	reporters["Noop"] = NewNoop
}
