package bios_certificate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
)

const (
	supportedProto = "ssh"
	privileged     = "sudo"
	cmd            = "cert"
	jsonFlag       = "--json"
)

type outcome error

type Error struct {
	Msg string `json:"error"`
}

type TargetRunner struct {
	ts *TestStep
	ev testevent.Emitter
}

func NewTargetRunner(ts *TestStep, ev testevent.Emitter) *TargetRunner {
	return &TargetRunner{
		ts: ts,
		ev: ev,
	}
}

func (r *TargetRunner) Run(ctx xcontext.Context, target *target.Target) error {
	var stdoutMsg, stderrMsg strings.Builder

	// limit the execution time if specified
	timeout := r.ts.Options.Timeout
	if timeout != 0 {
		var cancel xcontext.CancelFunc
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(timeout))
		defer cancel()
	}

	pe := test.NewParamExpander(target)

	var params inputStepParams
	if err := pe.ExpandObject(r.ts.inputStepParams, &params); err != nil {
		err := fmt.Errorf("failed to expand input parameter: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)

	if params.Transport.Proto != supportedProto {
		err := fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	transportProto, err := transport.NewTransport(params.Transport.Proto, params.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	switch params.Command {
	case "enable":
		_, err = r.ts.runEnable(ctx, &stdoutMsg, &stderrMsg, transportProto)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	case "update":
		_, err = r.ts.runUpdate(ctx, &stdoutMsg, &stderrMsg, transportProto)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	case "disable":
		_, err = r.ts.runDisable(ctx, &stdoutMsg, &stderrMsg, transportProto)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	default:
		err := fmt.Errorf("command not supported")
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return err
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return nil
}

func (ts *TestStep) runEnable(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport,
) (outcome, error) {
	if ts.Parameter.Password == "" || ts.Parameter.CertPath == "" {
		return nil, fmt.Errorf("password and certificate file must be set")
	}

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		"enable",
		fmt.Sprintf("--password=%s", ts.Parameter.Password),
		fmt.Sprintf("--cert=%s", ts.Parameter.CertPath),
		jsonFlag,
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))

	err = parseOutput(stderrMsg, stderr)
	if err != nil {
		return nil, err
	}

	return outcome, err
}

func (ts *TestStep) runUpdate(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport,
) (outcome, error) {
	if ts.Parameter.CertPath == "" || ts.Parameter.KeyPath == "" {
		return nil, fmt.Errorf("new certificate and old private key file must be set")
	}

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		"update",
		fmt.Sprintf("--private-key=%s", ts.Parameter.KeyPath),
		fmt.Sprintf("--cert=%s", ts.Parameter.CertPath),
		jsonFlag,
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))

	err = parseOutput(stderrMsg, stderr)
	if err != nil {
		return nil, err
	}

	return outcome, err
}

func (ts *TestStep) runDisable(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport,
) (outcome, error) {
	if ts.Parameter.Password == "" || ts.Parameter.KeyPath == "" {
		return nil, fmt.Errorf("password and private key file must be set")
	}

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		"disable",
		fmt.Sprintf("--password=%s", ts.Parameter.Password),
		fmt.Sprintf("--private-key=%s", ts.Parameter.KeyPath),
		jsonFlag,
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))

	err = parseOutput(stderrMsg, stderr)
	if err != nil {
		return nil, err
	}

	return outcome, err
}

// getOutputFromReader reads data from the provided io.Reader instances
// representing stdout and stderr, and returns the collected output as byte slices.
func getOutputFromReader(stdout, stderr io.Reader) ([]byte, []byte) {
	// Read from the stdout and stderr pipe readers
	outBuffer, err := readBuffer(stdout)
	if err != nil {
		fmt.Printf("failed to read from Stdout buffer: %v\n", err)
	}

	errBuffer, err := readBuffer(stderr)
	if err != nil {
		fmt.Printf("failed to read from Stderr buffer: %v\n", err)
	}

	return outBuffer, errBuffer
}

// readBuffer reads data from the provided io.Reader and returns it as a byte slice.
// It dynamically accumulates the data using a bytes.Buffer.
func readBuffer(r io.Reader) ([]byte, error) {
	buf := &bytes.Buffer{}
	_, err := io.Copy(buf, r)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseOutput(stderrMsg *strings.Builder, stderr []byte) error {
	err := Error{}
	if len(stderr) != 0 {
		if err := json.Unmarshal(stderr, &err); err != nil {
			err := fmt.Errorf("failed to unmarshal stderr '%s': %v", string(stderr), err)
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return err
		}
	}

	if err.Msg != "" {
		stderrMsg.WriteString(fmt.Sprintf("Command Stderr:\n%s\n", string(stderr)))

		return errors.New(err.Msg)
	}

	return nil
}
