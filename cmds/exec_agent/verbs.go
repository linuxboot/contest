// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/linuxboot/contest/pkg/remote"
	"github.com/linuxboot/contest/pkg/xcontext"
)

func run() error {
	verb := strings.ToLower(flagSet.Arg(0))
	if verb == "" {
		return fmt.Errorf("missing verb, see --help")
	}

	switch verb {
	case "start":
		bin := flagSet.Arg(1)
		if bin == "" {
			return fmt.Errorf("missing binary argument, see --help")
		}

		var args []string
		if flagSet.NArg() > 2 {
			args = flagSet.Args()[2:]
		}
		return start(bin, args)

	case "poll":
		pid, err := strconv.Atoi(flagSet.Arg(1))
		if err != nil {
			return fmt.Errorf("failed to parse exec id: %w", err)
		}

		return poll(pid)

	case "kill":
		pid, err := strconv.Atoi(flagSet.Arg(1))
		if err != nil {
			return fmt.Errorf("failed to parse exec id: %w", err)
		}

		return kill(pid)

	case "reap":
		pid, err := strconv.Atoi(flagSet.Arg(1))
		if err != nil {
			return fmt.Errorf("failed to parse exec id: %w", err)
		}

		return reap(pid)

	default:
		return fmt.Errorf("invalid verb: %s", verb)
	}
}

func start(bin string, args []string) error {
	ctx := xcontext.Background()

	if flagTimeQuota != nil && *flagTimeQuota != 0 {
		var cancel xcontext.CancelFunc
		ctx, cancel = xcontext.WithTimeout(ctx, *flagTimeQuota)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, bin, args...)

	var stdout, stderr remote.SafeBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("starting command: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// send back the session id
	err := remote.SendResponse(&remote.StartMessage{
		SessionID: strconv.Itoa(cmd.Process.Pid),
	})
	if err != nil {
		return fmt.Errorf("could not send session id: %w", err)
	}

	// start monitoring the cmd, also wait on the process
	mon := remote.NewMonitor(cmd, &stdout, &stderr)
	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- mon.Start(ctx)
	}()

	// start unix socket server to handle process state requests
	server := remote.NewMonitorServer(mon)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve()
	}()

	// catch termination signals
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case err := <-monitorDone:
			if err := server.Shutdown(); err != nil {
				log.Printf("failed to shutdown monitor server: %v", err)
			}
			return err

		case sig := <-sigs:
			if err := server.Shutdown(); err != nil {
				log.Printf("failed to shutdown monitor server: %v", err)
			}
			if err := mon.Kill(); err != nil {
				log.Printf("failed to kill monitored process: %v", err)
			}
			return fmt.Errorf("signal caught: %v", sig)

		case err := <-serverDone:
			if err := mon.Kill(); err != nil {
				log.Printf("failed to kill monitored process: %v", err)
			}
			return err
		}
	}
}

func poll(pid int) error {
	c := remote.NewMonitorClient(pid)

	msg, err := c.Poll()
	if err != nil {
		// connection errors also means that the process or agent might have died
		var e *remote.ErrCantConnect
		if errors.As(err, &e) {
			return remote.SendResponse(&remote.PollMessage{
				Error: "agent is dead or exceeded time quota",
			})
		}

		return fmt.Errorf("failed to call monitor: %w", err)
	}

	return remote.SendResponse(msg)
}

func kill(pid int) error {
	c := remote.NewMonitorClient(pid)
	return c.Kill()
}

func reap(pid int) error {
	c := remote.NewMonitorClient(pid)
	return c.Reap()
}
