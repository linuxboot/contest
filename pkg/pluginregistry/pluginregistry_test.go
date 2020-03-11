// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package pluginregistry

import (
	"testing"
	"time"

	"github.com/facebookincubator/contest/pkg/event"
	"github.com/facebookincubator/contest/pkg/target"
	"github.com/facebookincubator/contest/pkg/test"
	targetLockerNoop "github.com/facebookincubator/contest/plugins/targetlocker/noop"
	"golang.org/x/xerrors"

	"github.com/stretchr/testify/require"
)

type testStepFactoryGood struct{}

func (f *testStepFactoryGood) New() test.TestStep               { return nil }
func (f *testStepFactoryGood) Events() []event.Name             { return []event.Name{"validEventName"} }
func (f *testStepFactoryGood) UniqueImplementationName() string { return "unit-test-positive" }

type testStepFactoryBadEventName struct{}

func (f *testStepFactoryBadEventName) New() test.TestStep   { return nil }
func (f *testStepFactoryBadEventName) Events() []event.Name { return []event.Name{"invalid event name"} }
func (f *testStepFactoryBadEventName) UniqueImplementationName() string {
	return "unit-test-neg-invalid-event-name"
}

func TestRegisterTestStepEvents(t *testing.T) {
	pr := NewPluginRegistry()
	t.Run("positive", func(t *testing.T) {
		err := pr.RegisterFactory(&testStepFactoryGood{})
		require.NoError(t, err)
	})
	t.Run("negative", func(t *testing.T) {
		t.Run("invalid_event_name", func(t *testing.T) {
			err := pr.RegisterFactory(&testStepFactoryBadEventName{})
			require.Error(t, err)
			require.True(t, xerrors.As(err, &event.ErrInvalidEventName{}))
		})
	})
}

type fakeFactory struct{}

func (f *fakeFactory) UniqueImplementationName() string { return "unit-test" }

func TestNewPluginRegistry_RegisterFactory(t *testing.T) {
	pr := NewPluginRegistry()
	t.Run("positive", func(t *testing.T) {
		err := pr.RegisterFactories(target.LockerFactories{&targetLockerNoop.Factory{}}.ToAbstract())
		require.NoError(t, err)
		targetLocker, err := pr.NewTargetLocker("noop", time.Second, "")
		require.NoError(t, err)
		require.NotNil(t, targetLocker)
	})
	t.Run("negative", func(t *testing.T) {
		t.Run("UnknownFactoryType_unknownFactory", func(t *testing.T) {
			err := pr.RegisterFactory(&fakeFactory{})
			require.True(t, xerrors.As(err, &ErrUnknownFactoryType{}))
		})
		t.Run("UnknownFactoryType_nilFactory", func(t *testing.T) {
			err := pr.RegisterFactory(nil)
			require.Error(t, err)
			require.True(t, xerrors.As(err, &ErrUnknownFactoryType{}), err)
		})
	})
}
