package sshcmd

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
)

type outcome error

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

	var params inputStepParams

	if err := pe.ExpandObject(r.ts.inputStepParams, &params); err != nil {
		return err
	}

	transport, err := transport.NewTransport(params.Transport.Proto, params.Transport.Options, pe)
	if err != nil {
		return fmt.Errorf("fail to create transport: %w", err)
	}

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)

	_, err = r.runCMD(ctx, &stdoutMsg, &stderrMsg, target, transport)
	if err != nil {
		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return err
}

func (r *TargetRunner) runCMD(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target,
	transport transport.Transport,
) (outcome, error) {
	proc, err := transport.NewProcess(ctx, r.ts.Bin.Executable, r.ts.Bin.Args)
	if err != nil {
		err := fmt.Errorf("Failed to create proc: %w", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	writeCommand(proc.String(), stdoutMsg, stderrMsg)

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("Failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("Failed to pipe stderr: %v", err)
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
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr:\n%s\n", string(stderr)))

	err = parseOutput(stdoutMsg, stderrMsg, stdout, r.ts.expectStepParams)
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

func parseOutput(stdoutMsg, stderrMsg *strings.Builder, stdout []byte, expects []Expect) error {
	var err error

	for _, expect := range expects {
		re, err := regexp.Compile(expect.Regex)
		if err != nil {
			err = fmt.Errorf("Failed to parse the regex: %v", err)
			stderrMsg.WriteString(err.Error())
		}

		matches := re.FindAll(stdout, -1)
		if len(matches) > 0 {
			stdoutMsg.WriteString(fmt.Sprintf("Found the expected string in Stdout: '%s'\n", expect))
		} else {
			err = fmt.Errorf("Could not find the expected string '%s' in Stdout.\n", expect)
			stderrMsg.WriteString(err.Error())
		}
	}

	return err
}
