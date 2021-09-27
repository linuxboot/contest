// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package reporter

import (
	"encoding/json"
	"fmt"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/lib/comparison"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type TargetReport struct {
	Message         string
	AchievedSuccess string
	DesiredSuccess  string
	report          string
	ok              bool
}

var _ Report = &TargetReport{}

func (t *TargetReport) String() string {
	return t.report
}

func (t *TargetReport) OK() bool {
	return t.ok
}

type Target struct {
}

var _ Reporter = &Target{}

// RunParameters contains the parameters necessary for the run reporter to
// elaborate the results of the Job
type TargetRunParameters struct {
	Expression string
}

var _ Parameters = &TargetRunParameters{}

func (f *TargetRunParameters) String() string {
	return f.Expression
}

// TargetFinalParameters contains the parameters necessary for the final reporter to
// elaborate the results of the Job
type TargetFinalParameters struct {
	AverageExpression string
}

var _ Parameters = &TargetRunParameters{}

func (f *TargetFinalParameters) String() string {
	return f.AverageExpression
}

// ValidateRunParameters validates the parameters for the run reporter
func (ts *Target) ValidateRunParameters(params []byte) (Report, error) {
	var rp TargetRunParameters
	if err := json.Unmarshal(params, &rp); err != nil {
		return nil, err
	}
	if _, err := comparison.ParseExpression(rp.Expression); err != nil {
		return nil, fmt.Errorf("could not parse success expression")
	}
	return &Validate{"OK", true}, nil
}

// ValidateFinalParameters validates the parameters for the final reporter
func (ts *Target) ValidateFinalParameters(params []byte) (Report, error) {
	var fp TargetFinalParameters
	if err := json.Unmarshal(params, &fp); err != nil {
		return nil, err
	}
	if _, err := comparison.ParseExpression(fp.AverageExpression); err != nil {
		return nil, fmt.Errorf("could not parse average success expression")
	}
	return &Validate{"OK", true}, nil
}

// Name implements Name
func (ts *Target) Name() string {
	return "Target"
}

// RunReport calculates the report to be associated with a job run.
func (ts *Target) RunReport(ctx xcontext.Context, p Parameters, runStatus *job.RunStatus, ev testevent.Fetcher) ([]Report, error) {
	// FIXME. Can get get a run parameter interface generic enough not to need this?
	rp, ok := p.(*TargetRunParameters)
	if !ok {
		return nil, fmt.Errorf("Arguments must be %T, not %T", &TargetRunParameters{}, rp)
	}

	var (
		success     uint64
		fail        uint64
		testReports []Report
	)

	// Flag the run as successful only if all Tests within the Run where successful
	for _, t := range runStatus.TestStatuses {
		fail = 0
		success = 0

		for _, t := range t.TargetStatuses {
			if t.Error != "" {
				fail++
			} else {
				success++
			}

		}

		if success+fail == 0 {
			return nil, fmt.Errorf("overall count of success and failures is zero for test %s", t.TestCoordinates.TestName)
		}
		cmpExpr, err := comparison.ParseExpression(rp.Expression)
		if err != nil {
			return nil, fmt.Errorf("error while calculating run report for test %s: %v", t.TestCoordinates.TestName, err)
		}
		res, err := cmpExpr.EvaluateSuccess(success, success+fail)
		if err != nil {
			return nil, fmt.Errorf("error while calculating run report for test %s: %v", t.TestCoordinates.TestName, err)
		}

		if !res.Pass {
			testReports = append(testReports, &Validate{fmt.Sprintf("Test %s does not pass success criteria: %s", t.TestCoordinates.TestName, res.Expr), false})
		} else {
			testReports = append(testReports, &Validate{fmt.Sprintf("Test %s passes success criteria: %s", t.TestCoordinates.TestName, res.Expr), true})
		}
	}

	return testReports, nil
}

// FinalReport calculates the final report to be associated to a job.
func (ts *Target) FinalReport(ctx xcontext.Context, parameters Parameters, runStatuses []job.RunStatus, ev testevent.Fetcher) ([]Report, error) {
	return []Report{&Validate{fmt.Sprintf("final reporting not implemented yet in Target"), false}}, nil
}

// New builds a new TargetReporter
func NewTarget() Reporter {
	return &Target{}
}

func init() {
	reporters["Target"] = NewTarget
}
