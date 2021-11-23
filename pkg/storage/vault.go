// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// storage emitterVault is used to define the storage engines used by ConTest.
// Engines can be overridden via the exported function StoreEngine.

package storage

import (
	"fmt"
	"github.com/linuxboot/contest/pkg/config"
)

type EngineType uint32

const (
	UnknownEngine EngineType = iota
	SyncEngine
	AsyncEngine
)

type EngineVault interface {
	// GetEngine - fetch the engine of selected type from the emitterVault
	GetEngine(EngineType) (Storage, error)
}

type EngineVaultMap map[EngineType]Storage

type SimpleEngineVault struct {
	vault EngineVaultMap
}

// GetEngine - get storage engine from the vault. Defaults to SyncEngine.
func (v *SimpleEngineVault) GetEngine(engineType EngineType) (Storage, error) {
	var found bool
	var err error
	var engine Storage
	if engine, found = v.vault[engineType]; !found {
		err = fmt.Errorf("storage #{engineType} not assigned")
	}
	return engine, err
}

// StoreEngine - store supplied engine in the emitterVault. As SyncEngine by default
// Switching to a new storage engine implies garbage collecting the old one,
// with possible loss of pending events if not flushed correctly
func (v *SimpleEngineVault) StoreEngine(storageEngine Storage, engineType EngineType) error {
	var err error
	if storageEngine != nil {
		var ver uint64
		if ver, err = storageEngine.Version(); err != nil {
			err = fmt.Errorf("could not determine storage version: %w", err)
			return err
		}

		if ver < config.MinStorageVersion {
			err = fmt.Errorf("could not configure storage of type %T (minimum storage version: %d, current storage version: %d)", storageEngine, config.MinStorageVersion, ver)
			return err
		}

		v.vault[engineType] = storageEngine
	} else {
		delete(v.vault, engineType)
	}

	return err
}

// NewSimpleEngineVault - returns a new instance of SimpleEngineVault
func NewSimpleEngineVault() *SimpleEngineVault {
	return &SimpleEngineVault{vault: make(EngineVaultMap)}
}
