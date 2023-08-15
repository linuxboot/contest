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
	var outputBuf strings.Builder

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
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	transportProto, err := transport.NewTransport(r.ts.inputStepParams.Transport.Proto, r.ts.inputStepParams.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	switch r.ts.inputStepParams.Parameter.Command {
	case "enroll-key":
		if _, err = r.ts.enrollKeys(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}
	case "rotate-key":
		if _, err = r.ts.rotateKeys(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}
	case "custom":
		if _, err = r.ts.customKey(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}
	case "reset":

		if _, err = r.ts.reset(ctx, &outputBuf, transportProto); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}
	case "status":
		if _, err = r.ts.getStatus(ctx, &outputBuf, transportProto, r.ts.Parameter.SetupMode, r.ts.Parameter.SecureBoot); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}
	default:
		return fmt.Errorf("Command '%s' is not valid. Possible values are 'status', 'enroll-key', 'rotate-key', 'reset' and 'sign'.", r.ts.Parameter.Command)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

func (ts *TestStep) checkInstalled(
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	outcome, status, err := ts.status(ctx, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	if status.Installed {
		return nil, nil
	}

	outcome, err = ts.createKeys(ctx, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	outcome, status, err = ts.status(ctx, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	if !status.Installed {
		return outcome, fmt.Errorf("failed to setup sbctl on the device")
	}

	return outcome, nil
}

func (ts *TestStep) setEfivarsMutable(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport) (error, error) {
	stdout, _, outcome, err := execCmdWithArgs(ctx, true, "find", []string{"/sys/firmware/efi/efivars", "-name", `"PK-*"`}, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	if len(stdout) != 0 {
		if _, _, outcome, err = execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/PK-*"}, outputBuf, transport); err != nil {
			return outcome, err
		}
	}

	stdout, _, outcome, err = execCmdWithArgs(ctx, true, "find", []string{"/sys/firmware/efi/efivars", "-name", `"KEK-*"`}, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	if len(stdout) != 0 {
		if _, _, outcome, err = execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/KEK-*"}, outputBuf, transport); err != nil {
			return outcome, err
		}
	}

	stdout, _, outcome, err = execCmdWithArgs(ctx, true, "find", []string{"/sys/firmware/efi/efivars", "-name", `"db-*"`}, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	if len(stdout) != 0 {
		if _, _, outcome, err = execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/db-*"}, outputBuf, transport); err != nil {
			return outcome, err
		}
	}

	stdout, _, outcome, err = execCmdWithArgs(ctx, true, "find", []string{"/sys/firmware/efi/efivars", "-name", `"dbx-*"`}, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	if len(stdout) != 0 {
		if _, _, outcome, err = execCmdWithArgs(ctx, true, "chattr", []string{"-i", "/sys/firmware/efi/efivars/dbx-*"}, outputBuf, transport); err != nil {
			return outcome, err
		}
	}

	return nil, nil
}

func (ts *TestStep) getStatus(
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport, expectSetupMode, expectSecureBoot bool,
) (outcome, error) {
	writeStatusTestStep(ts, outputBuf)

	outcome, status, err := ts.status(ctx, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	err = checkStatus(outputBuf, status, expectSetupMode, expectSecureBoot)

	return outcome, err
}

func (ts *TestStep) createKeys(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport) (outcome, error) {
	_, _, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, []string{"create-keys"}, outputBuf, transport)

	return outcome, err
}

func (ts *TestStep) status(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport) (outcome, Status, error) {
	stdout, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, []string{"status", jsonFlag}, outputBuf, transport)
	if err != nil {
		return nil, Status{}, err
	}

	status, err := parseStatus(outputBuf, stdout, stderr)
	if err != nil {
		return nil, Status{}, err
	}

	return outcome, status, nil
}

func checkStatus(outputBuf *strings.Builder, status Status, expectSetupMode, expectSecureBoot bool) error {
	if expectSetupMode != status.SetupMode {
		return fmt.Errorf("SetupMode is expected to be %v, but is %v instead.\n", expectSetupMode, status.SetupMode)
	}

	if expectSecureBoot != status.SecureBoot {
		return fmt.Errorf("SecureBoot is expected to be %v, but is %v instead.\n", expectSecureBoot, status.SecureBoot)
	}

	outputBuf.WriteString("Secure Boot is in expected state\n")

	return nil
}

func parseStatus(outputBuf *strings.Builder, stdout, stderr string) (Status, error) {
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
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	writeResetTestStep(ts, outputBuf)

	if err := supportedHierarchy(ts.inputStepParams.Parameter.Hierarchy); err != nil {
		return nil, err
	}

	if outcome, err := ts.setEfivarsMutable(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}

	if outcome, err := ts.importKeys(ctx, outputBuf, transport, false, true); err != nil {
		return outcome, err
	}

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, []string{"reset", fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy)}, outputBuf, transport)
	if err != nil {
		return outcome, err
	}

	switch ts.Parameter.ShouldFail {
	case false:
		if len(stderr) != 0 {
			return outcome, fmt.Errorf("failed unexpectedly to enroll secure boot keys for hierarchy %s: %v", ts.inputStepParams.Parameter.Hierarchy, string(stderr))
		}
	case true:
		if len(stderr) == 0 {
			return outcome, fmt.Errorf("reset secure boot keys for hierarchy %s, but expected to fail", ts.inputStepParams.Parameter.Hierarchy)
		}
	}

	return outcome, nil
}

var importArgs = []string{
	"import-keys",
	"--force",
}

func (ts *TestStep) importKeys(
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport, pair, signingPair bool,
) (outcome, error) {
	var (
		outcome error
		err     error
		args    = importArgs
		stderr  string
	)

	if pair {
		if ts.Parameter.CertFile == "" || ts.Parameter.KeyFile == "" {
			return outcome, fmt.Errorf("no files provided")
		}

		switch ts.Parameter.Hierarchy {
		case "dbx":
			args = append(args, fmt.Sprintf("--dbx-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--dbx-cert=%v", ts.inputStepParams.Parameter.CertFile))
		case "db":
			args = append(args, fmt.Sprintf("--db-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--db-cert=%v", ts.inputStepParams.Parameter.CertFile))
		case "KEK":
			args = append(args, fmt.Sprintf("--kek-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--kek-cert=%v", ts.inputStepParams.Parameter.CertFile))
		case "PK":
			args = append(args, fmt.Sprintf("--pk-key=%v", ts.inputStepParams.Parameter.KeyFile), fmt.Sprintf("--pk-cert=%v", ts.inputStepParams.Parameter.CertFile))
		}

		_, stderr, outcome, err = execCmdWithArgs(ctx, true, ts.Parameter.ToolPath, args, outputBuf, transport)
		if err != nil {
			return outcome, err
		}

		if len(stderr) != 0 {
			return outcome, fmt.Errorf("failed to import secure boot keys: %v", string(stderr))
		}

	}

	if signingPair {
		if ts.Parameter.SigningCertFile == "" || ts.Parameter.SigningKeyFile == "" {
			return outcome, fmt.Errorf("no signing files provided")
		}

		args := importArgs

		switch ts.Parameter.Hierarchy {
		case "dbx":
			args = append(args, fmt.Sprintf("--kek-key=%v", ts.inputStepParams.Parameter.SigningKeyFile), fmt.Sprintf("--kek-cert=%v", ts.inputStepParams.Parameter.SigningCertFile))
		case "db":
			args = append(args, fmt.Sprintf("--kek-key=%v", ts.inputStepParams.Parameter.SigningKeyFile), fmt.Sprintf("--kek-cert=%v", ts.inputStepParams.Parameter.SigningCertFile))
		case "KEK":
			args = append(args, fmt.Sprintf("--pk-key=%v", ts.inputStepParams.Parameter.SigningKeyFile), fmt.Sprintf("--pk-cert=%v", ts.inputStepParams.Parameter.SigningCertFile))
		case "PK":
			args = append(args, fmt.Sprintf("--pk-key=%v", ts.inputStepParams.Parameter.SigningKeyFile), fmt.Sprintf("--pk-cert=%v", ts.inputStepParams.Parameter.SigningCertFile))
		}

		_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.Parameter.ToolPath, args, outputBuf, transport)
		if err != nil {
			return outcome, err
		}

		if len(stderr) != 0 {
			return outcome, fmt.Errorf("failed to import secure boot keys for signing: %v", string(stderr))
		}
	}

	return outcome, nil
}

