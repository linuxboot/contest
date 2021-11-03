// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package remote

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSafeSignalSimple(t *testing.T) {
	s := newSafeSignal()
	c := make(chan struct{})

	go func() {
		s.Wait()
		close(c)
	}()

	s.Signal()

	select {
	case <-c:
		return

	case <-time.After(time.Second):
		t.Fail()
	}
}

func TestSafeSignalMultipleSignal(t *testing.T) {
	s := newSafeSignal()

	// should not panic on multiple signal calls
	defer func() {
		if err := recover(); err != nil {
			t.Fail()
		}
	}()

	s.Signal()
	s.Signal()
}

func TestSafeBufferSimple(t *testing.T) {
	var b SafeBuffer
	require.Equal(t, 0, b.Len())

	data := []byte{1, 2, 3}
	n, err := b.Write(data)

	require.NoError(t, err)
	require.Equal(t, len(data), n)

	read := make([]byte, len(data))
	n, err = b.Read(read)

	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Equal(t, read, data)
}

func TestSafeExitCodeSimple(t *testing.T) {
	var s SafeExitCode
	require.Equal(t, (*int)(nil), s.Load())

	s.Store(42)
	require.Equal(t, 42, *s.Load())
}
