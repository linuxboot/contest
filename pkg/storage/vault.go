// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// storage vault is used to define the storage engines used by ConTest.
// Engines can be overridden via the exported function StoreEngine.

package storage

import (
	"fmt"
	"github.com/linuxboot/contest/pkg/config"
	"sync"
)

const SyncEngine = "DefaultSync"
const AsyncEngine = "DefaultAsync"

type EngineVaultMap map[string]Storage

type EngineVault struct {
	lock  sync.RWMutex
	vault EngineVaultMap
}

var once sync.Once
var instance *EngineVault

// GetEngine - get storage engine from the vault. Defaults to SyncEngine.
func (v *EngineVault) GetEngine(name ...string) (res Storage, err error) {
	v.lock.RLock()
	defer v.lock.RUnlock()

	var engineName string
	if len(name) > 0 {
		engineName = name[0]
	} else {
		engineName = SyncEngine
	}

	var found bool
	if res, found = v.vault[engineName]; !found {
		err = fmt.Errorf("storage %s not assigned", engineName)
	}
	return
}

// StoreEngine - store supplied engine in the vault. As SyncEngine by default
func (v *EngineVault) StoreEngine(storageEngine Storage, name ...string) (err error) {
	v.lock.Lock()
	defer v.lock.Unlock()

	var engineName string
	if len(name) > 0 {
		engineName = name[0]
	} else {
		engineName = SyncEngine
	}

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

		v.vault[engineName] = storageEngine
	} else {
		delete(v.vault, engineName)
	}

	return
}

func GetStorageEngineVault() *EngineVault {
	once.Do(func() {
		instance = &EngineVault{vault: make(EngineVaultMap)}
	})
	return instance
}
