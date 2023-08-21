package transport

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

type deferedStack struct {
	funcs []func()

	closed bool
	done   chan struct{}

	mu sync.Mutex
}

func newDeferedStack() *deferedStack {
	s := &deferedStack{nil, false, make(chan struct{}), sync.Mutex{}}

	go func() {
		<-s.done
		for i := len(s.funcs) - 1; i >= 0; i-- {
			s.funcs[i]()
		}
	}()

	return s
}

func (s *deferedStack) Add(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.funcs = append(s.funcs, f)
}

func (s *deferedStack) Done() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	close(s.done)
	s.closed = true
}

func canExecute(fi os.FileInfo) bool {
	// TODO: deal with acls?
	stat := fi.Sys().(*syscall.Stat_t)
	if stat.Uid == uint32(os.Getuid()) {
		return stat.Mode&0o500 == 0o500
	}

	if stat.Gid == uint32(os.Getgid()) {
		return stat.Mode&0o050 == 0o050
	}

	return stat.Mode&0o005 == 0o005
}

func checkBinary(bin string) error {
	// check binary exists and is executable
	fi, err := os.Stat(bin)
	if err != nil {
		return fmt.Errorf("no such file: %s", bin)
	}

	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a file: %s", bin)
	}

	if !canExecute(fi) {
		return fmt.Errorf("provided binary %s is not executable", bin)
	}
	return nil
}
