// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package reporter

import (
	"fmt"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type Report interface {
	String() string
	OK() bool
}

type Parameters interface {
	String() string
}

type Reporter interface {
	Name() string
	ValidateRunParameters(params []byte) (Report, error)
	ValidateFinalParameters(params []byte) (Report, error)
	RunReport(ctx xcontext.Context, parameters Parameters, runStatus *job.RunStatus, ev testevent.Fetcher) ([]Report, error)
	FinalReport(ctx xcontext.Context, parameters Parameters, runStatuses []job.RunStatus, ev testevent.Fetcher) ([]Report, error)
}

// Validate is a generic reporter that can be used if
// suitable.
type Validate struct {
	report string
	ok     bool
}

var reporters = map[string]func() Reporter{}

// New returns a Reporter for a given name,
// or false if one does not exist.
func New(name string) (Reporter, error) {
	r, ok := reporters[name]
	if !ok {
		return nil, fmt.Errorf("No reporter for %q", name)
	}
	return r(), nil
}

var _ Report = &Validate{}

// String implements string
func (v *Validate) String() string {
	return v.report
}

// OK implements OK
func (v *Validate) OK() bool {
	return v.ok
}
