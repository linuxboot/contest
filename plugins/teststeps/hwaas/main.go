package hwaas

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

// We need a default timeout to avoid endless running tests.
const (
	defaultTimeout   time.Duration = 15 * time.Minute
	defaultContextID string        = "0fb4acd8-e429-11ed-b5ea-0242ac120002"
	defaultMachineID string        = "machine"
	defaultDeviceID  string        = "device"
	defaultHost      string        = "http://9e-hwaas-aux1.lab.9e.network"
	defaultPort      int           = 80
	defaultVersion   string        = ""
	in                             = "input"
)

type inputStepParams struct {
	Parameter struct {
		Host      string   `json:"host,omitempty"`
		Port      int      `json:"port,omitempty"`
		Version   string   `json:"version,omitempty"`
		ContextID string   `json:"context_id,omitempty"`
		MachineID string   `json:"machine_id,omitempty"`
		DeviceID  string   `json:"device_id,omitempty"`
		Command   string   `json:"command,omitempty"`
		Args      []string `json:"args,omitempty"`
	} `json:"parameter"`

	Options struct {
		Timeout xjson.Duration `json:"timeout,omitempty"`
	} `json:"options,omitempty"`
}

// Name is the name used to look this plugin up.
var Name = "HwaaS"

// TestStep implementation for this teststep plugin
type TestStep struct {
	inputStepParams
}

// Run executes the cmd step.
func (ts *TestStep) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	// Validate the parameter
	if err := ts.validateAndPopulate(params); err != nil {
		return nil, err
	}

	tr := NewTargetRunner(ts, ev)
	return teststeps.ForEachTarget(Name, ctx, ch, tr.Run)
}

func (ts *TestStep) validateAndPopulate(stepParams test.TestStepParameters) error {
	var input *test.Param

	if input = stepParams.GetOne(in); input.IsEmpty() {
		return fmt.Errorf("input parameter cannot be empty")
	}

	if err := json.Unmarshal(input.JSON(), &ts.inputStepParams); err != nil {
		return fmt.Errorf("failed to deserialize %q parameters: %v", in, err)
	}

	if ts.Parameter.Host == "" {
		ts.Parameter.Host = defaultHost
	}

	if ts.Parameter.Port == 0 {
		ts.Parameter.Port = defaultPort
	}

	if ts.Parameter.Version == "" {
		ts.Parameter.Version = defaultVersion
	}

	if ts.Parameter.ContextID == "" {
		ts.Parameter.ContextID = defaultContextID
	}

	if ts.Parameter.MachineID == "" {
		ts.Parameter.MachineID = defaultMachineID
	}

	if ts.Parameter.DeviceID == "" {
		ts.Parameter.DeviceID = defaultDeviceID
	}

	if ts.Parameter.Command == "" {
		return fmt.Errorf("missing or empty 'command' parameter")
	}

	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (ts *TestStep) ValidateParameters(_ xcontext.Context, params test.TestStepParameters) error {
	return ts.validateAndPopulate(params)
}

// New initializes and returns a new HWaaS test step.
func New() test.TestStep {
	return &TestStep{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}

// Name returns the plugin name.
func (ts TestStep) Name() string {
	return Name
}
