// Copyright (c) Facebook, Inc. and id affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package dutctl

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/9elements/fti/pkg/dut"
	"github.com/9elements/fti/pkg/dutctl"
	"github.com/9elements/fti/pkg/remote_lab/client"
	"github.com/9elements/fti/pkg/tools"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/multiwriter"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "dutctl"

// We need a default timeout to avoid endless running tests.
const defaultTimeoutParameter = "10m"

// Dutctl is used to retrieve all the parameter, the plugin needs.
type Dutctl struct {
	serverAddr *test.Param  // Addr to the server where the dut is running.
	command    *test.Param  // Command that shall be run on the dut.
	args       []test.Param // Arguments that the command need.
	in         *test.Param  // In writes something on the serial
	expect     *test.Param  // Expect is the string expected in the serial.
	timeout    *test.Param  // Timeout after that the cmd will terminate.
}

// Name returns the plugin name.
func (d Dutctl) Name() string {
	return Name
}

func emitEvent(ctx xcontext.Context, name event.Name, payload interface{}, tgt *target.Target, ev testevent.Emitter) error {
	payloadStr, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot encode payload for event '%s': %w", name, err)
	}
	rm := json.RawMessage(payloadStr)
	evData := testevent.Data{
		EventName: name,
		Target:    tgt,
		Payload:   &rm,
	}
	if err := ev.Emit(ctx, evData); err != nil {
		return fmt.Errorf("cannot emit event EventURL: %w", err)
	}
	return nil
}

var (
	Timeout     time.Duration
	TimeTimeout time.Time
)

