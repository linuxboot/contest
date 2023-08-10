package cpuload

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

	if r.ts.Transport.Proto != supportedProto {
		err := fmt.Errorf("only '%s' is supported as protocol in this teststep", supportedProto)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if r.ts.Parameter.Duration != "" {
		if _, err := time.ParseDuration(r.ts.Parameter.Duration); err != nil {
			return fmt.Errorf("wrong duration statement, valid units are ns, us, ms, s, m and h")
		}
	}

	r.ts.writeTestStep(&stdoutMsg, &stderrMsg)

	transport, err := transport.NewTransport(r.ts.Transport.Proto, r.ts.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := r.ts.runLoad(ctx, &stdoutMsg, &stderrMsg, transport); err != nil {
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return err
}

func (ts *TestStep) runLoad(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport,
) error {
	var (
		args []string
	)

	if len(ts.Parameter.Cores) == 0 {
		args = []string{"stress-ng", "--cpu $(nproc)", "--cpu-load 100", fmt.Sprintf("--timeout %s", ts.Parameter.Duration), "&"}
	} else {
		for _, core := range ts.Parameter.Cores {
			args = append(args, []string{fmt.Sprintf("taskset -c %d", core), "stress-ng", "--cpu 1", "--cpu-load 100",
				fmt.Sprintf("--timeout %s", ts.Parameter.Duration), "&"}...)
		}
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return fmt.Errorf("Failed to create proc: %w", err)
	}

	writeCommand(proc.String(), stdoutMsg, stderrMsg)

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
	if outcome != nil {
		stderrMsg.WriteString(fmt.Sprintf("Stderr:\n%s\n", outcome.Error()))

		return fmt.Errorf("Failed to run load test: %v.", outcome)
	}

	if len(ts.Individual) > 0 || len(ts.General) > 0 {
		if err := ts.parseStats(ctx, stdoutMsg, stderrMsg, transport); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("Stderr:\n%s\n", err.Error()))
			return fmt.Errorf("Failed to parse cpu stats: %v.", err)
		}
	}

	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if outcome != nil {
		stderrMsg.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))

		return fmt.Errorf("Failed to run load test: %v.", outcome)
	}

	stdoutMsg.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))

	return err
}

func (ts *TestStep) parseStats(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) error {
	duration, err := time.ParseDuration(ts.Parameter.Duration)
	if err != nil {
		return fmt.Errorf("wrong interval statement, valid units are ns, us, ms, s, m and h")
	}

	// Run cpu stats only for 96% of the load duration.
	interval := duration * 96 / 100
	offset := duration * 2 / 100

	// Start the cpu stats command with a small offset, so it is in the middle of the load duration.
	time.Sleep(offset)

	args := []string{
		ts.Parameter.ToolPath,
		cmd,
		argument,
		fmt.Sprintf("--interval=%s", interval.String()),
		jsonFlag,
	}

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		return fmt.Errorf("Failed to create proc: %w", err)
	}

	writeCommand(proc.String(), stdoutMsg, stderrMsg)

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

	stdoutMsg.WriteString(fmt.Sprintf("Stats Stdout:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("Stats Stderr:\n%s\n", string(stderr)))

	if err = ts.parseOutput(ctx, stdoutMsg, stderrMsg, stdout); err != nil {
		return err
	}

	return nil
}

// getOutputFromReader reads data from the provided io.Reader instances
// representing stdout and stderr, and returns the collected output as byte slices.
func getOutputFromReader(stdout, stderr io.Reader) ([]byte, []byte) {
	// Read from the stdout and stderr pipe readers``
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

func (ts *TestStep) parseOutput(ctx xcontext.Context, stdoutMsg *strings.Builder, stderrMsg *strings.Builder,
	stdout []byte,
) error {
	var (
		stats      cpu.Stats
		finalError bool
	)

	if len(stdout) != 0 {
		if err := json.Unmarshal(stdout, &stats); err != nil {
			return fmt.Errorf("failed to unmarshal stdout: %v", err)
		}
	}

	for _, expect := range ts.expectStepParams.General {
		if err := stats.CheckGeneralOption(expect, stdoutMsg, stderrMsg); err != nil {
			finalError = true
		}
	}

	for _, expect := range ts.expectStepParams.Individual {
		if err := stats.CheckIndividualOption(expect, true, stdoutMsg, stderrMsg); err != nil {
			finalError = true
		}
	}

	if finalError {
		return fmt.Errorf("Some expect options are not as expected.")
	}

	return nil
}
