// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package signaling

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type unitTestCustomSignalType struct {
	ID int
}

func (s unitTestCustomSignalType) Error() string {
	return fmt.Sprintf("unit-test custom signal #%d", s.ID)
}

var unitTestCustomSignal0 = unitTestCustomSignalType{ID: 0}
var unitTestCustomSignal1 = unitTestCustomSignalType{ID: 1}
var unitTestCustomSignal2 = unitTestCustomSignalType{ID: 2}

func TestBackgroundContext(t *testing.T) {
	ctx := context.Background()

	var blocked bool
	select {
	case <-Until(ctx, unitTestCustomSignal0):
	default:
		blocked = true
	}

	require.True(t, blocked)
}

func TestSendSignal(t *testing.T) {
	ctx, pauseFunc := WithSignal(context.Background(), unitTestCustomSignal0)
	require.NotNil(t, ctx)

	pauseFunc()

	<-Until(ctx, unitTestCustomSignal0)

	require.Nil(t, ctx.Err())

	var canceled bool
	select {
	case <-ctx.Done():
		canceled = true
	default:
	}
	require.False(t, canceled)
}

func TestSendMultipleSignals(t *testing.T) {
	ctx := context.Background()
	ctx, sendSignal0 := WithSignal(ctx, unitTestCustomSignal0)
	ctx, sendSignal1 := WithSignal(ctx, unitTestCustomSignal1)
	require.NotNil(t, ctx)

	sendSignal1()
	sendSignal0()
	sendSignal1()
	sendSignal0()

	<-Until(ctx, unitTestCustomSignal0)
	<-Until(ctx, unitTestCustomSignal1)
}

func TestGrandGrandGrandChild(t *testing.T) {
	type myUniqueType string
	ctx0, sendSignal0 := WithSignal(context.Background(), unitTestCustomSignal0)
	ctx1, _ := WithSignal(context.WithValue(ctx0, myUniqueType("someKey1"), "someValue1"), unitTestCustomSignal1)
	ctx2, _ := WithSignal(context.WithValue(ctx1, myUniqueType("someKey2"), "someValue2"), unitTestCustomSignal2)

	r, err := IsSignaledWith(ctx2, unitTestCustomSignal0)
	require.False(t, r)
	require.NoError(t, err)
	sendSignal0()
	<-Until(ctx2, unitTestCustomSignal0)
	r, err = IsSignaledWith(ctx2, unitTestCustomSignal0)
	require.True(t, r)
	require.NoError(t, err)
	r, err = IsSignaledWith(ctx2, unitTestCustomSignal1)
	require.False(t, r)
	require.NoError(t, err)

	select {
	case <-Until(ctx2, unitTestCustomSignal1):
		require.FailNow(t, "unexpected closed chan for signal1")
	case <-Until(ctx2, unitTestCustomSignal2):
		require.FailNow(t, "unexpected closed chan for signal2")
	default:
	}

}
