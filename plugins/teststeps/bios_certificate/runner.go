package bios_certificate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	ctx.Infof("Executing on target %s", target)

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
		return err
	}

	if params.Transport.Proto != supportedProto {
		return fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
	}

	transportProto, err := transport.NewTransport(params.Transport.Proto, params.Transport.Options, pe)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// for any ambiguity, outcome is an error interface, but it encodes whether the process
	// was launched sucessfully and it resulted in a failure; err means the launch failed
	var outcome outcome

	switch params.Command {
	case "enable":
		outcome, err = r.runEnable(ctx, target, transportProto, params)
		if err != nil {
			return err
		}
	case "update":
		outcome, err = r.runUpdate(ctx, target, transportProto, params)
		if err != nil {
			return err
		}
	case "disable":
		outcome, err = r.runDisable(ctx, target, transportProto, params)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("command not supported")
	}

	return outcome
}

func (r *TargetRunner) runEnable(
	ctx xcontext.Context, target *target.Target,
	transport transport.Transport, params inputStepParams,
) (outcome, error) {
	if params.Parameter.Password == "" || params.Parameter.CertPath == "" {
		return nil, fmt.Errorf("password and certificate file must be set")
	}

	proc, err := transport.NewProcess(
		ctx,
		privileged,
		[]string{
			params.Parameter.ToolPath,
			cmd,
			"enable",
			fmt.Sprintf("--password=%s", params.Parameter.Password),
			fmt.Sprintf("--cert=%s", params.Parameter.CertPath),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process: %v", err)
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stdout: %v", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if err := parseOutput(stderr); err != nil {
		return nil, err
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: string(stdout)}, target, r.ev); err != nil {
		return nil, fmt.Errorf("cannot emit event: %v", err)
	}
	if err := emitEvent(ctx, EventStderr, eventPayload{Msg: string(stderr)}, target, r.ev); err != nil {
		return nil, fmt.Errorf("cannot emit event: %v", err)
	}

	return outcome, nil
}

func (r *TargetRunner) runUpdate(
	ctx xcontext.Context, target *target.Target,
	transport transport.Transport, params inputStepParams,
) (outcome, error) {
	if params.Parameter.CertPath == "" || params.Parameter.KeyPath == "" {
		return nil, fmt.Errorf("new certificate and old private key file must be set")
	}

	proc, err := transport.NewProcess(
		ctx,
		privileged,
		[]string{
			params.Parameter.ToolPath,
			cmd,
			"update",
			fmt.Sprintf("--private-key=%s", params.Parameter.Password),
			fmt.Sprintf("--cert=%s", params.Parameter.CertPath),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process: %v", err)
	}

	stdOut, err := proc.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stdout: %v", err)
	}

	stdErr, err := proc.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdOut, stdErr)

	if err := parseOutput(stderr); err != nil {
		return nil, err
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: string(stdout)}, target, r.ev); err != nil {
		return nil, fmt.Errorf("cannot emit event: %v", err)
	}
	if err := emitEvent(ctx, EventStderr, eventPayload{Msg: string(stderr)}, target, r.ev); err != nil {
		return nil, fmt.Errorf("cannot emit event: %v", err)
	}

	return outcome, nil
}

func (r *TargetRunner) runDisable(
	ctx xcontext.Context, target *target.Target,
	transport transport.Transport, params inputStepParams,
) (outcome, error) {
	if params.Parameter.Password == "" || params.Parameter.KeyPath == "" {
		return nil, fmt.Errorf("password and private key file must be set")
	}

	proc, err := transport.NewProcess(
		ctx,
		privileged,
		[]string{
			params.Parameter.ToolPath,
			cmd,
			"disable",
			fmt.Sprintf("--password=%s", params.Parameter.Password),
			fmt.Sprintf("--private-key=%s", params.Parameter.KeyPath),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process: %v", err)
	}

	stdOut, err := proc.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stdout: %v", err)
	}

	stdErr, err := proc.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdOut, stdErr)

	if err := parseOutput(stderr); err != nil {
		return nil, err
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: string(stdout)}, target, r.ev); err != nil {
		return nil, fmt.Errorf("cannot emit event: %v", err)
	}
	if err := emitEvent(ctx, EventStderr, eventPayload{Msg: string(stderr)}, target, r.ev); err != nil {
		return nil, fmt.Errorf("cannot emit event: %v", err)
	}

	return outcome, nil
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

func parseOutput(stderr []byte) error {
	err := Error{}

	if len(stderr) != 0 {
		if err := json.Unmarshal(stderr, &err); err != nil {
			return fmt.Errorf("failed to unmarshal stderr: %v", err)
		}
	}

	if err.Msg != "" {
		return fmt.Errorf("%s", err.Msg)
	}

	return nil
}
