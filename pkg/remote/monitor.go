// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package remote

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/exec"
	"sync"
)

type Monitor struct {
	cmd *exec.Cmd

	stdout   LenReader
	stderr   LenReader
	exitCode SafeExitCode

	reaper *SafeSignal
}

func NewMonitor(cmd *exec.Cmd, stdout LenReader, stderr LenReader) *Monitor {
	reaper := newSafeSignal()

	return &Monitor{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
		reaper: reaper,
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()

		// got cancelled; don't wait for a controller to reap
		m.reaper.Signal()
	}()

	err := m.cmd.Wait()
	if err == nil {
		log.Print("process finished")
		m.exitCode.Store(0)
	} else {
		log.Printf("process exited with err: %v", err)

		var ee *exec.ExitError
		if errors.As(err, &ee) {
			m.exitCode.Store(ee.ExitCode())
		}
	}

	// wait for controller to read all data then signal the reap
	m.reaper.Wait()
	return err
}

func (m *Monitor) Kill() error {
	if m.exitCode.Load() == nil {
		return m.cmd.Process.Kill()
	}

	return nil
}

func (m *Monitor) newRPC() *monitorRPC {
	return &monitorRPC{mon: m}
}

func (m *Monitor) pollFds() ([]byte, []byte, error) {
	stdout := make([]byte, m.stdout.Len())
	if _, err := m.stdout.Read(stdout); err != nil {
		return nil, nil, fmt.Errorf("failed to read stdout: %w", err)
	}

	stderr := make([]byte, m.stderr.Len())
	if _, err := m.stderr.Read(stderr); err != nil {
		return nil, nil, fmt.Errorf("failed to read stderr: %w", err)
	}

	return stdout, stderr, nil
}

type PollReply struct {
	Stdout []byte
	Stderr []byte

	ExitCode int
	Alive    bool
}

// monitorRPC is a tightly coupled view into Monitor that can be used
// as a server handler for RPC calls
type monitorRPC struct {
	mon *Monitor

	// there really shouldnt be multiple concurrent consumers
	// by design, but better be safe than sorry
	mu sync.Mutex
}

func (rpc *monitorRPC) Poll(_ int, reply *PollReply) error {

	log.Printf("got a call for: poll")
	rpc.mu.Lock()
	defer rpc.mu.Unlock()

	stdout, stderr, err := rpc.mon.pollFds()
	if err != nil {
		return err
	}

	reply.Stdout = stdout
	reply.Stderr = stderr

	code := rpc.mon.exitCode.Load()
	if code == nil {
		reply.Alive = true
	} else {
		reply.ExitCode = *code
	}

	return nil
}

func (rpc *monitorRPC) Kill(_ int, _ *interface{}) error {
	log.Print("got a call for: kill")
	rpc.mu.Lock()
	defer rpc.mu.Unlock()

	if err := rpc.mon.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}
	return nil
}

func (rpc *monitorRPC) Reap(_ int, _ *interface{}) error {
	log.Print("got a call for: reap")
	rpc.mu.Lock()
	defer rpc.mu.Unlock()

	rpc.mon.reaper.Signal()
	return nil
}

const sockFormat = "/tmp/exec_bin_sock_%d"

type MonitorServer struct {
	addr string
	rpc  *monitorRPC

	http *http.Server
}

func NewMonitorServer(mon *Monitor) *MonitorServer {
	return &MonitorServer{
		addr: fmt.Sprintf(sockFormat, mon.cmd.Process.Pid),
		rpc:  mon.newRPC(),
	}
}

func (m *MonitorServer) Serve() error {
	log.Printf("starting monitor...")

	if err := os.RemoveAll(m.addr); err != nil {
		return fmt.Errorf("failed to clear lingering socket %s: %w", m.addr, err)
	}

	listener, err := net.Listen("unix", m.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", m.addr, err)
	}
	defer listener.Close()

	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("rpc", m.rpc); err != nil {
		return fmt.Errorf("failed to register rpc api: %v", err)
	}

	log.Printf("starting RPC server at: %s", m.addr)
	m.http = &http.Server{
		Addr:    m.addr,
		Handler: rpcServer,
	}
	return m.http.Serve(listener)
}

func (m *MonitorServer) Shutdown() error {
	log.Printf("shutting down monitor...")

	if err := os.RemoveAll(m.addr); err != nil {
		return fmt.Errorf("failed to remove unix socket %s: %w", m.addr, err)
	}

	if m.http != nil {
		// dont care about cancellation context
		return m.http.Shutdown(context.Background())
	}
	return nil
}

type ErrCantConnect struct {
	w error
}

func (e ErrCantConnect) Error() string {
	return e.w.Error()
}

func (e ErrCantConnect) Unwrap() error {
	return e.w
}

type MonitorClient struct {
	addr string
}

func NewMonitorClient(pid int) *MonitorClient {
	addr := fmt.Sprintf(sockFormat, pid)
	return &MonitorClient{addr}
}

func (m *MonitorClient) Poll() (*PollMessage, error) {
	client, err := rpc.DialHTTP("unix", m.addr)
	if err != nil {
		return nil, &ErrCantConnect{fmt.Errorf("failed to connect to %s: %w", m.addr, err)}
	}
	defer client.Close()

	var reply PollReply
	if err := client.Call("rpc.Poll", 0, &reply); err != nil {
		return nil, fmt.Errorf("failed to call rpc method: %w", err)
	}

	var code *int
	if !reply.Alive {
		code = &reply.ExitCode
	}

	return &PollMessage{
		Stdout:   string(reply.Stdout),
		Stderr:   string(reply.Stderr),
		ExitCode: code,
	}, nil
}

func (m *MonitorClient) Kill() error {
	client, err := rpc.DialHTTP("unix", m.addr)
	if err != nil {
		return &ErrCantConnect{fmt.Errorf("failed to connect to %s: %w", m.addr, err)}
	}
	defer client.Close()

	var reply interface{}
	if err := client.Call("rpc.Kill", 0, &reply); err != nil {
		return fmt.Errorf("failed to call rpc method: %w", err)
	}

	return nil
}

func (m *MonitorClient) Reap() error {
	client, err := rpc.DialHTTP("unix", m.addr)
	if err != nil {
		return &ErrCantConnect{fmt.Errorf("failed to connect to %s: %w", m.addr, err)}
	}
	defer client.Close()

	var reply interface{}
	if err := client.Call("rpc.Reap", 0, &reply); err != nil {
		return fmt.Errorf("failed to call rpc method: %w", err)
	}

	return nil
}
