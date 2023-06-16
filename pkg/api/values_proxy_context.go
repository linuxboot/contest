// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package api

import (
	"context"
	"time"
)

type valuesProxyContext struct {
	valueser context.Context
}

var _ context.Context = (*valuesProxyContext)(nil)

// Deadline implements interface context.Context.
func (ctx *valuesProxyContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// Done implements interface context.Context.
func (ctx *valuesProxyContext) Done() <-chan struct{} {
	return nil
}

// Err implements interface context.Context.
func (ctx *valuesProxyContext) Err() error {
	return nil
}

// Value implements interface context.Context.
func (ctx *valuesProxyContext) Value(key any) any {
	return ctx.valueser.Value(key)
}

// newValuesProxyContext returns a context without cancellation/deadline signals,
// but with all the values kept as is.
func newValuesProxyContext(ctx context.Context) context.Context {
	ctx = &valuesProxyContext{
		valueser: ctx,
	}
	return ctx
}
