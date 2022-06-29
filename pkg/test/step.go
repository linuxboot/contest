// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
	"strconv"
	"unicode"
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

// TestStepChannels represents the input and output  channels used by a TestStep
// to communicate with the TestRunner
type TestStepChannels struct {
	In  <-chan *target.Target
	Out chan<- TestStepResult
}

// StepsVariables represents a read/write access for step variables
type StepsVariables interface {
	// Add adds a new or replaces existing variable associated with current test step and target
	Add(tgtID string, name string, in interface{}) error

	// Get obtains existing variable by a mappedName which should be specified in variables mapping
	Get(tgtID string, stepLabel, name string, out interface{}) error
}

// TestStep is the interface that all steps need to implement to be executed
// by the TestRunner
type TestStep interface {
	// Name returns the name of the step
	Name() string
	// Run runs the test step. The test step is expected to be synchronous.
	Run(ctx xcontext.Context, ch TestStepChannels, ev testevent.Emitter,
		stepsVars StepsVariables, params TestStepParameters,
		resumeState json.RawMessage) (json.RawMessage, error)
	// ValidateParameters checks that the parameters are correct before passing
	// them to Run.
	ValidateParameters(ctx xcontext.Context, params TestStepParameters) error
}

// CheckVariableName checks that input argument forms a valid variable name
func CheckVariableName(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("empty variable name")
	}
	for idx, ch := range s {
		if idx == 0 {
			if !unicode.IsLetter(ch) {
				return fmt.Errorf("first character should be alpha, got %c", ch)
			}
		}
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' {
			continue
		}
		return fmt.Errorf("got unxpected character: %c", ch)
	}
	return nil
}
