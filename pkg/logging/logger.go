// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package logging

import (
	"context"

	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
)

// Trace is an shorthand for logger.FromCtx(ctx).Trace
func Trace(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Trace(args...)
}

// Tracef is an shorthand for logger.FromCtx(ctx).Tracef
func Tracef(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Tracef(format, args...)
}

// TraceFields is an shorthand for logger.FromCtx(ctx).TraceFields
func TraceFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).TraceFields(message, fields)
}

// Debug is an shorthand for logger.FromCtx(ctx).Debug
func Debug(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Debug(args...)
}

// Debugf is an shorthand for logger.FromCtx(ctx).Debugf
func Debugf(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Debugf(format, args...)
}

// DebugFields is an shorthand for logger.FromCtx(ctx).DebugFields
func DebugFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).DebugFields(message, fields)
}

// Info is an shorthand for logger.FromCtx(ctx).Info
func Info(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Info(args...)
}

// Infof is an shorthand for logger.FromCtx(ctx).Infof
func Infof(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Infof(format, args...)
}

// InfoFields is an shorthand for logger.FromCtx(ctx).InfoFields
func InfoFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).InfoFields(message, fields)
}

// Warn is an shorthand for logger.FromCtx(ctx).Warn
func Warn(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Warn(args...)
}

// Warnf is an shorthand for logger.FromCtx(ctx).Warnf
func Warnf(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Warnf(format, args...)
}

// WarnFields is an shorthand for logger.FromCtx(ctx).WarnFields
func WarnFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).WarnFields(message, fields)
}

// Error is an shorthand for logger.FromCtx(ctx).Error
func Error(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Error(args...)
}

// Errorf is an shorthand for logger.FromCtx(ctx).Errorf
func Errorf(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Errorf(format, args...)
}

// ErrorFields is an shorthand for logger.FromCtx(ctx).ErrorFields
func ErrorFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).ErrorFields(message, fields)
}

// Panic is an shorthand for logger.FromCtx(ctx).Panic
func Panic(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Panic(args...)
}

// Panicf is an shorthand for logger.FromCtx(ctx).Panicf
func Panicf(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Panicf(format, args...)
}

// PanicFields is an shorthand for logger.FromCtx(ctx).PanicFields
func PanicFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).PanicFields(message, fields)
}

// Fatal is an shorthand for logger.FromCtx(ctx).Fatal
func Fatal(ctx context.Context, args ...any) {
	logger.FromCtx(ctx).Fatal(args...)
}

// Fatalf is an shorthand for logger.FromCtx(ctx).Fatalf
func Fatalf(ctx context.Context, format string, args ...any) {
	logger.FromCtx(ctx).Fatalf(format, args...)
}

// FatalFields is an shorthand for logger.FromCtx(ctx).FatalFields
func FatalFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FromCtx(ctx).FatalFields(message, fields)
}
