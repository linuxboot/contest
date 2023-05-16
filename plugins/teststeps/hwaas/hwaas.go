package hwaas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

// http response structs
// this struct is the response for GET /flash
type getFlash struct {
	State string `json:"state"` // possible values: "ready", "busy" or "error"
	Error string `json:"error"`
}

type postFlash struct {
	Action string `json:"action"` // possible values: "read" or "write"
}

type postReset struct {
	State string `json:"state"` // possible values: "on" or "off"
}

type postPower struct {
	Duration string `json:"duration"` // possible values: 0s-20s
}

// this struct can be used for GET /vcc /led /reset
type getState struct {
	State string `json:"state"` // possible values: "on" or "off"
}

// Name is the name used to look this plugin up.
var Name = "HWaaS"

const (
	EventStdout = event.Name("Stdout")
)

var Events = []event.Name{
	EventStdout,
}

type eventStdoutPayload struct {
	Msg string
}

// We need a default timeout to avoid endless running tests.
const (
	defaultTimeoutParameter time.Duration = 15 * time.Minute
	defaultContextID        string        = "0fb4acd8-e429-11ed-b5ea-0242ac120002"
	defaultMachineID        string        = "machine"
	defaultDeviceID         string        = "device"
	defaultHost             string        = "http://9e-hwaas-aux1.lab.9e.network"
	defaultPort             string        = "8080"
)

// HWaaS is used to run arbitrary commands as test steps.
type HWaaS struct {
	host                   *test.Param
	port                   *test.Param
	contextID              *test.Param
	machineID              *test.Param
	deviceID               *test.Param
	command                *test.Param  // Command that shall be run on the dut.
	args                   []test.Param // Arguments that the command need.
	emitStdout, emitStderr bool
}

type Parameter struct {
	host                   string
	port                   string
	contextID              string
	machineID              string
	deviceID               string
	command                string
	args                   []string
	emitStdout, emitStderr bool
}

// Name returns the plugin name.
func (hws HWaaS) Name() string {
	return Name
}

func emitEvent(ctx xcontext.Context, name event.Name, payload interface{}, tgt *target.Target, ev testevent.Emitter) error {
	payloadStr, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot encode payload for event '%s': %v", name, err)
	}
	rm := json.RawMessage(payloadStr)
	evData := testevent.Data{
		EventName: name,
		Target:    tgt,
		Payload:   &rm,
	}
	if err := ev.Emit(ctx, evData); err != nil {
		return fmt.Errorf("cannot emit event EventCmdStart: %v", err)
	}
	return nil
}

// Run executes the cmd step.
func (hws *HWaaS) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	log := ctx.Logger()

	returnFunc := func(err error) {
		if ctx.Writer() != nil {
			writer := ctx.Writer()
			_, err := writer.Write([]byte(err.Error()))
			if err != nil {
				log.Warnf("writing to ctx.Writer failed: %w", err)
			}
		}
	}

	// Validate the parameter
	if err := hws.validateAndPopulate(params); err != nil {
		returnFunc(fmt.Errorf("failed to validate parameter: %v", err))

		return nil, err
	}

	f := func(ctx xcontext.Context, target *target.Target) error {
		var (
			parameter Parameter
			err       error
		)

		// expand all variables
		parameter.host, err = hws.host.Expand(target)
		if err != nil {
			returnFunc(fmt.Errorf("failed to expand variable 'hostname': %v", err))

			return err
		}
		if parameter.host == "" {
			returnFunc(fmt.Errorf("variable 'hostname' must not be empty: %v", err))

			return err
		}

		parameter.port, err = hws.port.Expand(target)
		if err != nil {
			returnFunc(fmt.Errorf("failed to expand variable 'port': %v", err))

			return err
		}
		if parameter.port == "" {
			returnFunc(fmt.Errorf("variable 'port' must not be empty: %v", err))

			return err
		}

		parameter.contextID, err = hws.contextID.Expand(target)
		if err != nil {
			returnFunc(fmt.Errorf("failed to expand variable 'contextID': %v", err))

			return err
		}
		if parameter.contextID == "" {
			returnFunc(fmt.Errorf("variable 'contextID' must not be empty"))

			return fmt.Errorf("variable 'contextID' must not be empty")
		}
		if _, err = uuid.Parse(parameter.contextID); err != nil {
			returnFunc(fmt.Errorf("variable 'contextID' must be an uuid"))

			return fmt.Errorf("variable 'contextID' must be an uuid")
		}

		parameter.machineID, err = hws.machineID.Expand(target)
		if err != nil {
			returnFunc(fmt.Errorf("failed to expand variable 'machineID': %v", err))

			return err
		}
		if parameter.machineID == "" {
			returnFunc(fmt.Errorf("variable 'machineID' must not be empty: %v", err))

			return err
		}

		parameter.deviceID, err = hws.deviceID.Expand(target)
		if err != nil {
			returnFunc(fmt.Errorf("failed to expand variable 'deviceID': %v", err))

			return err
		}
		if parameter.deviceID == "" {
			returnFunc(fmt.Errorf("variable 'deviceID' must not be empty: %v", err))

			return err
		}

		parameter.command, err = hws.command.Expand(target)
		if err != nil {
			returnFunc(fmt.Errorf("failed to expand variable 'command': %v", err))

			return err
		}
		if parameter.command == "" {
			returnFunc(fmt.Errorf("variable 'command' must not be empty: %v", err))

			return err
		}

		var args []string
		for _, arg := range hws.args {
			expArg, err := arg.Expand(target)
			if err != nil {
				returnFunc(fmt.Errorf("failed to expand argument '%s': %v", arg, err))

				return err
			}
			args = append(args, expArg)
		}

		parameter.args = args

		parameter.emitStdout = hws.emitStdout
		parameter.emitStderr = hws.emitStderr

		switch parameter.command {
		case "power":
			if len(args) >= 1 {
				switch args[0] {
				case "on":
					if err := parameter.powerOn(ctx, target, ev); err != nil {
						returnFunc(err)

						return err
					}

					return nil

				case "off":
					if err := parameter.powerOffSoft(ctx, target, ev); err != nil {
						returnFunc(err)

						return err
					}

					if len(args) >= 2 {
						if args[1] == "hard" {
							if err := parameter.powerOffHard(ctx, target, ev); err != nil {
								returnFunc(err)

								return err
							}
						} else {
							returnFunc(fmt.Errorf("failed to execute the power off command. The last argument is not valid. The only possible value is 'hard'."))

							return fmt.Errorf("failed to execute the power off command. The last argument is not valid. The only possible value is 'hard'.")
						}
					}

					return nil

				default:
					returnFunc(fmt.Errorf("failed to execute the power command. The argument %q is not valid. Possible values are 'on' and 'off'.", args))

					return err
				}
			} else {
				returnFunc(fmt.Errorf("failed to execute the power command. Args is empty. Possible values are 'on' and 'off'."))

				return err
			}
		case "flash":
			if len(args) >= 2 {
				switch args[0] {
				case "write":
					if err := parameter.flashWrite(ctx, args[1], target, ev); err != nil {
						returnFunc(err)

						return err
					}

					return nil
				case "read":
					if err := parameter.flashRead(ctx, args[1], target, ev); err != nil {
						returnFunc(err)

						return err
					}

					return nil
				default:
					returnFunc(fmt.Errorf("Failed to execute the flash command. The argument %q is not valid. Possible values are 'read /path/to/binary' and 'write /path/to/binary'.", args))

					return err
				}
			} else {
				returnFunc(fmt.Errorf("Failed to execute the power command. Args is not valid. Possible values are 'read /path/to/binary' and 'write /path/to/binary'."))

				return err
			}
		default:
			returnFunc(fmt.Errorf("Command %q is not valid. Possible values are 'power' and 'flash'.", args))

			return err
		}
	}

	return teststeps.ForEachTarget(Name, ctx, ch, f)
}

