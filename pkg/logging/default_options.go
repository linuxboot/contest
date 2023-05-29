// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package logging

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus/formatter"
)

// DefaultOptions is a set options recommended to use by default.
func WithBelt(
	ctx context.Context,
	logLevel logger.Level,
) context.Context {
	l := logrus.DefaultLogrusLogger()
	l.Formatter = &formatter.CompactText{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
	}
	ctx = logger.CtxWithLogger(ctx, logrus.New(l).WithLevel(logLevel))
	return ctx
}
