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
	var outputBuf strings.Builder

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
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	writeTestStep(r.ts, &outputBuf, &outputBuf)

	if params.Transport.Proto != supportedProto {
		err := fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	transportProto, err := transport.NewTransport(params.Transport.Proto, params.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	switch params.Command {
	case "enable":
		if err := r.ts.runEnable(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	case "update":
		if err := r.ts.runUpdate(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	case "disable":
		if err := r.ts.runDisable(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	default:
		err := fmt.Errorf("command not supported")
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return err
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

func (ts *TestStep) runEnable(
	ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	if ts.Parameter.Password == "" || ts.Parameter.CertPath == "" {
		return fmt.Errorf("password and certificate file must be set")
	}

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		"enable",
		fmt.Sprintf("--password=%s", ts.Parameter.Password),
		fmt.Sprintf("--cert=%s", ts.Parameter.CertPath),
		jsonFlag,
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return fmt.Errorf("failed to create process: %v", err)
	}

	writeCommand(proc.String(), outputBuf)

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stdout: %v", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if len(string(stdout)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
	} else if len(string(stderr)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
	}

	if outcome != nil {
		return fmt.Errorf("failed to run bios certificate cmd: %v", outcome)
	}

	if err := parseOutput(outputBuf, stderr); err != nil {
		return err
	}

	return err
}

func (ts *TestStep) runUpdate(
	ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	if ts.Parameter.CertPath == "" || ts.Parameter.KeyPath == "" {
		return fmt.Errorf("new certificate and old private key file must be set")
	}

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		"update",
		fmt.Sprintf("--private-key=%s", ts.Parameter.KeyPath),
		fmt.Sprintf("--cert=%s", ts.Parameter.CertPath),
		jsonFlag,
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return fmt.Errorf("failed to create process: %v", err)
	}

	writeCommand(proc.String(), outputBuf)

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stdout: %v", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if len(string(stdout)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
	} else if len(string(stderr)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
	}

	if outcome != nil {
		return fmt.Errorf("failed to run bios certificate cmd: %v", outcome)
	}

	err = parseOutput(outputBuf, stderr)
	if ts.ShouldFail && err != nil {
		return nil
	}

	return err
}

func (ts *TestStep) runDisable(
	ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	if ts.Parameter.Password == "" || ts.Parameter.KeyPath == "" {
		return fmt.Errorf("password and private key file must be set")
	}

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		"disable",
		fmt.Sprintf("--password=%s", ts.Parameter.Password),
		fmt.Sprintf("--private-key=%s", ts.Parameter.KeyPath),
		jsonFlag,
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return fmt.Errorf("failed to create process: %v", err)
	}

	writeCommand(proc.String(), outputBuf)

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stdout: %v", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if len(string(stdout)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
	} else if len(string(stderr)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
	}

	if outcome != nil {
		return fmt.Errorf("failed to run bios certificate cmd: %v", outcome)
	}

	if err := parseOutput(outputBuf, stderr); err != nil {
		return err
	}

	return err
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

func parseOutput(outputBuf *strings.Builder, stderr []byte) error {
	err := Error{}
	if len(stderr) != 0 {
		if err := json.Unmarshal(stderr, &err); err != nil {
			return fmt.Errorf("failed to unmarshal stderr '%s': %v", string(stderr), err)
		}
	}

	if err.Msg != "" {
		return errors.New(err.Msg)
	}

	return nil
}