func (hws *HWaaS) validateAndPopulate(params test.TestStepParameters) error {
	// validate the hwaas hostname
	hws.host = params.GetOne("host")
	if hws.host.IsEmpty() {
		hws.host = test.NewParam(defaultHost)
	}

	// validate the hwaas port
	hws.port = params.GetOne("port")
	if hws.port.IsEmpty() {
		hws.port = test.NewParam(defaultPort)
	}

	// validate the hwaas context ID
	hws.contextID = params.GetOne("contextID")
	if hws.contextID.IsEmpty() {
		hws.contextID = test.NewParam(defaultContextID)
	}

	// validate the hwaas machine ID
	hws.machineID = params.GetOne("machineID")
	if hws.machineID.IsEmpty() {
		hws.machineID = test.NewParam(defaultMachineID)
	}

	// validate the hwaas device ID
	hws.deviceID = params.GetOne("deviceID")
	if hws.deviceID.IsEmpty() {
		hws.deviceID = test.NewParam(defaultDeviceID)
	}

	// validate the hwaas command
	hws.command = params.GetOne("command")
	if hws.command.IsEmpty() {
		return fmt.Errorf("missing or empty 'command' parameter")
	}

	// validate the hwaas command args
	hws.args = params.Get("args")

	// validate emit_stdout
	emitStdoutParam := params.GetOne("emit_stdout")
	if !emitStdoutParam.IsEmpty() {
		v, err := strconv.ParseBool(emitStdoutParam.String())
		if err != nil {
			return fmt.Errorf("invalid non-boolean `emit_stdout` parameter: %v", err)
		}
		hws.emitStdout = v
	}

	// validate emit_stderr
	emitStderrParam := params.GetOne("emit_stderr")
	if !emitStderrParam.IsEmpty() {
		v, err := strconv.ParseBool(emitStderrParam.String())
		if err != nil {
			return fmt.Errorf("invalid non-boolean `emit_stderr` parameter: %v", err)
		}
		hws.emitStderr = v
	}

	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *HWaaS) ValidateParameters(_ xcontext.Context, params test.TestStepParameters) error {
	return ts.validateAndPopulate(params)
}

// New initializes and returns a new HWaaS test step.
func New() test.TestStep {
	return &HWaaS{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, nil
}

// HTTPRequest triggerers a http request and returns the response. The parameter that can be set are:
// method: can be every http method
// endpoint: api endpoint that shall be requested
// body: the body of the request
func HTTPRequest(ctx xcontext.Context, method string, endpoint string, body io.Reader) (*http.Response, error) {
	client := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func toStdout(ctx xcontext.Context, stdout *string, output string) {
	log := ctx.Logger()

	*stdout = fmt.Sprintf("%s%s\n", *stdout, output)

	log.Infof(output)
}
