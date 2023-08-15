package cpustats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
	"github.com/linuxboot/contest/plugins/teststeps/cpu"
)

const (
	supportedProto = "ssh"
	privileged     = "sudo"
	cmd            = "cpu"
	argument       = "stats"
	jsonFlag       = "--json"
)

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
	var outputBuf strings.Builder

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

	if r.ts.Transport.Proto != supportedProto {
		err := fmt.Errorf("only '%s' is supported as protocol in this teststep", supportedProto)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if r.ts.Parameter.Interval != "" {
		if _, err := time.ParseDuration(r.ts.Parameter.Interval); err != nil {
			return fmt.Errorf("wrong interval statement, valid units are ns, us, ms, s, m and h")
		}
	}

	r.ts.writeTestStep(&outputBuf)

	transport, err := transport.NewTransport(r.ts.Transport.Proto, r.ts.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if err = r.ts.runStats(ctx, &outputBuf, transport); err != nil {
		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

func (ts *TestStep) runStats(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	var args []string

	if ts.Parameter.Interval != "" {
		args = []string{
			ts.Parameter.ToolPath,
			cmd,
			argument,
			fmt.Sprintf("--interval=%s", ts.Parameter.Interval),
			jsonFlag,
		}
	} else {
		args = []string{
			ts.Parameter.ToolPath,
			cmd,
			argument,
			jsonFlag,
		}
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return fmt.Errorf("Failed to create proc: %w", err)
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
	if outcome != nil {
		return fmt.Errorf("Failed to get CPU stats: %v.", outcome)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if len(string(stdout)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
	} else if len(string(stderr)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
	}

	if err = ts.parseOutput(ctx, outputBuf, stdout); err != nil {
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
	if _, err := io.Copy(buf, r); err != nil && err != io.EOF {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (ts *TestStep) parseOutput(ctx xcontext.Context, outputBuf *strings.Builder, stdout []byte) error {
	var (
		stats       cpu.Stats
		interval    bool
		finalError  bool
		errorString string
	)

	if ts.Parameter.Interval != "" {
		interval = true
	}

	if len(stdout) != 0 {
		if err := json.Unmarshal(stdout, &stats); err != nil {
			return fmt.Errorf("failed to unmarshal stdout: %v", err)
		}
	}

	for _, expect := range ts.expectStepParams.General {
		if err := stats.CheckGeneralOption(expect, outputBuf); err != nil {
			errorString += fmt.Sprintf("failed to check general option '%s': %v\n", expect.Option, err)
			finalError = true
		}
	}

	for _, expect := range ts.expectStepParams.Individual {
		if err := stats.CheckIndividualOption(expect, interval, outputBuf); err != nil {
			errorString += fmt.Sprintf("failed to check individual option '%s': %v\n", expect.Option, err)
			finalError = true
		}
	}

	if finalError {
		return fmt.Errorf("Some expect options are not as expected:\n%s", errorString)
	}

	return nil
}