// Run executes the Dutctl action.
func (d *Dutctl) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters,
	ev testevent.Emitter, resumeState json.RawMessage,
) (json.RawMessage, error) {
	log := ctx.Logger()
	// Validate the parameter
	if err := d.validateAndPopulate(params); err != nil {
		return nil, err
	}
	f := func(ctx xcontext.Context, target *target.Target) error {
		var err error
		var dutInterface dutctl.DutCtl
		var stdout tools.LogginFunc
		var o dut.FlashOptions

		// expand all variables
		serverAddr, err := d.serverAddr.Expand(target)
		if err != nil {
			return fmt.Errorf("failed to expand variable 'serverAddr': %v", err)
		}

		command, err := d.command.Expand(target)
		if err != nil {
			return fmt.Errorf("failed to expand variable 'command': %v", err)
		}

		var args []string
		for _, arg := range d.args {
			earg, err := arg.Expand(target)
			if err != nil {
				return fmt.Errorf("failed to expand array 'args': %v", err)
			}
			args = append(args, earg)
		}

		expect, err := d.expect.Expand(target)
		if err != nil {
			return fmt.Errorf("failed to expand args 'expect': %v", err)
		}

		// Input can be *not* set
		input, err := d.expect.Expand(target)
		if err != nil {
			return fmt.Errorf("failed to expand arg 'input': %w", err)
		}

		timeoutStr, err := d.timeout.Expand(target)
		if err != nil {
			return fmt.Errorf("failed to expand args 'timeout' %s: %v", timeoutStr, err)
		}
		Timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("cannot parse timeout parameter: %v", err)
		}
		TimeTimeout = time.Now().Add(Timeout)

		if serverAddr != "" {
			var certFile string

			fp := "/" + filepath.Join("etc", "fti", "keys", "ca-cert.pem")
			if _, err := os.Stat(fp); err == nil {
				certFile = fp
			}

			if !strings.Contains(serverAddr, ":") {
				// Add default port
				if certFile == "" {
					serverAddr += ":10000"
				} else {
					serverAddr += ":10001"
				}
			}

			dutInterface, err = client.NewDutCtl("", false, serverAddr, false, "", 0, 2)
			if err != nil {
				// Try insecure on port 10000
				if strings.Contains(serverAddr, ":10001") {
					serverAddr = strings.Split(serverAddr, ":")[0] + ":10000"
				}

				dutInterface, err = client.NewDutCtl("", false, serverAddr, false, "", 0, 2)
				if err != nil {
					return err
				}
			}

		}

		defer func() {
			if dutInterface != nil {
				dutInterface.Close()
			}
		}()

		switch command {
		case "power":
			err = dutInterface.InitPowerPlugins(stdout)
			if err != nil {
				return fmt.Errorf("Failed to init power plugins: %v\n", err)
			}
			if len(args) >= 1 {
				switch args[0] {
				case "on":
					err = dutInterface.PowerOn()
					if err != nil {
						return fmt.Errorf("Failed to power on: %v\n", err)
					}
					log.Infof("dut powered on.")
					if expect != "" {
						if err := serial(ctx, dutInterface, expect, input); err != nil {
							return fmt.Errorf("the expect %q was not found in the logs", expect)
						}
					}
				case "off":
					err = dutInterface.PowerOff()
					if err != nil {
						return fmt.Errorf("Failed to power off: %v\n", err)
					}
					log.Infof("dut powered off.")
				case "powercycle":
					if len(args) == 2 {
						reboots, err := strconv.Atoi(args[1])
						if err != nil {
							return fmt.Errorf("powercycle amount could not be parsed: %v\n", err)
						}
						for i := 1; i < reboots; i++ {
							err = dutInterface.PowerOn()
							if err != nil {
								return fmt.Errorf("Failed to power on: %v\n", err)
							}
							log.Infof("dut powered on.")
							if expect != "" {
								if err := serial(ctx, dutInterface, expect, input); err != nil {
									return fmt.Errorf("the expect %q was not found in the logs", expect)
								}
							}
							err = dutInterface.PowerOff()
							if err != nil {
								return fmt.Errorf("Failed to power off: %v\n", err)
							}
							log.Infof("dut powered off.")
						}
					} else {
						return fmt.Errorf("You have to add only a second argument to specify how often you want to powercycle.")
					}
				default:
					return fmt.Errorf("Failed to execute the power command. The argument %q is not valid. Possible values are 'on', 'off' and 'powercycle'.", args)
				}
			} else {
				return fmt.Errorf("Failed to execute the power command. Args is empty. Possible values are 'on', 'off' and 'powercycle'.")
			}
		case "flash":
			err = dutInterface.InitPowerPlugins(stdout)
			if err != nil {
				return fmt.Errorf("Failed to init power plugins: %v\n", err)
			}
			err = dutInterface.InitFlashPlugins(stdout)
			if err != nil {
				return fmt.Errorf("Failed to init programmer plugins: %v\n", err)
			}

			if len(args) >= 2 {
				switch args[0] {
				case "read", "write", "verify":
					if args[1] == "" {
						return fmt.Errorf("No file was set to read, write or verify: %v\n", err)
					}
				default:
					return fmt.Errorf("Failed to execute the flash command. The argument %q is not valid. Possible values are 'read /path/to/binary', 'write /path/to/binary' and 'verify /path/to/binary'.", args)
				}

				switch args[0] {
				case "read":
					s, err := dutInterface.FlashSupportsRead()
					if err != nil {
						return fmt.Errorf("Error calling FlashSupportsRead\n")
					}
					if !s {
						return fmt.Errorf("Programmer doesn't support read op\n")
					}

					rom, err := dutInterface.FlashRead()
					if err != nil {
						return fmt.Errorf("Fail to read: %v\n", err)
					}

					err = ioutil.WriteFile(args[1], rom, 0o660)
					if err != nil {
						return fmt.Errorf("Failed to write file: %v\n", err)
					}

					log.Infof("dut flash was read.")
				case "write":
					rom, err := ioutil.ReadFile(args[1])
					if err != nil {
						return fmt.Errorf("File '%s' could not be read successfully: %v\n", args[1], err)
					}

					err = dutInterface.FlashWrite(rom, &o)
					if err != nil {
						return fmt.Errorf("Failed to write rom: %v\n", err)
					}

					log.Infof("dut flash was written.")

				case "verify":
					s, err := dutInterface.FlashSupportsVerify()
					if err != nil {
						return fmt.Errorf("FlashSupportsVerify returned error: %v\n", err)
					}

					if !s {
						return fmt.Errorf("Programmer doesn't support verify op\n")
					}

					rom, err := ioutil.ReadFile(args[1])
					if err != nil {
						return fmt.Errorf("File could not be read successfully: %v", err)
					}

					err = dutInterface.FlashVerify(rom)
					if err != nil {
						return fmt.Errorf("Failed to verify: %v\n", err)
					}

					log.Infof("dut flash was verified.")

				default:
					return fmt.Errorf("Failed to execute the flash command. The argument %q is not valid. Possible values are 'read /path/to/binary', 'write /path/to/binary' and 'verify /path/to/binary'.", args)
				}
			} else {
				return fmt.Errorf("Failed to execute the power command. Args is not valid. Possible values are 'read /path/to/binary', 'write /path/to/binary' and 'verify /path/to/binary'.")
			}

		case "serial":
			if err := serial(ctx, dutInterface, expect, input); err != nil {
				return fmt.Errorf("the expected string %q was not found in the logs", expect)
			}
		default:
			return fmt.Errorf("Command %q is not valid. Possible values are 'power', 'flash' and 'serial'.", args)
		}

		return nil
	}

	return teststeps.ForEachTarget(Name, ctx, ch, f)
}

