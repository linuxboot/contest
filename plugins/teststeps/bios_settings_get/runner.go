package bios_settings_get

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/firmwareci/system-suite/pkg/wmi"
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
	argument       = "list"
	jsonFlag       = "--json"
)

type outcome error

type TargetRunner struct {
	ts *TestStep
	ev testevent.Emitter
}

type Data struct {
	WMIOptions wmi.WMIOptions `json:"data"`
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
	ctx.Infof("Executing on target %s", target)

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
		return err
	}

	if err := pe.ExpandObject(r.ts.expectStepParams, &expectParams); err != nil {
		return err
	}

	if inputParams.Transport.Proto != supportedProto {
		return fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
	}

	transportProto, err := transport.NewTransport(inputParams.Transport.Proto, inputParams.Transport.Options, pe)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// for any ambiguity, outcome is an error interface, but it encodes whether the process
	// was launched sucessfully and it resulted in a failure; err means the launch failed
	outcome, err := r.runGet(ctx, target, transportProto, inputParams, expectParams)
	if err != nil {
		return err
	}

	return outcome
}

func (r *TargetRunner) runGet(
	ctx xcontext.Context, target *target.Target,
	transport transport.Transport, inputParams inputStepParams, expectParams []Expect,
) (outcome, error) {
	proc, err := transport.NewProcess(
		ctx,
		privileged,
		[]string{
			inputParams.Parameter.ToolPath,
			cmd,
			argument,
			jsonFlag,
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

	if err := parseListOutput(stdout, stderr, expectParams); err != nil {
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

func parseListOutput(stdout, stderr []byte, expectOptions []Expect) error {
	data := Data{}
	err := Error{}

	if len(stdout) != 0 {
		if err := json.Unmarshal(stdout, &data); err != nil {
			return fmt.Errorf("failed to unmarshal stdout: %v", err)
		}
	}

	if len(stderr) != 0 {
		if err := json.Unmarshal(stderr, &err); err != nil {
			return fmt.Errorf("failed to unmarshal stderr: %v", err)
		}
	}

	errorMsg := fmt.Errorf("")

	if err.Msg == "" {
		for _, item := range expectOptions {
			found := false
			for _, attribute := range data.WMIOptions.Attributes {
				if attribute.Name == item.Option {
					found = true
					if attribute.Value != item.Value {
						errorMsg = fmt.Errorf("%vexpected setting %s value is not as expected, have %q want %q\n",
							errorMsg, attribute.Name, attribute.Value, item.Value)
					}
				}
			}

			if item.Option == "IsEnabled" {
				if item.Value == "true" && data.WMIOptions.Authentication["Admin"].IsEnabled {
					return nil
				} else if item.Value == "false" && !data.WMIOptions.Authentication["Admin"].IsEnabled {
					return nil
				} else {
					errorMsg = fmt.Errorf("%vexpected setting 'Admin - IsEnabled' value is not as expected, have \"%t\" want %q\n",
						errorMsg, data.WMIOptions.Authentication["Admin"].IsEnabled, item.Value)
				}
			}

			if !found {
				errorMsg = fmt.Errorf("%vexpected setting %s is not found in the attribute list\n", errorMsg, item.Option)
			}
		}
	} else {
		errorMsg = fmt.Errorf("%s", err.Msg)
	}

	return errorMsg
}
