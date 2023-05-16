// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// The CPUCmd plugin implements an CPU command executor step. Only PublicKey
// authentication is supported.
//
// The cpu command uses ssh config files, e.g. ~/.ssh/config.
// The big difference is that cpu will default to port 23, not port 22,
// if no entries exist for a host.
//
// Warning: this plugin does not lock keys in memory, and does no
// safe erase in memory to avoid forensic attacks. If you need that, please
// submit a PR.
//
// Warning: commands are interpreted, so be careful with external input in the
// test step arguments.
package cpucmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/u-root/cpu/cpu"
	"golang.org/x/crypto/ssh"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "CPUCmd"

// Events is used by the framework to determine which events this plugin will
// emit. Any emitted event that is not registered here will cause the plugin to
// fail.
var Events = []event.Name{}

const defaultTimeoutParameter = "10m"

// CPUCmd is used to run arbitrary commands as test steps.
type CPUCmd struct {
	Host            *test.Param
	Port            *test.Param
	User            *test.Param
	PrivateKeyFile  *test.Param
	Executable      *test.Param
	Args            []test.Param
	Expect          *test.Param
	Timeout         *test.Param
	SkipIfEmptyHost *test.Param
	*cpu.Cmd
}

// Name returns the plugin name.
func (ts CPUCmd) Name() string {
	return Name
}

// Run executes the cmd step.
func (ts *CPUCmd) Run(
	ctx xcontext.Context,
	stepIO test.TestStepInputOutput,
	ev testevent.Emitter,
	stepsVars test.StepsVariables,
	params test.TestStepParameters,
	resumeState json.RawMessage,
) (json.RawMessage, error) {
	log := ctx.Logger()
	// XXX: Dragons ahead! The target (%t) substitution, and function
	// expression evaluations are done at run-time, so they may still fail
	// despite passing at early validation time.
	// If the function evaluations called in validateAndPopulate are not idempotent,
	// the output of the function expressions may be different (e.g. with a call to a
	// backend or a random pool of results)
	// Function evaluation could be done at validation time, but target
	// substitution cannot, because the targets are not known at that time.
	if err := ts.validateAndPopulate(params); err != nil {
		return nil, err
	}

	f := func(ctx xcontext.Context, target *target.Target) error {
		// apply filters and substitutions to user, host, private key, and command args
		// cpu does not do user setting yet.
		// user, err := ts.User.Expand(target)
		// if err != nil {
		// 	return fmt.Errorf("cannot expand user parameter: %v", err)
		// }

		host, err := ts.Host.Expand(target, stepsVars)
		if err != nil {
			return fmt.Errorf("cannot expand host parameter: %v", err)
		}

		if len(host) == 0 {
			shouldSkip := false
			if !ts.SkipIfEmptyHost.IsEmpty() {
				var err error
				shouldSkip, err = strconv.ParseBool(ts.SkipIfEmptyHost.String())
				if err != nil {
					return fmt.Errorf("cannot expand 'skip_if_empty_host' parameter value '%s': %w", ts.SkipIfEmptyHost, err)
				}
			}

			if shouldSkip {
				return nil
			}
			return fmt.Errorf("host value is empty")
		}

		portStr, err := ts.Port.Expand(target, stepsVars)
		if err != nil {
			return fmt.Errorf("cannot expand port parameter: %v", err)
		}
		// The cpu package can use .ssh/config to provide reasonable values.
		// if portStr is empty, then cpu will return the default port.
		port, err := cpu.GetPort(host, portStr)
		if err != nil {
			return fmt.Errorf("Can not expand %q:%q to a cpu port", host, portStr)
		}
		timeoutStr, err := ts.Timeout.Expand(target, stepsVars)
		if err != nil {
			return fmt.Errorf("cannot expand timeout parameter %s: %v", timeoutStr, err)
		}

		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("cannot parse timeout paramter: %v", err)
		}

		timeTimeout := time.Now().Add(timeout)

		privKeyFile, err := ts.PrivateKeyFile.Expand(target, stepsVars)
		if err != nil {
			return fmt.Errorf("cannot expand private key file parameter: %v", err)
		}

		executable, err := ts.Executable.Expand(target, stepsVars)
		if err != nil {
			return fmt.Errorf("cannot expand executable parameter: %v", err)
		}

		// apply functions to the command args, if any
		args := []string{executable}
		for _, arg := range ts.Args {
			earg, err := arg.Expand(target, stepsVars)
			if err != nil {
				return fmt.Errorf("cannot expand command argument '%s': %v", arg, err)
			}
			args = append(args, earg)
		}

		// Nothing to do yet for HostKeyConfig
		c := cpu.Command(host, args...).
			WithRoot("/").
			WithPrivateKeyFile(privKeyFile).
			WithPort(port)

		if err := c.Dial(); err != nil {
			return fmt.Errorf("cannot connect to CPU server %s: %v", host, err)
		}

		if err := c.Start(); err != nil {
			return fmt.Errorf("cannot create CPU session to server %s: %v", host, err)
		}

		defer func() {
			if err := c.Close(); err != nil {
				for _, e := range err {
					if e != io.EOF {
						ctx.Warnf("Failed to close CPU session to %s: %v", host, err)
					}
				}
			}
		}()

		log.Debugf("Running remote CPU command on %s: '%v'", host, c)
		errCh := make(chan error, 1)
		var stdout, stderr bytes.Buffer
		go func() {
			// Have to ignore errors on close; this design only allows
			// one error, and the Wait is pretty important.

			// For now, we have no stdin.
			c.Stdin.Close()

			err := c.Wait()

			if r, err := c.Outputs(); err == nil {
				stdout, stderr = r[0], r[1]
			} else {
				log.Infof("----------> Could not get stdout/stderr: %v", err)
			}

			errCh <- err
		}()

		expect := ts.Expect.String()
		re, err := regexp.Compile(expect)
		keepAliveCnt := 0

		if err != nil {
			return fmt.Errorf("malformed expect parameter: Can not compile %s with %v", expect, err)
		}

		// The flaw in this design is the assumption that stdout/stderr fit in memory :-)
		for {
			select {
			case err := <-errCh:
				log.Infof("Stdout of command '%v' is '%s'", c, stdout.String())
				if err == nil {
					// Execute expectations
					if expect == "" {
						ctx.Warnf("no expectations specified")
					} else {
						matches := re.FindAll(stdout.Bytes(), -1)
						if len(matches) > 0 {
							log.Infof("match for regex '%s' found", expect)
						} else {
							return fmt.Errorf("match for %s not found for target %v", expect, target)
						}
					}
				} else {
					ctx.Warnf("Stderr of command '%v' is '%s'", c, stderr.Bytes())
				}
				return err
			case <-ctx.Done():
				return c.Signal(ssh.SIGKILL)
			case <-time.After(250 * time.Millisecond):
				keepAliveCnt++
				if expect != "" {
					matches := re.FindAll(stdout.Bytes(), -1)
					if len(matches) > 0 {
						log.Infof("match for regex '%s' found", expect)
						return nil
					}
				}
				if time.Now().After(timeTimeout) {
					return fmt.Errorf("timed out after %s", timeout)
				}
				// This is needed to keep the connection to the server alive
				if keepAliveCnt%20 == 0 {
					err = c.Signal(ssh.Signal("CONT"))
					if err != nil {
						log.Warnf("Unable to send CONT to cpu server: %v", err)
					}
				}
			}
		}
	}
	return teststeps.ForEachTarget(Name, ctx, stepIO, f)
}

