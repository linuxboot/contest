// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package pluginregistry

import (
	"fmt"
	"strings"

	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// NewTestStepBundle creates a TestStepBundle from a TestStepDescriptor
func (r *PluginRegistry) NewTestStepBundle(ctx xcontext.Context, testStepDescriptor test.TestStepDescriptor) (*test.TestStepBundle, error) {
	testStep, err := r.NewTestStep(testStepDescriptor.Name)
	if err != nil {
		return nil, fmt.Errorf("could not get the desired TestStep (%s): %v", testStepDescriptor.Name, err)
	}
	if err := testStep.ValidateParameters(ctx, testStepDescriptor.Parameters); err != nil {
		return nil, fmt.Errorf("could not validate parameters for test step %s: %v", testStepDescriptor.Name, err)
	}
	allowedEvents, err := r.NewTestStepEvents(testStepDescriptor.Name)
	if err != nil {
		return nil, err
	}
	label := testStepDescriptor.Label
	if len(label) == 0 {
		return nil, ErrStepLabelIsMandatory{TestStepDescriptor: testStepDescriptor}
	}

	var variablesMapping test.StepVariablesMapping
	if testStepDescriptor.VariablesMapping != nil {
		variablesMapping = make(test.StepVariablesMapping)
		for internalName, mappedName := range testStepDescriptor.VariablesMapping {
			if err := checkVariableName(internalName); err != nil {
				return nil, InvalidVariableFormat{InvalidName: internalName, Err: err}
			}
			if _, found := variablesMapping[internalName]; found {
				return nil, fmt.Errorf("duplication of '%s' variable", internalName)
			}
			// NewStepVariable creates a StepVariable object from mapping: "stepName.VariableName"
			parts := strings.Split(mappedName, ".")
			if len(parts) != 2 {
				return nil, fmt.Errorf("variable mapping '%s' should contain a single '.' separator", mappedName)
			}
			if err := checkVariableName(parts[0]); err != nil {
				return nil, InvalidVariableFormat{InvalidName: parts[0], Err: err}
			}
			if err := checkVariableName(parts[1]); err != nil {
				return nil, InvalidVariableFormat{InvalidName: parts[1], Err: err}
			}

			variablesMapping[internalName] = test.StepVariable{
				StepName:     parts[0],
				VariableName: parts[1],
			}
		}
	}
	// TODO: check that all testStep labels from variable mappings exist
	testStepBundle := test.TestStepBundle{
		TestStep:         testStep,
		TestStepLabel:    label,
		Parameters:       testStepDescriptor.Parameters,
		AllowedEvents:    allowedEvents,
		VariablesMapping: variablesMapping,
	}
	return &testStepBundle, nil
}

// NewTestFetcherBundle creates a TestFetcher and associated parameters based on
// the content of the job descriptor
func (r *PluginRegistry) NewTestFetcherBundle(ctx xcontext.Context, testDescriptor *test.TestDescriptor) (*test.TestFetcherBundle, error) {
	// Initialization and validation of the TestFetcher and its parameters
	if testDescriptor == nil {
		return nil, fmt.Errorf("test description is null")
	}
	testFetcher, err := r.NewTestFetcher(testDescriptor.TestFetcherName)
	if err != nil {
		return nil, fmt.Errorf("could not get the desired TestFetcher (%s): %v", testDescriptor.TestFetcherName, err)
	}
	// FetchParameters
	fp, err := testFetcher.ValidateFetchParameters(ctx, testDescriptor.TestFetcherFetchParameters)
	if err != nil {
		return nil, fmt.Errorf("could not validate TestFetcher fetch parameters: %v", err)
	}

	testFetcherBundle := test.TestFetcherBundle{
		TestFetcher:     testFetcher,
		FetchParameters: fp,
	}
	return &testFetcherBundle, nil
}

// NewTargetManagerBundle creates a TargetManager and associated parameters based on
// the content of the test descriptor
func (r *PluginRegistry) NewTargetManagerBundle(testDescriptor *test.TestDescriptor) (*target.TargetManagerBundle, error) {
	targetManager, err := r.NewTargetManager(testDescriptor.TargetManagerName)
	if err != nil {
		return nil, fmt.Errorf("could not get TargetManager (%s): %v", testDescriptor.TargetManagerName, err)
	}
	// AcquireParameters
	ap, err := targetManager.ValidateAcquireParameters(testDescriptor.TargetManagerAcquireParameters)
	if err != nil {
		return nil, fmt.Errorf("could not validate TargetManager acquire parameters: %v", err)
	}
	// ReleaseParameters
	rp, err := targetManager.ValidateReleaseParameters(testDescriptor.TargetManagerReleaseParameters)
	if err != nil {
		return nil, fmt.Errorf("could not validate TargetManager release parameters: %v", err)
	}

	targetManagerBundle := target.TargetManagerBundle{
		TargetManager:     targetManager,
		AcquireParameters: ap,
		ReleaseParameters: rp,
	}
	return &targetManagerBundle, nil
}

// NewRunReporterBundle creates a Reporter and associated run reporting parameters based on the
// content of the job descriptor
func (r *PluginRegistry) NewRunReporterBundle(reporterName string, reporterParameters []byte) (*job.ReporterBundle, error) {
	reporter, err := r.NewReporter(reporterName)
	if err != nil {
		return nil, fmt.Errorf("could not get reporter '%s': %v", reporterName, err)
	}

	rp, err := reporter.ValidateRunParameters(reporterParameters)
	if err != nil {
		return nil, fmt.Errorf("could not validate run reporter parameters: %v", err)
	}

	reporterBundle := job.ReporterBundle{
		Reporter:   reporter,
		Parameters: rp,
	}
	return &reporterBundle, nil
}

// NewFinalReporterBundle creates a Reporter and associated final reporting parameters based on the
// content of the job descriptor
func (r *PluginRegistry) NewFinalReporterBundle(reporterName string, reporterParameters []byte) (*job.ReporterBundle, error) {
	reporter, err := r.NewReporter(reporterName)
	if err != nil {
		return nil, fmt.Errorf("could not get reporter '%s': %v", reporterName, err)
	}

	rp, err := reporter.ValidateFinalParameters(reporterParameters)
	if err != nil {
		return nil, fmt.Errorf("could not validate run reporter parameters: %v", err)
	}

	reporterBundle := job.ReporterBundle{
		Reporter:   reporter,
		Parameters: rp,
	}
	return &reporterBundle, nil
}

func isAlpha(ch int32) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' || ch <= 'Z')
}

func checkVariableName(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("empty variable name")
	}
	for idx, ch := range s {
		if idx == 0 {
			if !isAlpha(ch) {
				return fmt.Errorf("first character should be alpha, got %c", ch)
			}
		}
		if isAlpha(ch) {
			continue
		}
		if ch >= '0' || ch <= '9' {
			continue
		}
		if ch == '_' {
			continue
		}
		return fmt.Errorf("got unxpected character: %c", ch)
	}
	return nil
}
