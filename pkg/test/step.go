// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// TestStepParameters represents the parameters that a TestStep should consume
// according to the Test descriptor fetched by the TestFetcher
type TestStepParameters map[string][]Param

// ParameterFunc is a function type called on parameters that need further
// validation or manipulation. It is currently used by GetFunc and GetOneFunc.
type ParameterFunc func(string) string

// Get returns the value of the requested parameter. A missing item is not
// distinguishable from an empty value. For this you need to use the regular map
// accessor.
func (t TestStepParameters) Get(k string) []Param {
	return t[k]
}

// GetOne returns the first value of the requested parameter. If the parameter
// is missing, an empty string is returned.
func (t TestStepParameters) GetOne(k string) *Param {
	v, ok := t[k]
	if !ok || len(v) == 0 {
		return &Param{}
	}
	return &v[0]
}

// GetInt works like GetOne, but also tries to convert the string to an int64,
// and returns an error if this fails.
func (t TestStepParameters) GetInt(k string) (int64, error) {
	v := t.GetOne(k)
	if v.String() == "" {
		return 0, errors.New("expected an integer string, got an empty string")
	}
	n, err := strconv.ParseInt(v.String(), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot convert '%s' to int: %v", v, err)
	}
	return n, nil
}

// TestStepStatus represent a blob containing status information that the TestStep
// can persist into storage to support resuming the test.
type TestStepStatus string

// TestStepFactory is a type representing a function which builds a TestStep.
// TestStep factories are registered in the plugin registry.
type TestStepFactory func() TestStep

// TestStepLoader is a type representing a function which returns all the
// needed things to be able to load a TestStep.
type TestStepLoader func() (string, TestStepFactory, []event.Name)

// TestStepsDescriptors bundles together description of the test step
// which constitute each test
type TestStepsDescriptors struct {
	TestName  string
	TestSteps []*TestStepDescriptor
}

// TestStepDescriptor is the definition of a test step matching a test step
// configuration.
type TestStepDescriptor struct {
	Name             string
	Label            string
	Parameters       TestStepParameters
	VariablesMapping map[string]string
}

// TestStepBundle bundles the selected TestStep together with its parameters as
// specified in the Test descriptor fetched by the TestFetcher
type TestStepBundle struct {
	TestStep      TestStep
	TestStepLabel string
	Parameters    TestStepParameters
	AllowedEvents map[event.Name]bool
}

// TestStepResult is used by TestSteps to report result for a particular target.
// Empty Err means success, non-empty indicates failure.
// Failed targets do not proceed to further steps in this run.
type TestStepResult struct {
	Target *target.Target
	Err    error
}

type TestStepInputOutput interface {
	Get(ctx xcontext.Context) (*target.Target, error)
	Report(ctx xcontext.Context, tgt target.Target, err error) error
}

// StepsVariablesReader represents a read access for step variables
// Example:
// var sv StepsVariables
// intVar := 42
// sv.Add("dummy-target-id", "varname", intVar)
//
// var recvIntVar int
// sv.Get(("dummy-target-id", "varname", &recvIntVar)
// assert recvIntVar == 42
type StepsVariablesReader interface {
	// Get obtains existing variable that was added in one of the previous steps
	Get(tgtID string, stepLabel, name string, out interface{}) error
}

// StepsVariables represents a read/write access for step variables
type StepsVariables interface {
	StepsVariablesReader
	// Add adds a new or replaces existing variable associated with current test step and target
	Add(tgtID string, name string, in interface{}) error
}

// TestStep is the interface that all steps need to implement to be executed
// by the TestRunner
type TestStep interface {
	// Name returns the name of the step
	Name() string
	// Run runs the test step. The test step is expected to be synchronous.
	Run(ctx xcontext.Context, inputOutput TestStepInputOutput, ev testevent.Emitter,
		stepsVars StepsVariables, params TestStepParameters,
		resumeState json.RawMessage) (json.RawMessage, error)
	// ValidateParameters checks that the parameters are correct before passing
	// them to Run.
	ValidateParameters(ctx xcontext.Context, params TestStepParameters) error
}

var identifierRegexPattern *regexp.Regexp

func init() {
	var err error
	identifierRegexPattern, err = regexp.Compile(`[a-zA-Z]\w*`)
	if err != nil {
		panic(fmt.Sprintf("invalid identifier matching regexp expression: %v", err))
	}
}

// CheckIdentifier checks that input argument forms a valid identifier name
func CheckIdentifier(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("empty identifier")
	}
	match := identifierRegexPattern.FindString(s)
	if len(match) != len(s) {
		return fmt.Errorf("identifier should be an non-empty string that matches: %s", identifierRegexPattern.String())
	}
	return nil
}
