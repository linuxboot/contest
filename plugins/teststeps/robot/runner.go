package cpuload

import (
	"bytes"
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
)

const (
	supportedProto = "local"
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
	var outputBuf *strings.Builder

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

		return emitStderr(ctx, EventStderr, outputBuf.String(), target, r.ev, err)
	}

	r.ts.writeTestStep(outputBuf)

	transport, err := transport.NewTransport(r.ts.Transport.Proto, r.ts.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, outputBuf.String(), target, r.ev, err)
	}

	if err := r.ts.runRobot(ctx, outputBuf, transport); err != nil {
		return emitStderr(ctx, EventStderr, outputBuf.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: outputBuf.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return err
}

func (ts *TestStep) runRobot(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	args := []string{"robot"}

	for _, arg := range ts.Parameter.Args {
		args = append(args, "-v", arg)
	}

	args = append(args, ts.Parameter.FilePath)

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
	if outcome != nil {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", outcome.Error()))

		return fmt.Errorf("Failed to run robot test: %v.", outcome)
	}

	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	if outcome != nil {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))

		return fmt.Errorf("Failed to run robot test: %v.", outcome)
	}

	outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))

	if ts.Parameter.ReportOnly {
		return nil
	}

	return parseOutput(ctx, outputBuf, transport)
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

func parseOutput(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport) error {
	return nil
}