func (ts *CPUCmd) validateAndPopulate(params test.TestStepParameters) error {
	ts.Host = params.GetOne("host")
	if ts.Host.IsEmpty() {
		return errors.New("invalid or missing 'host' parameter, must be exactly one string")
	}

	// validate and populate does not totally populate; work is repeated
	// elsewhere.
	pp, err := cpu.GetPort(ts.Host.String(), params.GetOne("port").String())
	if err != nil {
		return fmt.Errorf("invalid 'port' parameter: %v", err)
	}
	// Just write it back, always; covers the case that it was not there, or was not set.
	ts.Port = test.NewParam(strconv.Itoa(int(pp)))

	// CPU does not support this yet; we're still working out whether we want it.
	if false {
		ts.User = params.GetOne("user")
		if ts.User.IsEmpty() {
			return errors.New("invalid or missing 'user' parameter, must be exactly one string")
		}
	}

	// do not fail if key file is empty, in such case it won't be used
	ts.PrivateKeyFile = params.GetOne("private_key_file")

	ts.Executable = params.GetOne("executable")
	if ts.Executable.IsEmpty() {
		return errors.New("invalid or missing 'executable' parameter, must be exactly one string")
	}
	ts.Args = params.Get("args")
	ts.Expect = params.GetOne("expect")

	if params.GetOne("timeout").IsEmpty() {
		ts.Timeout = test.NewParam(defaultTimeoutParameter)
	} else {
		ts.Timeout = params.GetOne("timeout")
	}

	ts.SkipIfEmptyHost = params.GetOne("skip_if_empty_host")
	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *CPUCmd) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	ctx.Debugf("Params %+v", params)
	return ts.validateAndPopulate(params)
}

// New initializes and returns a new CPUCmd test step.
func New() test.TestStep {
	return &CPUCmd{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}
