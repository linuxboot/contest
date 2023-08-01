package secureboot

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
	jsonFlag       = "--json"
)

type outcome error

type TargetRunner struct {
	ts *TestStep
	ev testevent.Emitter
}

// Status is the json data structure returned by the status command
type Status struct {
	Installed  bool `json:"installed"`
	SetupMode  bool `json:"setup_mode"`
	SecureBoot bool `json:"secure_boot"`
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
	timeout := r.ts.Options.Timeout
	if timeout != 0 {
		var cancel xcontext.CancelFunc
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(timeout))
		defer cancel()
	}

	pe := test.NewParamExpander(target)

	if r.ts.inputStepParams.Transport.Proto != supportedProto {
		err := fmt.Errorf("only %q is supported as protocol in this teststep", supportedProto)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	transportProto, err := transport.NewTransport(r.ts.inputStepParams.Transport.Proto, r.ts.inputStepParams.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	_, err = r.ts.checkInstalled(ctx, &stdoutMsg, &stderrMsg, transportProto)
	if err != nil {
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	switch r.ts.inputStepParams.Parameter.Command {
	case "enroll-key":

		if err := supportedHierarchy(r.ts.inputStepParams.Parameter.Hierarchy); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

		if r.ts.Parameter.KeyFile == "" {
			err := fmt.Errorf("path to keyfile cannot be empty")
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)

		}

		if r.ts.Parameter.CertFile == "" {
			err := fmt.Errorf("path to certificate file cannot be empty")
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)

		}

		writeEnrollKeysTestStep(r.ts, &stdoutMsg, &stderrMsg)

		// for any ambiguity, outcome is an error interface, but it encodes whether the process
		// was launched sucessfully and it resulted in a failure; err means the launch failed
		_, err = r.ts.getStatus(ctx, &stdoutMsg, &stderrMsg, transportProto, true, false)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			_, err = r.ts.reset(ctx, &stdoutMsg, &stderrMsg, transportProto)
			if err != nil {
				stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

				return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
			}

			_, err = r.ts.getStatus(ctx, &stdoutMsg, &stderrMsg, transportProto, true, false)
			if err != nil {
				stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

				return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
			}
		}

		_, err = r.ts.importKeys(ctx, &stdoutMsg, &stderrMsg, transportProto)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

		_, err = r.ts.enrollKeys(ctx, &stdoutMsg, &stderrMsg, transportProto)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	case "sign":

		if len(r.ts.Parameter.Files) == 0 {
			err := fmt.Errorf("no file(s) provided to be signed")
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

		writeSignTestStep(r.ts, &stdoutMsg, &stderrMsg)

		for _, filePath := range r.ts.Parameter.Files {
			_, err := r.ts.signFile(ctx, &stdoutMsg, &stderrMsg, transportProto, filePath)
			if err != nil {
				stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

				return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
			}
		}
	case "reset":
		writeResetTestStep(r.ts, &stdoutMsg, &stderrMsg)

		_, err := r.ts.reset(ctx, &stdoutMsg, &stderrMsg, transportProto)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

		_, err = r.ts.getStatus(ctx, &stdoutMsg, &stderrMsg, transportProto, true, false)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	case "status":
		writeStatusTestStep(r.ts, &stdoutMsg, &stderrMsg)

		_, err = r.ts.getStatus(ctx, &stdoutMsg, &stderrMsg, transportProto, r.ts.Parameter.SetupMode, r.ts.Parameter.SecureBoot)
		if err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	default:
		return fmt.Errorf("Command '%s' is not valid. Possible values are 'enroll-key', 'reset' and 'sign'.", r.ts.Parameter.Command)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return nil
}

func (ts *TestStep) checkInstalled(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	outcome, status, err := ts.status(ctx, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	if status.Installed {
		return outcome, nil
	}

	outcome, err = ts.createKeys(ctx, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	outcome, status, err = ts.status(ctx, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	if !status.Installed {
		return outcome, fmt.Errorf("failed to setup sbctl on the device")
	}

	return outcome, nil
}

func (ts *TestStep) getStatus(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport, expectSetupMode, expectSecureBoot bool,
) (outcome, error) {
	outcome, status, err := ts.status(ctx, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	err = checkStatus(stdoutMsg, stderrMsg, status, expectSetupMode, expectSecureBoot)

	return outcome, err
}

func (ts *TestStep) createKeys(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) (outcome, error) {
	args := []string{
		ts.inputStepParams.Parameter.ToolPath,
		"create-keys",
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
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
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr: \n%s\n", string(stderr)))

	stdoutMsg.WriteString("\n")
	stderrMsg.WriteString("\n")

	return outcome, nil
}

func (ts *TestStep) status(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) (outcome, Status, error) {
	args := []string{
		ts.inputStepParams.Parameter.ToolPath,
		"status",
		jsonFlag,
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)

		return nil, Status{}, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, Status{}, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, Status{}, err
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
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr: \n%s\n", string(stderr)))

	stdoutMsg.WriteString("\n")
	stderrMsg.WriteString("\n")
	status, err := parseStatus(stdoutMsg, stderrMsg, stdout, stderr)
	if err != nil {
		return nil, Status{}, err
	}

	return outcome, status, nil
}

func checkStatus(stdoutMsg, stderrMsg *strings.Builder, status Status, expectSetupMode, expectSecureBoot bool) error {
	if expectSetupMode != status.SetupMode {
		return fmt.Errorf("SetupMode is expected to be %v, but is %v instead.\n", expectSetupMode, status.SetupMode)
	}

	if expectSecureBoot != status.SecureBoot {
		return fmt.Errorf("SecureBoot is expected to be %v, but is %v instead.\n", expectSecureBoot, status.SecureBoot)
	}

	stdoutMsg.WriteString("Secure Boot is in expected state\n")

	return nil
}

func parseStatus(stdoutMsg, stderrMsg *strings.Builder, stdout, stderr []byte) (Status, error) {
	status := Status{}
	if len(stdout) != 0 {
		if err := json.Unmarshal(stdout, &status); err != nil {
			return Status{}, fmt.Errorf("failed to unmarshal stdout: %v", err)
		}
	}

	if len(stderr) != 0 {
		return Status{}, fmt.Errorf("failed to fetch secureboot status: %v", string(stderr))
	}

	return status, nil
}

func (ts *TestStep) reset(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	var outcome outcome

	args := []string{
		ts.inputStepParams.Parameter.ToolPath,
		"reset",
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome = proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr: \n%s\n", string(stderr)))

	if len(stderr) != 0 {
		return outcome, fmt.Errorf("failed to reset secure boot state %v", string(stderr))
	}

	stdoutMsg.WriteString("\n")
	stderrMsg.WriteString("\n")

	return outcome, err
}

func (ts *TestStep) importKeys(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	var outcome outcome

	args := []string{
		ts.Parameter.ToolPath,
		"import-keys",
		"--force",
	}

	switch ts.Parameter.Hierarchy {
	case "db":
		args = append(args, fmt.Sprintf("--kek-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--kek-cert=%v", ts.inputStepParams.Parameter.CertFile))
	case "KEK":
		args = append(args, fmt.Sprintf("--pk-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--pk-cert=%v", ts.inputStepParams.Parameter.CertFile))
	case "PK":
		args = append(args, fmt.Sprintf("--pk-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--pk-cert=%v", ts.inputStepParams.Parameter.CertFile))
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome = proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr: \n%s\n", string(stderr)))

	if len(stderr) != 0 {
		return outcome, fmt.Errorf("failed to import secure boot keys %v", string(stderr))
	}

	stdoutMsg.WriteString("\n")
	stderrMsg.WriteString("\n")

	return outcome, err
}

func (ts *TestStep) enrollKeys(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	var outcome outcome

	args := []string{
		ts.inputStepParams.Parameter.ToolPath,
		"enroll-keys",
		"--microsoft",
	}

	switch ts.inputStepParams.Parameter.Hierarchy {
	case "db":
		args = append(args, fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy))
	case "KEK":
		args = append(args, fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy))
	case "PK":
		args = append(args, fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy))
	}
	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome = proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr: \n%s\n", string(stderr)))

	switch ts.Parameter.ShouldFail {
	case false:
		if len(stderr) != 0 {
			return outcome, fmt.Errorf("failed unexpectedly to enroll secure boot keys for hierarchy %s: %v", ts.inputStepParams.Parameter.Hierarchy, string(stderr))
		}
	case true:
		if len(stderr) == 0 {
			return outcome, fmt.Errorf("enrolled secure boot keys for hierarchy %s, but expected to fail", ts.inputStepParams.Parameter.Hierarchy)
		}
	}

	stdoutMsg.WriteString("\n")
	stderrMsg.WriteString("\n")

	return outcome, err
}

func (ts *TestStep) signFile(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport, filePath string,
) (outcome, error) {
	var outcome outcome

	args := []string{
		ts.inputStepParams.Parameter.ToolPath,
		"sign",
		filePath,
	}

	writeCommand(privileged, args, stdoutMsg, stderrMsg)

	proc, err := transport.NewProcess(ctx, privileged, args, "")
	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return nil, err
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome = proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

	stdoutMsg.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))
	stderrMsg.WriteString(fmt.Sprintf("Command Stderr: \n%s\n", string(stderr)))

	if len(stderr) != 0 {
		return outcome, fmt.Errorf("failed to sign file %s: %v", filePath, string(stderr))
	}

	stdoutMsg.WriteString("\n")
	stderrMsg.WriteString("\n")

	return outcome, err
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

func supportedHierarchy(hierarchy string) error {
	switch hierarchy {
	case "db":
		return nil
	case "KEK":
		return nil
	case "PK":
		return nil
	default:
		return fmt.Errorf("unknown hierarchy %s, only [db,KEK,PK] are supported!", hierarchy)
	}
}
