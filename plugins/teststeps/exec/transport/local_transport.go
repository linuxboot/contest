// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build unsafe
// +build unsafe

package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/linuxboot/contest/pkg/logging"
)

type LocalTransport struct{}

func NewLocalTransport() Transport {
	return &LocalTransport{}
}

func (lt *LocalTransport) NewProcess(ctx context.Context, bin string, args []string) (Process, error) {
	return newLocalProcess(ctx, bin, args)
}

// localProcess is just a thin layer over exec.Command
type localProcess struct {
	cmd *exec.Cmd
}

func newLocalProcess(ctx context.Context, bin string, args []string) (Process, error) {
	if err := checkBinary(bin); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	return &localProcess{cmd}, nil
}

func (lp *localProcess) Start(ctx context.Context) error {
	logging.Debugf(ctx, "starting local binary: %v", lp)
	if err := lp.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	return nil
}

func (lp *localProcess) Wait(_ context.Context) error {
	if err := lp.cmd.Wait(); err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) {
			return &ExitError{e.ExitCode()}
		}

		return fmt.Errorf("failed to wait on process: %w", err)
	}

	return nil
}

func (lp *localProcess) StdoutPipe() (io.Reader, error) {
	stdout, err := lp.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe")
	}
	return stdout, nil
}

func (lp *localProcess) StderrPipe() (io.Reader, error) {
	stderr, err := lp.cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe")
	}
	return stderr, nil
}

func (lp *localProcess) String() string {
	return lp.cmd.String()
}
