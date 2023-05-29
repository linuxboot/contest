// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package storage

import (
	"context"

	"github.com/linuxboot/contest/pkg/logging"
)

// ConsistencyModel hints at whether queries should go to the primary database
// or any available replica (in which case, the guarantee is eventual consistency)
type ConsistencyModel int

const (
	ConsistentReadAfterWrite ConsistencyModel = iota
	ConsistentEventually
)

type consistencyModelKeyT string

const consistencyModelKey = consistencyModelKeyT("storage_consistency_model")

// Storage defines the interface that storage engines must implement
type Storage interface {
	JobStorage
	EventStorage

	// Close flushes and releases resources associated with the storage engine.
	Close() error

	// Version returns the version of the storage being used
	Version() (uint64, error)
}

// TransactionalStorage is implemented by storage backends that support transactions.
// Only default isolation level is supported.
type TransactionalStorage interface {
	Storage
	BeginTx() (TransactionalStorage, error)
	Commit() error
	Rollback() error
}

// ResettableStorage is implemented by storage engines that support reset operation
type ResettableStorage interface {
	Storage
	Reset() error
}

func isStronglyConsistent(ctx context.Context) bool {
	value := ctx.Value(consistencyModelKey)
	logging.Debugf(ctx, "consistency model check: %v", value)

	switch model := value.(type) {
	case ConsistencyModel:
		return model == ConsistentReadAfterWrite

	default:
		return true
	}
}

func WithConsistencyModel(ctx context.Context, model ConsistencyModel) context.Context {
	return context.WithValue(ctx, consistencyModelKey, model)
}