// Retrieve all the parameters defines through the jobDesc
func (d *Dutctl) validateAndPopulate(params test.TestStepParameters) error {
	// Retrieving parameter as json Raw.Message
	// validate the dut server addr
	d.serverAddr = params.GetOne("serverAddr")
	if d.serverAddr.IsEmpty() {
		return fmt.Errorf("missing or empty 'serverAddr' parameter")
	}
	// validate the dutctl cmd
	d.command = params.GetOne("command")
	if d.command.IsEmpty() {
		return fmt.Errorf("missing or empty 'command' parameter")
	}
	// validate the dutctl cmd args
	d.args = params.Get("args")

	// validate the dutctl cmd expect
	d.expect = params.GetOne("expect")

	// validate teh dutctl cmd input
	d.in = params.GetOne("in")

	// validate the dutctl cmd timeout
	if params.GetOne("timeout").IsEmpty() {
		d.timeout = test.NewParam(defaultTimeoutParameter)
	} else {
		d.timeout = params.GetOne("timeout")
	}

	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (d *Dutctl) ValidateParameters(_ xcontext.Context, params test.TestStepParameters) error {
	return d.validateAndPopulate(params)
}

// New initializes and returns a new awsDutctl test step.
func New() test.TestStep {
	return &Dutctl{}
}

// Load returns the name, factory and evend which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, nil
}

func serial(ctx xcontext.Context, dutInterface dutctl.DutCtl, expect string, input string) error {
	log := ctx.Logger()

	err := dutInterface.InitSerialPlugins()
	if err != nil {
		return fmt.Errorf("Failed to init serial plugins: %v\n", err)
	}
	iface, err := dutInterface.GetSerial(0)
	if err != nil {
		return fmt.Errorf("Failed to get serial: %v\n", err)
	}

	// Write in into serial
	if input != "" {
		num, err := iface.Write([]byte(input))
		if err != nil {
			return fmt.Errorf("Error writing '%s' to dutctl: %w", input, err)
		}
		log.Infof("Wrote %d to 'dutctl'", num)
	}

	dst, err := os.Create("/tmp/dutctlserial")
	if err != nil {
		return fmt.Errorf("Creating serial dst file failed: %v", err)
	}
	defer dst.Close()

	// Set up multiwriter
	mw := multiwriter.New()
	if ctx.Writer() != nil {
		err := mw.AddWriter(ctx.Writer())
		if err != nil {
			ctx.Errorf("MultiWriter.AddWriter() = '%w'", err)
		}
	}

	// Add stdout buffer to the Multiwriter
	err = mw.AddWriter(dst)
	if err != nil {
		return fmt.Errorf("MultiWriter.AddWriter() = '%w'", err)
	}

	go func(ctx xcontext.Context) error {
		defer func() {
			iface.Close()
		}()
		for {
			select {
			case <-ctx.Done():
				return nil
			default:
				_, err = io.Copy(mw, iface)
				if err != nil {
					return fmt.Errorf("Failed to copy serial to stdout: %v", err)
				}
			}
		}
	}(ctx)

	log.Infof("Greping serial from dut.")

	for {
		if time.Now().After(TimeTimeout) {
			return fmt.Errorf("timed out after %s", Timeout)
		}
		serial, err := ioutil.ReadFile("/tmp/dutctlserial")
		if err != nil {
			return fmt.Errorf("Failed to read serial file: %v", err)
		}
		if strings.Contains(string(serial), expect) {
			log.Infof("%s", string(serial))
			ctx.Done()
			return nil
		}
		time.Sleep(time.Second)
	}
}
