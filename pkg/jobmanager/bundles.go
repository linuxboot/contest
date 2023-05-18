// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package jobmanager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/storage/limits"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// newReportingBundles returns the bundles for the run report and the final report
func newReportingBundles(registry *pluginregistry.PluginRegistry, jobDescriptor *job.Descriptor) ([]*job.ReporterBundle, []*job.ReporterBundle, error) {

	var (
		runReporterBundles   []*job.ReporterBundle
		finalReporterBundles []*job.ReporterBundle
	)

	for _, reporter := range jobDescriptor.Reporting.RunReporters {
		if strings.TrimSpace(reporter.Name) == "" {
			return nil, nil, errors.New("invalid empty or all-whitespace run reporter name")
		}
		if err := limits.NewValidator().ValidateReporterName(reporter.Name); err != nil {
			return nil, nil, err
		}
		bundle, err := registry.NewRunReporterBundle(reporter.Name, reporter.Parameters)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create bundle for run reporter '%s': %v", reporter.Name, err)
		}
		runReporterBundles = append(runReporterBundles, bundle)
	}

	for _, reporter := range jobDescriptor.Reporting.FinalReporters {
		if strings.TrimSpace(reporter.Name) == "" {
			return nil, nil, errors.New("invalid empty or all-whitespace final reporter name")
		}
		if err := limits.NewValidator().ValidateReporterName(reporter.Name); err != nil {
			return nil, nil, err
		}
		bundle, err := registry.NewFinalReporterBundle(reporter.Name, reporter.Parameters)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create bundle for final reporter '%s': %v", reporter.Name, err)
		}
		finalReporterBundles = append(finalReporterBundles, bundle)
	}

	return runReporterBundles, finalReporterBundles, nil
}

func newBundlesFromSteps(ctx xcontext.Context, descriptors []*test.TestStepDescriptor, registry *pluginregistry.PluginRegistry) ([]test.TestStepBundle, error) {

	// look up test step plugins in the plugin registry
	var stepBundles []test.TestStepBundle

	for idx, descriptor := range descriptors {
		if descriptor == nil {
			return nil, fmt.Errorf("test step description is null")
		}
		if err := limits.NewValidator().ValidateTestStepLabel(descriptor.Label); err != nil {
			return nil, err
		}
		tsb, err := registry.NewTestStepBundle(ctx, *descriptor)
		if err != nil {
			return nil, fmt.Errorf("NewStepBundle for test step '%s' with index %d failed: %w", descriptor.Name, idx, err)
		}
		stepBundles = append(stepBundles, *tsb)
	}

	if len(stepBundles) == 0 {
		return nil, fmt.Errorf("at least one test step is required per test")
	}

	return stepBundles, nil

}

// newStepBundles creates bundles for the test
func newStepBundles(ctx xcontext.Context, descriptors []*test.TestStepDescriptor, registry *pluginregistry.PluginRegistry) ([]test.TestStepBundle, error) {

	testStepBundles, err := newBundlesFromSteps(ctx, descriptors, registry)
	if err != nil {
		return nil, fmt.Errorf("could not create test steps bundle: %w", err)
	}

	// verify that there are not duplicated labels
	labels := make(map[string]bool)
	for _, bundle := range testStepBundles {
		if _, ok := labels[bundle.TestStepLabel]; ok {
			return nil, fmt.Errorf("found duplicated labels: %s", bundle.TestStepLabel)
		}
		labels[bundle.TestStepLabel] = true
	}
	// TODO: verify that test variables refer to existing steps
	return testStepBundles, nil
}
