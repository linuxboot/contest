// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type singleSilentEngineVaultProvider struct {
	singleEngine Storage
}

func (v *singleSilentEngineVaultProvider) Init() {
	v.singleEngine = &nullStorage{}
}

func (v *singleSilentEngineVaultProvider) Clear() {
	v.singleEngine = nil
}

func (v *singleSilentEngineVaultProvider) GetEngine(_ EngineType) (Storage, error) {
	return v.singleEngine, nil
}

func (v *singleSilentEngineVaultProvider) StoreEngine(storageEngine Storage, engineType EngineType) error {
	return nil
}

func TestStorageEngineVault(t *testing.T) {
	const engineId = AsyncEngine

	vault := NewSimpleEngineVault()

	// Nothing here upon creation
	engine, err := vault.GetEngine(engineId)
	require.Nil(t, engine)
	require.Error(t, err)

	// Create engine
	require.NoError(t, vault.StoreEngine(&nullStorage{}, engineId))
	engine, err = vault.GetEngine(engineId)
	require.NotNil(t, engine)
	require.NoError(t, err)

	// Delete engine
	require.NoError(t, vault.StoreEngine(nil, engineId))
	engine, err = vault.GetEngine(engineId)
	require.Nil(t, engine)
	require.Error(t, err)
}

func TestStorageEngineVaultClearing(t *testing.T) {
	vault := NewSimpleEngineVault()

	types := []EngineType{SyncEngine, AsyncEngine}

	for _, engineType := range types {
		require.NoError(t, vault.StoreEngine(&nullStorage{}, engineType))
		engine, err := vault.GetEngine(engineType)
		require.NotNil(t, engine)
		require.NoError(t, err)
	}

	vault = NewSimpleEngineVault()
	for _, engineType := range types {
		engine, err := vault.GetEngine(engineType)
		require.Nil(t, engine)
		require.Error(t, err)
	}
}

func TestCustomEngineVaultProvider(t *testing.T) {
	vault := &singleSilentEngineVaultProvider{}
	vault.Init()

	// Now there should be an engine
	engine, err := vault.GetEngine(SyncEngine)
	require.NotNil(t, engine)
	require.NoError(t, err)

	vault.Clear()
	// Now there should be no engine, but no error also
	engine, err = vault.GetEngine(SyncEngine)
	require.Nil(t, engine)
	require.NoError(t, err)
}
