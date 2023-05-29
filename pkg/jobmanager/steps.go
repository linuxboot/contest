// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package jobmanager

import (
	"context"

	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/test"
)

// stepsResolver is an interface which determines how to fetch TestStepsDescriptors, which could
// have either already been pre-calculated, or built by the TestFetcher.
type stepsResolver interface {
	GetStepsDescriptors(context.Context) ([]test.TestStepsDescriptors, error)
}

type literalStepsResolver struct {
	stepsDescriptors []test.TestStepsDescriptors
}

func (l literalStepsResolver) GetStepsDescriptors(context.Context) ([]test.TestStepsDescriptors, error) {
	return l.stepsDescriptors, nil
}

type fetcherStepsResolver struct {
	jobDescriptor *job.Descriptor
	registry      *pluginregistry.PluginRegistry
}

func (f fetcherStepsResolver) GetStepsDescriptors(ctx context.Context) ([]test.TestStepsDescriptors, error) {
	var descriptors []test.TestStepsDescriptors
	for _, testDescriptor := range f.jobDescriptor.TestDescriptors {
		bundleTestFetcher, err := f.registry.NewTestFetcherBundle(ctx, testDescriptor)
		if err != nil {
			return nil, err
		}

		testName, stepDescriptors, err := bundleTestFetcher.TestFetcher.Fetch(ctx, bundleTestFetcher.FetchParameters)
		if err != nil {
			return nil, err
		}

		var cleanupDescriptors []*test.TestStepDescriptor
		if bundleTestFetcher.CleanupFetcher != nil {
			_, cleanupDescriptors, err = bundleTestFetcher.CleanupFetcher.Fetch(ctx, bundleTestFetcher.CleanupParameters)
			if err != nil {
				return nil, err
			}
		}

		descriptors = append(
			descriptors,
			test.TestStepsDescriptors{
				TestName:     testName,
				TestSteps:    stepDescriptors,
				CleanupSteps: cleanupDescriptors,
			},
		)
	}

	return descriptors, nil
}
