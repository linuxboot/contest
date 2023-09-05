package bios_settings_get

import (
	"bytes"
	"encoding/json"
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
	argument       = "get"
	jsonFlag       = "--json"
)

type TargetRunner struct {
	ts *TestStep
	ev testevent.Emitter
}

// Output is the data structure for a bios option that is returned
type Output struct {
	Data Data `json:"data"`
}

type Data struct {
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	PossibleValues []string `json:"possible_values"`
	Value          string   `json:"value"`
}

type Error struct {
	Msg string `json:"error"`
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

	var (
		inputParams  inputStepParams
		expectParams []Expect
	)

	if err := pe.ExpandObject(r.ts.inputStepParams, &inputParams); err != nil {
		err := fmt.Errorf("failed to expand input parameter: %v", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if err := pe.ExpandObject(r.ts.expectStepParams, &expectParams); err != nil {
		err := fmt.Errorf("failed to expand expect parameter: %v", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	writeTestStep(r.ts, &outputBuf)

	if inputParams.Transport.Proto != supportedProto {
		err := fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	transportProto, err := transport.NewTransport(inputParams.Transport.Proto, inputParams.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if err := r.ts.runGet(ctx, &outputBuf, transportProto); err != nil {
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

func (ts *TestStep) runGet(
	ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	var finalErr error

	for _, expect := range ts.expectStepParams {
		args := []string{
			ts.Parameter.ToolPath,
			cmd,
			argument,
			fmt.Sprintf("--option=%s", expect.Option),
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
			err := fmt.Errorf("failed to pipe stdout: %v", err)
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return err
		}

		stderrPipe, err := proc.StderrPipe()
		if err != nil {
			err := fmt.Errorf("failed to pipe stderr: %v", err)
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return err
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
			err := fmt.Errorf("failed to run bios get cmd for option '%s': %v", expect.Option, outcome)
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))
			finalErr = err

			continue
		}

		if err = parseOutput(outputBuf, stdout, stderr, expect); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))
			outputBuf.WriteString("\n\n")

			finalErr = fmt.Errorf("At least one expect parameter is not as expected.")

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

func parseOutput(outputBuf *strings.Builder, stdout, stderr []byte, expectOption Expect) error {
	output := Output{}
	if len(stdout) != 0 {
		if err := json.Unmarshal(stdout, &output); err != nil {
			return fmt.Errorf("failed to unmarshal stdout: %v", err)
		}
	}

	err := Error{}
	if len(stderr) != 0 {
		if err := json.Unmarshal(stderr, &err); err != nil {
			return fmt.Errorf("failed to unmarshal stderr: %v", err)
		}
	}

	if err.Msg == "" {
		if expectOption.Option == output.Data.Name {
			if expectOption.Value != output.Data.Value {
				return fmt.Errorf("Expected setting '%s' value is not as expected, have '%s' want '%s'.\n",
					expectOption.Option, output.Data.Value, expectOption.Value)
			} else {
				outputBuf.WriteString(fmt.Sprintf("BIOS setting '%s' is set as expected: '%s'.\n", expectOption.Option, expectOption.Value))
				return nil
			}
		} else {
			return fmt.Errorf("Expected setting '%s' is not found in the attribute list.\n", expectOption.Option)
		}
	} else {
		err := fmt.Errorf("%s", err.Msg)

		return err
	}
}
