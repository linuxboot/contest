// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package jobmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pkg_config "github.com/linuxboot/contest/pkg/config"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/storage/limits"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

func newJob(ctx xcontext.Context, registry *pluginregistry.PluginRegistry, jobDescriptor *job.Descriptor, resolver stepsResolver) (*job.Job, error) {

	if resolver == nil {
		return nil, fmt.Errorf("cannot create job without resolver")
	}

	if jobDescriptor == nil {
		return nil, errors.New("JobDescriptor cannot be nil")
	}
	jobName := jobDescriptor.JobName
	if err := limits.NewValidator().ValidateJobName(jobName); err != nil {
		return nil, err
	}
	if err := jobDescriptor.Validate(); err != nil {
		return nil, fmt.Errorf("could not validate job descriptor: %w", err)
	}
	if len(jobDescriptor.TestDescriptors) == 0 {
		return nil, fmt.Errorf("cannot create job with zero tests")
	}

	runReportersBundle, finalReportersBundle, err := newReportingBundles(registry, jobDescriptor)
	if err != nil {
		return nil, fmt.Errorf("error while building reporters bundles: %w", err)
	}

	stepsDescriptors, err := resolver.GetStepsDescriptors(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get steps descriptors: %w", err)
	}
	if len(stepsDescriptors) != len(jobDescriptor.TestDescriptors) {
		return nil, fmt.Errorf("length of steps descriptors must match lenght of test descriptors")
	}

	tests, err := buildTestsFromDescriptors(ctx, registry, jobDescriptor.TestDescriptors, stepsDescriptors)
	if err != nil {
		return nil, fmt.Errorf("could not build test steps: %w", err)
	}

	extendedDescriptor := job.ExtendedDescriptor{
		Descriptor:           *jobDescriptor,
		TestStepsDescriptors: stepsDescriptors,
	}

	var targetManagerAcquireTimeout = pkg_config.TargetManagerAcquireTimeout
	if jobDescriptor.TargetManagerAcquireTimeout != nil {
		targetManagerAcquireTimeout = time.Duration(*jobDescriptor.TargetManagerAcquireTimeout)
	}

	var targetManagerReleaseTimeout = pkg_config.TargetManagerReleaseTimeout
	if jobDescriptor.TargetManagerReleaseTimeout != nil {
		targetManagerReleaseTimeout = time.Duration(*jobDescriptor.TargetManagerReleaseTimeout)
	}

	// The ID of the job object defaults to zero, and it's populated as soon as the job
	// is persisted in storage.
	job := job.Job{
		ExtendedDescriptor:          &extendedDescriptor,
		Name:                        jobDescriptor.JobName,
		Tags:                        jobDescriptor.Tags,
		Runs:                        jobDescriptor.Runs,
		RunInterval:                 time.Duration(jobDescriptor.RunInterval),
		TargetManagerAcquireTimeout: targetManagerAcquireTimeout,
		TargetManagerReleaseTimeout: targetManagerReleaseTimeout,
		Tests:                       tests,
		RunReporterBundles:          runReportersBundle,
		FinalReporterBundles:        finalReportersBundle,
	}

	return &job, nil
}

func buildTestsFromDescriptors(
	ctx xcontext.Context,
	registry *pluginregistry.PluginRegistry,
	testDescriptors []*test.TestDescriptor,
	stepsDescriptors []test.TestStepsDescriptors) ([]*test.Test, error) {

	tests := make([]*test.Test, 0, len(testDescriptors))

	for index, td := range testDescriptors {
		thisTestStepsDescriptors := stepsDescriptors[index]

		if err := td.Validate(); err != nil {
			return nil, fmt.Errorf("could not validate test descriptor: %w", err)
		}
		bundleTargetManager, err := registry.NewTargetManagerBundle(td)
		if err != nil {
			return nil, err
		}
		bundleTestFetcher, err := registry.NewTestFetcherBundle(ctx, td)
		if err != nil {
			return nil, err
		}

		bundleTest, err := newBundlesFromSteps(ctx, thisTestStepsDescriptors.TestSteps, registry)
		if err != nil {
			return nil, fmt.Errorf("could not create test steps bundles: %w", err)
		}
		testName := thisTestStepsDescriptors.TestName
		if err := limits.NewValidator().ValidateTestName(testName); err != nil {
			return nil, err
		}

		var bundleCleanup []test.TestStepBundle
		if len(thisTestStepsDescriptors.CleanupSteps) > 0 {
			bundleCleanup, err = newBundlesFromSteps(ctx, thisTestStepsDescriptors.CleanupSteps, registry)
			if err != nil {
				return nil, fmt.Errorf("could not create cleanup test steps bundles: %w", err)
			}
		}
		if err := validateNoDuplicateLabels(append(bundleTest, bundleCleanup...)); err != nil {
			return nil, err
		}
		if td.Disabled {
			continue
		}
		test := test.Test{
			Name:                testName,
			TargetManagerBundle: bundleTargetManager,
			TestFetcherBundle:   bundleTestFetcher,
			TestStepsBundles:    bundleTest,
			RetryParameters:     td.RetryParameters,
			CleanupStepsBundles: bundleCleanup,
		}
		tests = append(tests, &test)
	}

	return tests, nil
}

// NewJobFromDescriptor creates a job object from a job descriptor
func NewJobFromDescriptor(ctx xcontext.Context, registry *pluginregistry.PluginRegistry, jobDescriptor *job.Descriptor) (*job.Job, error) {
	resolver := fetcherStepsResolver{jobDescriptor: jobDescriptor, registry: registry}
	return newJob(ctx, registry, jobDescriptor, resolver)
}

// NewJobFromExtendedDescriptor creates a job object from an extended job descriptor
func NewJobFromExtendedDescriptor(ctx xcontext.Context, registry *pluginregistry.PluginRegistry, jobDescriptor *job.ExtendedDescriptor) (*job.Job, error) {
	resolver := literalStepsResolver{stepsDescriptors: jobDescriptor.TestStepsDescriptors}
	return newJob(ctx, registry, &jobDescriptor.Descriptor, resolver)
}

// NewJobFromJSONDescriptor builds a descriptor object from a JSON serialization
func NewJobFromJSONDescriptor(ctx xcontext.Context, registry *pluginregistry.PluginRegistry, jobDescriptorJSON string) (*job.Job, error) {
	var jd *job.Descriptor
	if err := json.Unmarshal([]byte(jobDescriptorJSON), &jd); err != nil {
		return nil, err
	}

	j, err := NewJobFromDescriptor(ctx, registry, jd)
	if err != nil {
		return nil, err
	}
	return j, nil
}
