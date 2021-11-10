// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package storage

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStorageEngineVault(t *testing.T) {
	const storageName = "TestStorageEngineVault"
	vault := GetStorageEngineVault()

	// Nothing here upon creation
	engine, err := vault.GetEngine(storageName)
	require.Nil(t, engine)
	require.Error(t, err)

	require.NoError(t, GetStorageEngineVault().StoreEngine(&nullStorage{}, storageName))
	engine, err = vault.GetEngine(storageName)
	require.NotNil(t, engine)
	require.NoError(t, err)
}
