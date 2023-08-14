package fwts

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
)

const (
	supportedProto = "ssh"
	privileged     = "sudo"
	cmd            = "fwts"
	outputFlag     = "--results-output=/tmp/output"
	outputPath     = "/tmp/output.log"
	jsonOutputPath = "/tmp/output.json"
)

type outcome error

type Output struct {
	Result string `json:"result"`
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
	var cancel xcontext.CancelFunc

	if r.ts.Options.Timeout != 0 {
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
		defer cancel()
	} else {
		r.ts.Options.Timeout = xjson.Duration(defaultTimeout)
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
		defer cancel()
	}

	pe := test.NewParamExpander(target)

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)

	if r.ts.Transport.Proto != supportedProto {
		err := fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	transport, err := transport.NewTransport(r.ts.Transport.Proto, r.ts.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	_, err = r.ts.runFWTS(ctx, &stdoutMsg, &stderrMsg, target, transport)
	if err != nil {
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return err
}

func (ts *TestStep) runFWTS(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target,
	transport transport.Transport,
) (outcome, error) {
	args := []string{
		cmd,
		strings.Join(ts.Parameter.Flags, " "),
		outputFlag,
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return nil, fmt.Errorf("Failed to create proc: %w", err)
	}

	writeCommand(proc.String(), stdoutMsg, stderrMsg)

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Failed to pipe stdout: %v", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("Failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))

	err = ts.parseOutput(ctx, stdoutMsg, stderrMsg, transport, outputPath)
	if err != nil {
		return outcome, err
	}

	return outcome, nil
}

// getOutputFromReader reads data from the provided io.Reader instances
// representing stdout and stderr, and returns the collected output as byte slices.
func getOutputFromReader(stdout, stderr io.Reader) ([]byte, []byte) {
	// Read from the stdout and stderr pipe readers
	outBuffer, err := readBuffer(stdout)
	if err != nil {
		fmt.Printf("Failed to read from Stdout buffer: %v\n", err)
	}

	errBuffer, err := readBuffer(stderr)
	if err != nil {
		fmt.Printf("Failed to read from Stderr buffer: %v\n", err)
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

func (ts *TestStep) parseOutput(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport, path string,
) error {
	proc, err := transport.NewProcess(ctx, "cat", []string{path}, "")
	if err != nil {
		return fmt.Errorf("Failed to parse Output: %w", err)
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("Failed to parse Output: %w", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return fmt.Errorf("Failed to parse Output: %w", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	_ = proc.Start(ctx)

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if len(stderr) != 0 {
		return fmt.Errorf("Error retrieving the output. Error: %s", string(stderr))
	}

	stdoutMsg.WriteString(fmt.Sprintf("\n\nTest logs:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("\n\nTest logs:\n%s\n", string(stdout)))

	if len(stdout) != 0 {
		re, err := regexp.Compile(`Total:\s+\|\s+\d+\|\s+\d+\|\s+\d+\|\s+\d+\|\s+\d+\|\s+\d+\|`)
		if err != nil {
			return fmt.Errorf("Failed to create the regex: %v", err)
		}

		match := re.FindString(string(stdout))
		if len(match) == 0 {
			stderrMsg.WriteString("Failed to parse stdout. Could not find result.\n")
		} else {
			data, err := parseLine(string(match))
			if err != nil {
				return err
			}

			if ts.Parameter.ReportOnly {
				stdoutMsg.WriteString(fmt.Sprintf("Test result:\n%s", printData(data)))
				return nil
			}

			if data.Failed > 0 {
				return fmt.Errorf("At least one Test failed. Test result:\n%s", printData(data))
			}

			stdoutMsg.WriteString(fmt.Sprintf("No Test failed. Test result:\n%s", printData(data)))
		}
	}

	return nil
}

type Data struct {
	Passed   int
	Failed   int
	Aborted  int
	Warnings int
	Skipped  int
	Info     int
}

func parseLine(line string) (Data, error) {
	// Remove leading and trailing spaces
	line = strings.TrimSpace(line)
	line = strings.TrimRight(line, "|")

	// Split the line into individual fields
	fields := strings.Split(line, "|")

	// Create a Data struct
	data := Data{}

	// Extract and parse each field
	for i, field := range fields {
		if i == 0 {
			continue
		}

		// Remove leading and trailing spaces
		field = strings.TrimSpace(field)

		// Parse the field value to an integer
		value, err := strconv.Atoi(field)
		if err != nil {
			return data, fmt.Errorf("failed to parse field %d: %w", i, err)
		}

		// Assign the parsed value to the corresponding field in the struct
		switch i {
		case 1:
			data.Passed = value
		case 2:
			data.Failed = value
		case 3:
			data.Aborted = value
		case 4:
			data.Warnings = value
		case 5:
			data.Skipped = value
		case 6:
			data.Info = value
		}
	}

	return data, nil
}

func printData(data Data) string {
	return fmt.Sprintf("Passed: %d\nFailed: %d\nAborted: %d\nWarnings: %d\nSkipped: %d\nInfo: %d",
		data.Passed, data.Failed, data.Aborted, data.Warnings, data.Skipped, data.Info)
}
