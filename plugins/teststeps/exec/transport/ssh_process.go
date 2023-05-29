// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/linuxboot/contest/pkg/logging"

	"golang.org/x/crypto/ssh"
)

type sshProcess struct {
	session       *ssh.Session
	cmd           string
	keepAliveDone chan struct{}

	stack *deferedStack
}

func newSSHProcess(ctx context.Context, client *ssh.Client, bin string, args []string, stack *deferedStack) (Process, error) {
	var stdin bytes.Buffer
	return newSSHProcessWithStdin(ctx, client, bin, args, &stdin, stack)
}

func newSSHProcessWithStdin(
	ctx context.Context, client *ssh.Client,
	bin string, args []string,
	stdin io.Reader,
	stack *deferedStack,
) (Process, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("cannot create SSH session to server: %v", err)
	}

	// set fds for the remote process
	session.Stdin = stdin

	cmd := shellquote.Join(append([]string{bin}, args...)...)
	keepAliveDone := make(chan struct{})

	return &sshProcess{session, cmd, keepAliveDone, stack}, nil
}

func (sp *sshProcess) Start(ctx context.Context) error {
	logging.Debugf(ctx, "starting remote binary: %s", sp.cmd)
	if err := sp.session.Start(sp.cmd); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	go func() {
		for {
			select {
			case <-sp.keepAliveDone:
				return

			case <-time.After(5 * time.Second):
				logging.Debugf(ctx, "sending sigcont to ssh server...")
				if err := sp.session.Signal(ssh.Signal("CONT")); err != nil {
					logging.Warnf(ctx, "failed to send CONT to ssh server: %w", err)
				}

			case <-ctx.Done():
				logging.Debugf(ctx, "killing ssh session because of cancellation...")

				// TODO:  figure out if there's a way to fix this (can be used for resource exhaustion)
				// note: not all servers implement the signal message so this might
				// not do anything; see comment about cancellation in Wait()
				if err := sp.session.Signal(ssh.SIGKILL); err != nil {
					logging.Warnf(ctx, "failed to send KILL on context cancel: %w", err)
				}

				sp.session.Close()
				return
			}
		}
	}()

	return nil
}

func (sp *sshProcess) Wait(ctx context.Context) error {
	// close these no matter what error we get from the wait
	defer func() {
		sp.stack.Done()
		close(sp.keepAliveDone)
	}()
	defer sp.session.Close()

	errChan := make(chan error, 1)
	go func() {
		if err := sp.session.Wait(); err != nil {
			var e *ssh.ExitError
			if errors.As(err, &e) {
				errChan <- &ExitError{e.ExitStatus()}
				return
			}

			errChan <- fmt.Errorf("failed to wait on process: %w", err)
		}
		errChan <- nil
	}()

	select {
	case <-ctx.Done():
		// cancellation was requested, a kill signal should've been sent but not
		// all ssh server implementations respect that, so in the worst case scenario
		// we just disconnect the ssh and leave the remote process to terminate by
		// itself (pid is also unavailable thru the ssh spec)

		// leave the process some time to exit in case the signal did work
		select {
		case <-time.After(3 * time.Second):
			return ctx.Err()

		case err := <-errChan:
			return err
		}

	case err := <-errChan:
		return err
	}
}

func (sp *sshProcess) StdoutPipe() (io.Reader, error) {
	stdout, err := sp.session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe")
	}

	return stdout, nil
}

func (sp *sshProcess) StderrPipe() (io.Reader, error) {
	stderr, err := sp.session.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe")
	}

	return stderr, nil
}

func (sp *sshProcess) String() string {
	return sp.cmd
}
