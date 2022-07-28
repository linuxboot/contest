// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package gathercmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// Name is the name used to look this plugin up.
var Name = "GatherCmd"

// event names for this plugin
const (
	EventCmdStart = event.Name("GatherCmdStart")
	EventCmdEnd   = event.Name("GatherCmdEnd")
)

// Events defines the events that a TestStep is allow to emit
var Events = []event.Name{
	EventCmdStart,
	EventCmdEnd,
}

// eventCmdStartPayload is the payload for EventCmdStart
type eventCmdStartPayload struct {
	Path string
	Args []string
}

// eventCmdEndPayload is the payload for EventCmdEnd
type eventCmdEndPayload struct {
	ExitCode int
	Err      string
}

// GatherCmd is used to run arbitrary commands as test steps but only once
// for all the targets. This can be used as test setup/teardown.
type GatherCmd struct {
	// binary to execute; will consult $PATH
	binary string

	// arguments to pass to the binary on execution
	args []string
}

// Name returns the plugin name.
func (ts GatherCmd) Name() string {
	return Name
}

func emitEvent(
	ctx xcontext.Context,
	emitter testevent.Emitter,
	target *target.Target,
	name event.Name,
	payload interface{},
) error {
	jsonstr, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot encode payload for event '%s': %v", name, err)
	}

	jsonPayload := json.RawMessage(jsonstr)
	data := testevent.Data{
		EventName: name,
		Target:    target,
		Payload:   &jsonPayload,
	}
	if err := emitter.Emit(ctx, data); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return nil
}

func truncate(in string, maxsize uint) string {
	size := uint(len(in))
	if size > maxsize {
		size = maxsize
	}
	return in[:size]
}

func (ts *GatherCmd) acquireTargets(ctx xcontext.Context, ch test.TestStepChannels) ([]*target.Target, error) {
	var targets []*target.Target

	for {
		select {
		case target, ok := <-ch.In:
			if !ok {
				ctx.Debugf("acquired %d targets", len(targets))
				return targets, nil
			}
			targets = append(targets, target)

		case <-ctx.Until(xcontext.ErrPaused):
			ctx.Debugf("paused during target acquisition, acquired %d", len(targets))
			return nil, xcontext.ErrPaused

		case <-ctx.Done():
			ctx.Debugf("canceled during target acquisition, acquired %d", len(targets))
			return nil, ctx.Err()
		}
	}
}

func (ts *GatherCmd) returnTargets(ctx xcontext.Context, ch test.TestStepChannels, targets []*target.Target) {
	for _, target := range targets {
		ch.Out <- test.TestStepResult{Target: target}
	}
}

// Run executes the step
func (ts *GatherCmd) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, emitter testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	log := ctx.Logger()

	if err := ts.setParams(params); err != nil {
		return nil, err
	}

	// acquire all targets and hold them hostage until the cmd is done
	targets, err := ts.acquireTargets(ctx, ch)
	if err != nil {
		return nil, err
	}
	defer ts.returnTargets(ctx, ch, targets)

	if len(targets) == 0 {
		return nil, nil
	}

	// arbitrarily choose first target to associate events with, anyone would work
	// but it is unnecessary to have the same event on all targets since this is a
	// "gather" type plugin
	eventTarget := targets[0]

	// used to manually cancel the exec if step becomes paused
	ctx, cancel := xcontext.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-ctx.Until(xcontext.ErrPaused):
			log.Debugf("step was paused, killing the executing command")
			cancel()

		case <-ctx.Done():
			// does not leak because cancel is deferred
		}
	}()

	cmd := exec.CommandContext(ctx, ts.binary, ts.args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Put the command into a separate session (and group) so signals do not propagate directly to it.
		Setsid: true,
	}

	log.Debugf("Running command: %+v", cmd)

	err = emitEvent(
		ctx,
		emitter, eventTarget,
		EventCmdStart,
		eventCmdStartPayload{Path: cmd.Path, Args: cmd.Args},
	)
	if err != nil {
		log.Warnf("Failed to emit event: %v", err)
	}

	if cmdErr := cmd.Run(); cmdErr != nil {
		// the command may have been canceled as a result of the step pausing
		if ctx.IsSignaledWith(xcontext.ErrPaused) {
			// nothing to save, just continue pausing
			return nil, xcontext.ErrPaused
		}

		// output the failure event
		var payload eventCmdEndPayload

		var exitError *exec.ExitError
		if errors.As(cmdErr, &exitError) {
			// we ran the binary and it had non-zero exit code
			payload = eventCmdEndPayload{
				ExitCode: exitError.ExitCode(),
				Err:      truncate(fmt.Sprintf("stdout: %s\nstderr: %s", stdout.String(), stderr.String()), 10240),
			}
		} else {
			// something failed while trying to spawn the process
			payload = eventCmdEndPayload{
				Err: fmt.Sprintf("unknown error: %v", cmdErr),
			}
		}

		if err := emitEvent(ctx, emitter, eventTarget, EventCmdEnd, payload); err != nil {
			log.Warnf("Failed to emit event: %v", err)
		}

		return nil, cmdErr
	}

	if err := emitEvent(ctx, emitter, eventTarget, EventCmdEnd, nil); err != nil {
		log.Warnf("Failed to emit event: %v", err)
	}

	// TODO: make step output with stdout/err when PR #83 gets merged
	return nil, nil
}

func (ts *GatherCmd) setParams(params test.TestStepParameters) error {
	param := params.GetOne("binary")
	if param.IsEmpty() {
		return errors.New("invalid or missing 'binary' parameter")
	}

	binary := param.String()
	if filepath.IsAbs(binary) {
		ts.binary = binary
	} else {
		fullpath, err := exec.LookPath(binary)
		if err != nil {
			return fmt.Errorf("cannot find '%s' executable in PATH: %v", binary, err)
		}
		ts.binary = fullpath
	}

	ts.args = []string{}
	args := params.Get("args")
	// expand args in case they use functions, but they shouldnt be target aware
	for _, arg := range args {
		expanded, err := arg.Expand(&target.Target{})
		if err != nil {
			return fmt.Errorf("failed to expand argument: %s -> %v", arg, err)
		}
		ts.args = append(ts.args, expanded)
	}

	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *GatherCmd) ValidateParameters(_ xcontext.Context, params test.TestStepParameters) error {
	return ts.setParams(params)
}

// New initializes and returns a new GatherCmd test step.
func New() test.TestStep {
	return &GatherCmd{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}
