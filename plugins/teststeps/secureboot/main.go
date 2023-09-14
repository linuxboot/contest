package secureboot

import (
	"encoding/json"
	"fmt"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

const (
	in  = "input"
	out = "expect"
)

type inputStepParams struct {
	Transport struct {
		Proto   string          `json:"proto"`
		Options json.RawMessage `json:"options,omitempty"`
	} `json:"transport"`

	Options struct {
		Timeout xjson.Duration `json:"timeout,omitempty"`
	} `json:"options,omitempty"`

	Parameter struct {
		Command         string `json:"command"`
		ToolPath        string `json:"tool_path"`
		Hierarchy       string `json:"hierarchy,omitempty"`
		Append          bool   `json:"append,omitempty"`
		KeyFile         string `json:"key_file,omitempty"`
		CertFile        string `json:"cert_file,omitempty"`
		SigningKeyFile  string `json:"signing_key_file,omitempty"`
		SigningCertFile string `json:"signing_cert_file,omitempty"`
		CustomKeyFile   string `json:"custom_key_file,omitempty"`
	} `json:"parameter"`
}

type expect struct {
	SecureBoot bool `json:"secure_boot,omitempty"`
	SetupMode  bool `json:"setup_mode,omitempty"`
	ShouldFail bool `json:"should_fail,omitempty"`
}

// Name is the name used to look this plugin up.
var Name = "Secure Boot Management"

// TestStep implementation for the exec plugin
type TestStep struct {
	inputStepParams
	expect
}

// Run executes the step.
func (ts *TestStep) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	tr := NewTargetRunner(ts, ev)
	return teststeps.ForEachTarget(Name, ctx, ch, tr.Run)
}

func (ts *TestStep) validateAndPopulateParams(stepParams test.TestStepParameters) error {
	var input *test.Param

	if input = stepParams.GetOne(in); input.IsEmpty() {
		return fmt.Errorf("input parameter cannot be empty")
	}

	if err := json.Unmarshal(input.JSON(), &ts.inputStepParams); err != nil {
		return fmt.Errorf("failed to deserialize %q parameters: %v", in, err)
	}

	if ts.Transport.Proto == "" {
		return fmt.Errorf("transport protocol must not be empty")
	}

	if ts.Parameter.Command == "" {
		return fmt.Errorf("command must not be empty")
	}

	if ts.Parameter.ToolPath == "" {
		return fmt.Errorf("ToolPath must not be empty")
	}

	expect := stepParams.GetOne(out)

	if !expect.IsEmpty() {
		if err := json.Unmarshal(expect.JSON(), &ts.expect); err != nil {
			return fmt.Errorf("failed to deserialize %q parameters: %v", in, err)
		}
	}

	return nil
}

// ValidateParameters validates the parameters associated to the step
func (ts *TestStep) ValidateParameters(_ xcontext.Context, stepParams test.TestStepParameters) error {
	return ts.validateAndPopulateParams(stepParams)
}

// New initializes and returns a new exec step.
func New() test.TestStep {
	return &TestStep{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}

// Name returns the name of the Step
func (ts TestStep) Name() string {
	return Name
}