func (ts *TestStep) enrollKeys(
	ctx xcontext.Context, outputBuf *strings.Builder,
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

	if ts.Parameter.Hierarchy != "PK" {
		if ts.Parameter.SigningKeyFile == "" {
			return nil, fmt.Errorf("path to signing keyfile cannot be empty")
		}

		if ts.Parameter.SigningCertFile == "" {
			return nil, fmt.Errorf("path to signing certificate file cannot be empty")
		}
	}

	writeEnrollKeysTestStep(ts, outputBuf)

	if outcome, err := ts.checkInstalled(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}

	if outcome, err := ts.setEfivarsMutable(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}
	if outcome, err := ts.importKeys(ctx, outputBuf, transport, true, true); err != nil {
		return outcome, err
	}

	args := []string{
		"enroll-keys",
		"--microsoft",
		fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy),
	}

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, args, outputBuf, transport)

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

func (ts *TestStep) rotateKeys(
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	writeRotateKeysTestStep(ts, outputBuf)

	if err := supportedHierarchy(ts.inputStepParams.Parameter.Hierarchy); err != nil {
		return nil, err
	}

	if ts.Parameter.KeyFile == "" {
		return nil, fmt.Errorf("path to keyfile cannot be empty")
	}

	if ts.Parameter.CertFile == "" {
		return nil, fmt.Errorf("path to certificate file cannot be empty")
	}

	if ts.Parameter.SigningKeyFile == "" {
		return nil, fmt.Errorf("path to signing keyfile cannot be empty")
	}

	if ts.Parameter.SigningCertFile == "" {
		return nil, fmt.Errorf("path to signing certificate file cannot be empty")
	}

	if outcome, err := ts.importKeys(ctx, outputBuf, transport, false, true); err != nil {
		return outcome, err
	}

	if outcome, err := ts.checkInstalled(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}

	if outcome, err := ts.setEfivarsMutable(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}

	args := []string{
		"rotate-keys",
		fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy),
		fmt.Sprintf("--key-file=%v", ts.inputStepParams.Parameter.KeyFile),
		fmt.Sprintf("--cert-file=%v", ts.inputStepParams.Parameter.CertFile),
	}

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, args, outputBuf, transport)

	switch ts.Parameter.ShouldFail {
	case false:
		if len(stderr) != 0 {
			return outcome, fmt.Errorf("failed unexpectedly to rotate secure boot keys for hierarchy %s: %v", ts.inputStepParams.Parameter.Hierarchy, string(stderr))
		}
	case true:
		if len(stderr) == 0 {
			return outcome, fmt.Errorf("rotated secure boot keys for hierarchy %s, but expected to fail", ts.inputStepParams.Parameter.Hierarchy)
		}
	}

	// rotated keys needs some time to be persistent
	time.Sleep(1500 * time.Millisecond)

	return outcome, err
}

