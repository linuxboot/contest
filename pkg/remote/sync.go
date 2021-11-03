// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package remote

import (
	"bytes"
	"io"
	"sync"
)

// SafeSignal is a goroutine safe signalling mechanism
// It can have multiple goroutines that trigger the signal without
// interfering with eachother (or crashing as using a channel would)
// Currently designed for a single waiter goroutine.
type SafeSignal struct {
	sync.Mutex
	done bool
	c    *sync.Cond
}

func newSafeSignal() *SafeSignal {
	s := &SafeSignal{}
	s.c = &sync.Cond{L: s}
	return s
}

func (s *SafeSignal) Signal() {
	s.Lock()
	s.done = true
	s.Unlock()

	s.c.Signal()
}

func (s *SafeSignal) Wait() {
	s.Lock()
	defer s.Unlock()

	for !s.done {
		s.c.Wait()
	}
}

type SafeBuffer struct {
	b  bytes.Buffer
	mu sync.Mutex
}

func (sb *SafeBuffer) Write(data []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.b.Write(data)
}

func (sb *SafeBuffer) Read(data []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.b.Read(data)
}

func (sb *SafeBuffer) Len() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	return sb.b.Len()
}

type LenReader interface {
	io.Reader
	Len() int
}

// SafeExitCode is a sync storage for the exit code of a process
// the initial value of nil cannot be reset after the first set
type SafeExitCode struct {
	exitCode *int

	mu sync.Mutex
}

func (s *SafeExitCode) Store(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.exitCode = &code
}

func (s *SafeExitCode) Load() *int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exitCode
}
