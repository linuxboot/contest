// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import "context"

// TestFetcherFactory is a type representing a function which builds
// a TestFetcher
type TestFetcherFactory func() TestFetcher

// TestFetcherLoader is a type representing a function which returns all the
// needed things to be able to load a TestFetcher
type TestFetcherLoader func() (string, TestFetcherFactory)

// TestFetcher is an interface used to get the test to run on the selected
// hosts.
type TestFetcher interface {
	ValidateFetchParameters(context.Context, []byte, bool) (interface{}, error)
	Fetch(context.Context, interface{}) (string, []*TestStepDescriptor, error)
}

// TestFetcherBundle bundles the selected TestFetcher together with its acquire
// and release parameters based on the content of the job descriptor.
// The bundle contains also the selected TestFetcher for the cleanup steps, together
// with its parameters.
type TestFetcherBundle struct {
	TestFetcher       TestFetcher
	FetchParameters   interface{}
	CleanupFetcher    TestFetcher
	CleanupParameters interface{}
}