func (ts *TestStep) customKey(
	ctx xcontext.Context, outputBuf *strings.Builder,
	transport transport.Transport,
) (outcome, error) {
	writeCustomKeyTestStep(ts, outputBuf)

	if err := supportedHierarchy(ts.inputStepParams.Parameter.Hierarchy); err != nil {
		return nil, err
	}

	if ts.Parameter.CustomKeyFile == "" {
		return nil, fmt.Errorf("path to custom keyfile cannot be empty")
	}

	if outcome, err := ts.checkInstalled(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}

	if outcome, err := ts.setEfivarsMutable(ctx, outputBuf, transport); err != nil {
		return outcome, err
	}

	args := []string{
		"custom-key",
		fmt.Sprintf("--partial=%v", ts.inputStepParams.Parameter.Hierarchy),
		fmt.Sprintf("--custom-bytes=%v", ts.inputStepParams.Parameter.CustomKeyFile),
	}

	_, stderr, outcome, err := execCmdWithArgs(ctx, true, ts.inputStepParams.Parameter.ToolPath, args, outputBuf, transport)

	switch ts.Parameter.ShouldFail {
	case false:
		if len(stderr) != 0 {
			return outcome, fmt.Errorf("failed unexpectedly to rotate secure boot keys for hierarchy %s: %v", ts.inputStepParams.Parameter.Hierarchy, string(stderr))
		}
	case true:
		if len(stderr) == 0 {
			return outcome, fmt.Errorf("rotated secure boot keys for hierarchy %s, but expected to fail", ts.inputStepParams.Parameter.Hierarchy)
		}
	}

	// rotated keys needs some time to be persistent
	time.Sleep(1500 * time.Millisecond)

	return outcome, err
}

func execCmdWithArgs(ctx xcontext.Context, privileged bool, cmd string, args []string, outputBuf *strings.Builder, transp transport.Transport) (string, string, error, error) {
	writeCommand(privileged, cmd, args, outputBuf)

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
		outputBuf.WriteString(fmt.Sprintf("%v\n", err))

		return "", "", nil, err
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		err := fmt.Errorf("failed to pipe stderr: %v", err)
		outputBuf.WriteString(fmt.Sprintf("%v\n", err))

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

	outputBuf.WriteString(fmt.Sprintf("Command Stdout:\n%s\n\n\n", string(stdout)))
	outputBuf.WriteString(fmt.Sprintf("Command Stderr: \n%s\n\n\n", string(stderr)))

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
	case "dbx":
		return nil
	case "KEK":
		return nil
	case "PK":
		return nil
	default:
		return fmt.Errorf("unknown hierarchy %s, only [db,KEK,PK] are supported!", hierarchy)
	}
}
