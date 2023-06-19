package sshcmd

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

// Name is the name used to look this plugin up.
var Name = "SSHCmd"

const (
	defaultTimeout = 10 * time.Minute
	in             = "input"
	out            = "expect"
)

type inputStepParams struct {
	Bin struct {
		Executable string   `json:"executable"`
		Args       []string `json:"args"`
		// TODO: add max execution timer
	} `json:"binary"`

	Transport struct {
		Proto   string          `json:"proto"`
		Options json.RawMessage `json:"options,omitempty"`
	} `json:"transport,omitempty"`

	Options struct {
		Timeout xjson.Duration `json:"timeout,omitempty"`
	} `json:"options,omitempty"`
}

type Expect struct {
	Regex string `json:"regex,omitempty"`
}

// TestStep implementation for this teststep plugin
type TestStep struct {
	inputStepParams
	expectStepParams []Expect
}

// Name returns the plugin name.
func (ts TestStep) Name() string {
	return Name
}

// Run executes the step.
func (ts *TestStep) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	if err := ts.populateParams(params); err != nil {
		return nil, err
	}

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

// ValidateParameters validates the parameters associated to the TestStep
func (ts *TestStep) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	return ts.populateParams(params)
}

// New initializes and returns a new TestStep.
func New() test.TestStep {
	return &TestStep{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}
