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
	sudo           = "sudo"
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

	switch r.ts.inputStepParams.Parameter.Command {
	case "enroll-key":
		if _, err = r.ts.enrollKeys(ctx, &stdoutMsg, &stderrMsg, transportProto); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	case "sign":
		if _, err := r.ts.signFile(ctx, &stdoutMsg, &stderrMsg, transportProto); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	case "reset":

		if _, err = r.ts.reset(ctx, &stdoutMsg, &stderrMsg, transportProto); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	case "status":
		writeStatusTestStep(r.ts, &stdoutMsg, &stderrMsg)

		if _, err = r.ts.getStatus(ctx, &stdoutMsg, &stderrMsg, transportProto, r.ts.Parameter.SetupMode, r.ts.Parameter.SecureBoot); err != nil {
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
		return nil, nil
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

func (ts *TestStep) setEfivarsMutable(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) (outcome, error) {
	_, _, outcome, err := execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/PK-*"}, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	_, _, outcome, err = execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/KEK-*"}, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	_, _, outcome, err = execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/db-*"}, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}
	return outcome, nil
}

func (ts *TestStep) getStatus(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport, expectSetupMode, expectSecureBoot bool,
) (outcome, error) {
	writeStatusTestStep(ts, stdoutMsg, stderrMsg)

	outcome, status, err := ts.status(ctx, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	err = checkStatus(stdoutMsg, stderrMsg, status, expectSetupMode, expectSecureBoot)

	return outcome, err
}

func (ts *TestStep) createKeys(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) (outcome, error) {
	_, _, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, []string{"create-keys"}, stdoutMsg, stderrMsg, transport)

	return outcome, err
}

func (ts *TestStep) status(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) (outcome, Status, error) {
	stdout, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, []string{"status", jsonFlag}, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return nil, Status{}, err
	}

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

func parseStatus(stdoutMsg, stderrMsg *strings.Builder, stdout, stderr string) (Status, error) {
	status := Status{}
	if len(stdout) != 0 {
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
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
	writeResetTestStep(ts, stdoutMsg, stderrMsg)

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, []string{"reset"}, stdoutMsg, stderrMsg, transport)
	if err != nil {
		return outcome, err
	}

	if len(stderr) != 0 {
		return outcome, fmt.Errorf("failed to reset secure boot state %v", string(stderr))
	}

	return ts.getStatus(ctx, stdoutMsg, stderrMsg, transport, true, false)
}

func (ts *TestStep) importKeys(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	args := []string{
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

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.Parameter.ToolPath, args, stdoutMsg, stderrMsg, transport)

	if len(stderr) != 0 {
		return outcome, fmt.Errorf("failed to import secure boot keys %v", string(stderr))
	}

	return outcome, err
}

func (ts *TestStep) enrollKeys(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	if err := supportedHierarchy(ts.inputStepParams.Parameter.Hierarchy); err != nil {
		return nil, err
	}

	if ts.Parameter.KeyFile == "" {
		return nil, fmt.Errorf("path to keyfile cannot be empty")
	}

	if ts.Parameter.CertFile == "" {
		return nil, fmt.Errorf("path to certificate file cannot be empty")
	}

	writeEnrollKeysTestStep(ts, stdoutMsg, stderrMsg)

	if outcome, err := ts.checkInstalled(ctx, stdoutMsg, stderrMsg, transport); err != nil {
		return outcome, err
	}

	if _, err := ts.getStatus(ctx, stdoutMsg, stderrMsg, transport, true, false); err != nil {

		if outcome, err := ts.reset(ctx, stdoutMsg, stderrMsg, transport); err != nil {
			return outcome, err
		}

		if outcome, err := ts.getStatus(ctx, stdoutMsg, stderrMsg, transport, true, false); err != nil {
			return outcome, err
		}
	}

	if outcome, err := ts.setEfivarsMutable(ctx, stdoutMsg, stderrMsg, transport); err != nil {
		return outcome, err
	}

	if outcome, err := ts.importKeys(ctx, stdoutMsg, stderrMsg, transport); err != nil {
		return outcome, err
	}

	args := []string{
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

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, args, stdoutMsg, stderrMsg, transport)

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

	// enrolled keys needs some time to be persistent
	time.Sleep(1500 * time.Millisecond)

	return outcome, err
}

func (ts *TestStep) signFile(
	ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	if len(ts.Parameter.Files) == 0 {
		return nil, fmt.Errorf("no  file(s) provided to be signed")
	}

	writeSignTestStep(ts, stdoutMsg, stderrMsg)

	if outcome, err := ts.checkInstalled(ctx, stdoutMsg, stderrMsg, transport); err != nil {
		return outcome, err
	}

	for _, filePath := range ts.Parameter.Files {
		if _, _, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.Command, []string{"sign", filePath}, stdoutMsg, stderrMsg, transport); err != nil {
			return outcome, err
		}
	}

	return nil, nil
}

func execCmdWithArgs(ctx xcontext.Context, privileged bool, cmd string, args []string, stdoutMsg, stderrMsg *strings.Builder, transp transport.Transport) (string, string, error, error) {
	writeCommand(privileged, cmd, args, stdoutMsg, stderrMsg)

	var (
		err  error
		proc transport.Process
	)

	switch privileged {
	case false:
		proc, err = transp.NewProcess(ctx, cmd, args, "")
	case true:
		newArgs := []string{cmd}
		newArgs = append(newArgs, args...)
		proc, err = transp.NewProcess(ctx, sudo, newArgs, "")
	}

	if err != nil {
		err := fmt.Errorf("failed to create process: %v", err)

		return "", "", nil, err
	}

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stdout: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return "", "", nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return "", "", nil, err
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

	return string(stdout), string(stderr), outcome, nil
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
