package bios_settings_get

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
		ToolPath string `json:"tool_path,omitempty"`
	} `json:"parameter"`
}

type Expect struct {
	Option string `json:"option"`
	Value  string `json:"value"`
}

// Name is the name used to look this plugin up.
var Name = "Get Bios Setting"

// TestStep re
type TestStep struct {
	inputStepParams
	expectStepParams []Expect
}

// Run executes the step.
func (ts *TestStep) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	tr := NewTargetRunner(ts, ev)
	return teststeps.ForEachTarget(Name, ctx, ch, tr.Run)
}

func (ts *TestStep) populateParams(stepParams test.TestStepParameters) error {
	var input *test.Param

	if input = stepParams.GetOne(in); input.IsEmpty() {
		return fmt.Errorf("input parameter cannot be empty")
	}

	if err := json.Unmarshal(input.JSON(), &ts.inputStepParams); err != nil {
		return fmt.Errorf("failed to deserialize %q parameters: %v", in, err)
	}

	expect := stepParams.Get(out)

	var (
		tmp          Expect
		expectParams []Expect
	)

	for _, item := range expect {
		if err := json.Unmarshal(item.JSON(), &tmp); err != nil {
			return fmt.Errorf("failed to deserialize %q parameters: %v", out, err)
		}

		expectParams = append(expectParams, tmp)
	}

	ts.expectStepParams = expectParams

	return nil
}

// ValidateParameters validates the parameters associated to the step
func (ts *TestStep) ValidateParameters(_ xcontext.Context, stepParams test.TestStepParameters) error {
	return ts.populateParams(stepParams)
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
