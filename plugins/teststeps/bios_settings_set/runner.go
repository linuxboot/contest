package bios_settings_set

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
	cmd            = "wmi"
	argument       = "set"
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

	writeTestStep(r.ts, &outputBuf)

	if params.Transport.Proto != supportedProto {
		err := fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if params.Parameter.Password == "" && params.Parameter.KeyPath == "" {
		err := fmt.Errorf("password or certificate file must be set")
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if len(params.Parameter.BiosOptions) == 0 {
		err := fmt.Errorf("at least one bios option and value must be set")
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	transportProto, err := transport.NewTransport(params.Transport.Proto, params.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if err := r.ts.runSet(ctx, &outputBuf, transportProto, params); err != nil {
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

func (ts *TestStep) runSet(
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport, params inputStepParams,
) error {
	var (
		authString string
		finalErr   error
	)

	if params.Parameter.Password != "" {
		authString = fmt.Sprintf("--password=%s", params.Parameter.Password)
	} else if params.Parameter.KeyPath != "" {
		authString = fmt.Sprintf("--private-key=%s", params.Parameter.KeyPath)
	}

	for _, option := range ts.Parameter.BiosOptions {
		args := []string{
			params.Parameter.ToolPath,
			cmd,
			argument,
			fmt.Sprintf("--option=%s", option.Option),
			fmt.Sprintf("--value=%s", option.Value),
			authString,
			jsonFlag,
		}

		proc, err := transport.NewProcess(ctx, privileged, args, "")
		if err != nil {
			err := fmt.Errorf("failed to create process: %v", err)
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return err
		}

		writeCommand(proc.String(), outputBuf)

		stdoutPipe, err := proc.StdoutPipe()
		if err != nil {
			return fmt.Errorf("Failed to pipe stdout: %v", err)
		}

		stderrPipe, err := proc.StderrPipe()
		if err != nil {
			return fmt.Errorf("Failed to pipe stderr: %v", err)
		}

		// try to start the process, if that succeeds then the outcome is the result of
		// waiting on the process for its result; this way there's a semantic difference
		// between "an error occured while launching" and "this was the outcome of the execution"
		outcome := proc.Start(ctx)
		if outcome == nil {
			outcome = proc.Wait(ctx)
		}

		stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe, outputBuf)

		if len(string(stdout)) > 0 {
			outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
		} else if len(string(stderr)) > 0 {
			outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
		}

		if outcome != nil {
			err := fmt.Errorf("failed to run bios set cmd for option '%s': %v", option.Option, outcome)
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))
			finalErr = err

			continue
		}

		if err := ts.parseOutput(stderr); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))
			outputBuf.WriteString("\n\n")

			finalErr = fmt.Errorf("At least one bios setting could not be set.")

			continue
		}

		outputBuf.WriteString("\n\n")
	}

	return finalErr
}

// getOutputFromReader reads data from the provided io.Reader instances
// representing stdout and stderr, and returns the collected output as byte slices.
func getOutputFromReader(stdout, stderr io.Reader, outputBuf *strings.Builder) ([]byte, []byte) {
	// Read from the stdout and stderr pipe readers
	outBuffer, err := readBuffer(stdout)
	if err != nil {
		outputBuf.WriteString(fmt.Sprintf("Failed to read from Stdout buffer: %v\n", err))
	}

	errBuffer, err := readBuffer(stderr)
	if err != nil {
		outputBuf.WriteString(fmt.Sprintf("Failed to read from Stderr buffer: %v\n", err))
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

func (ts *TestStep) parseOutput(stderr []byte) error {
	err := Error{}
	if len(stderr) != 0 {
		if err := json.Unmarshal(stderr, &err); err != nil {
			return fmt.Errorf("failed to unmarshal stderr: %v", err)
		}
	}

	if err.Msg != "" {
		if err.Msg == "BIOS options are locked, needs unlocking." && ts.expect.ShouldFail {
			return nil
		} else if err.Msg != "" && ts.expect.ShouldFail {
			return nil
		} else {
			return errors.New(err.Msg)
		}
	} else if ts.expect.ShouldFail {
		return fmt.Errorf("Setting BIOS option should fail, but produced no error.")
	}

	return nil
}
