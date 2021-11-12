// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// +build integration

package test

import (
	"fmt"
	"testing"

	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/plugins/storage/memory"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestTestEventsSuiteMemoryStorage(t *testing.T) {

	testSuite := TestEventsSuite{}
	// Run the TestSuite with memory storage layer
	storagelayer, err := memory.New()
	if err != nil {
		panic(fmt.Sprintf("could not initialize in-memory storage layer: %v", err))
	}
	testSuite.storage = storagelayer

	// Init context with EngineStorageVault
	vault := storage.GetStorageEngineVaultFromContext(ctx)
	if vault == nil {
		vault = storage.NewStorageEngineVault()
		ctx = storage.WithStorageEngineVault(ctx, vault)
	}
	err = vault.StoreEngine(storagelayer, storage.SyncEngine)
	require.NoError(t, err)

	suite.Run(t, &testSuite)
}
