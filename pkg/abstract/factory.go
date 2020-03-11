// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package abstract

import (
	"github.com/facebookincubator/contest/pkg/event"
)

// Factory is the common part of interface for any factory
type Factory interface {
	// UniqueImplementationName returns the Name of the implementation
	// unique among all reachable implementation of specific type of factories
	// (names are case insensitive).
	UniqueImplementationName() string
}

// FactoryWithEvents is an abstract factory which also may emit events
type FactoryWithEvents interface {
	Factory

	// Events is used by the framework to determine which events this plugin will
	// emit. Any emitted event that is not registered here will cause the plugin to
	// fail.
	Events() []event.Name
}
