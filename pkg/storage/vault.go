// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// storage vault is used to define the storage engines used by ConTest.
// Engines can be overridden via the exported function StoreEngine.

package storage

import (
	"fmt"
	"sync"

	"github.com/linuxboot/contest/pkg/config"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type EngineType int

const (
	SyncEngine EngineType = iota
	AsyncEngine
)

const kCtxEngineVaultKey = "StorageEngineVault"

type EngineVault interface {
	// Clear - erase everything from the vault
	Clear()

	// GetEngine - fetch the engine of selected type from the vault
	GetEngine(EngineType) (Storage, error)

	// StoreEngine - put the engine of selected type to the vault, replace it, or delete it if nil given
	StoreEngine(Storage, EngineType) error
}

type EngineVaultMap map[EngineType]Storage

type SynchronizedEngineVault struct {
	sync.RWMutex
	vault EngineVaultMap
}

// GetEngine - get storage engine from the vault. Defaults to SyncEngine.
func (v *SynchronizedEngineVault) GetEngine(engineType EngineType) (res Storage, err error) {
	v.RLock()
	defer v.RUnlock()

	var found bool
	if res, found = v.vault[engineType]; !found {
		err = fmt.Errorf("storage %d not assigned", engineType)
	}
	return
}

// StoreEngine - store supplied engine in the vault. As SyncEngine by default
// Switching to a new storage engine implies garbage collecting the old one,
// with possible loss of pending events if not flushed correctly
func (v *SynchronizedEngineVault) StoreEngine(storageEngine Storage, engineType EngineType) (err error) {
	v.Lock()
	defer v.Unlock()

	if storageEngine != nil {
		var ver uint64
		if ver, err = storageEngine.Version(); err != nil {
			err = fmt.Errorf("could not determine storage version: %w", err)
			return
		}

		if ver < config.MinStorageVersion {
			err = fmt.Errorf("could not configure storage of type %T (minimum storage version: %d, current storage version: %d)", storageEngine, config.MinStorageVersion, ver)
			return
		}

		v.vault[engineType] = storageEngine
	} else {
		delete(v.vault, engineType)
	}

	return
}

// Clear - remove everything from the vault
func (v *SynchronizedEngineVault) Clear() {
	v.Lock()
	defer v.Unlock()

	for k := range v.vault {
		delete(v.vault, k)
	}
}

// NewStorageEngineVault - returns a new instance of SynchronizedEngineVault
func NewStorageEngineVault() EngineVault {
	return &SynchronizedEngineVault{vault: make(EngineVaultMap)}
}

// WithStorageEngineVault - appends EngineVault-object to the given context
func WithStorageEngineVault(ctx xcontext.Context, vault EngineVault) xcontext.Context {
	return xcontext.WithValue(ctx, kCtxEngineVaultKey, vault)
}

// GetStorageEngineVaultFromContext - extracts EngineVault-object from the given context
func GetStorageEngineVaultFromContext(ctx xcontext.Context) EngineVault {
	value, ok := ctx.Value(kCtxEngineVaultKey).(EngineVault)
	if !ok {
		return nil
	}

	return value
}

// GetStorageEngineFromContext - extracts EngineVault-object from the given context
// and tries to extract an engine of specified type from it
func GetStorageEngineFromContext(ctx xcontext.Context, engineType EngineType) (Storage, error) {
	vault := GetStorageEngineVaultFromContext(ctx)
	if vault == nil {
		return nil, fmt.Errorf("no StorageVault found in the context")
	}

	return vault.GetEngine(engineType)
}
