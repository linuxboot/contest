package bios_certificate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// events that we may emit during the plugin's lifecycle
const (
	EventStdout = event.Name("Stdout")
	EventStderr = event.Name("Stderr")
)

// Events defines the events that a TestStep is allow to emit. Emitting an event
// that is not registered here will cause the plugin to terminate with an error.
var Events = []event.Name{
	EventStdout,
	EventStderr,
}

type eventPayload struct {
	Msg string
}

func emitEvent(ctx xcontext.Context, name event.Name, payload interface{}, tgt *target.Target, ev testevent.Emitter) error {
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot marshal payload for event '%s': %w", name, err)
	}

	msg := json.RawMessage(payloadData)
	data := testevent.Data{
		EventName: name,
		Target:    tgt,
		Payload:   &msg,
	}

	if err := ev.Emit(ctx, data); err != nil {
		return fmt.Errorf("cannot emit event EventCmdStart: %w", err)
	}

	return nil
}

// Function to format teststep information and append it to a string builder.
func writeTestStep(step *TestStep, builders ...*strings.Builder) {
	for _, builder := range builders {
		builder.WriteString("Input Parameter:\n")
		builder.WriteString(fmt.Sprintf("  Command: %s\n", step.Command))
		builder.WriteString("  Transport:\n")
		builder.WriteString(fmt.Sprintf("    Protocol: %s\n", step.Transport.Proto))
		builder.WriteString("    Options: \n")
		optionsJSON, err := json.MarshalIndent(step.Transport.Options, "", "    ")
		if err != nil {
			builder.WriteString(fmt.Sprintf("%v", step.Transport.Options))
		} else {
			builder.WriteString(string(optionsJSON))
		}
		builder.WriteString("\n")

		builder.WriteString("  Parameter:\n")
		builder.WriteString(fmt.Sprintf("    ToolPath: %s\n", step.Parameter.ToolPath))
		builder.WriteString(fmt.Sprintf("    Password: %s\n", step.Parameter.Password))
		builder.WriteString(fmt.Sprintf("    CertPath: %s\n", step.Parameter.CertPath))
		builder.WriteString(fmt.Sprintf("    KeyPath: %s\n", step.Parameter.KeyPath))
		builder.WriteString("\n")

		builder.WriteString("  Options:\n")
		builder.WriteString(fmt.Sprintf("    Timeout: %s\n", time.Duration(step.Options.Timeout)))
		builder.WriteString("\n\n")
	}
}

// Function to format command information and append it to a string builder.
func writeCommand(command string, args []string, builders ...*strings.Builder) {
	for _, builder := range builders {
		builder.WriteString("Executing Command:\n")
		builder.WriteString(fmt.Sprintf("%s %s", command, strings.Join(args, " ")))
		builder.WriteString("\n\n")
	}
}

// Function to format command output information and append it to a string builder.
// func writeCommandOutput(builder *strings.Builder, stdout, stderr []byte) {
// 	builder.WriteString(fmt.Sprintf("Command Stdout:\n%s\n", string(stdout)))

// 	errMsg := Error{}
// 	if len(stderr) != 0 {
// 		if err := json.Unmarshal(stderr, &errMsg); err != nil {
// 			builder.WriteString(fmt.Sprintf("%v\n", err))
// 		}
// 	}

// 	builder.WriteString("Command Stderr:\n")
// 	builder.WriteString(errMsg.Msg)
// }

// emitStderr emits the whole error message an returns the error
func emitStderr(ctx xcontext.Context, name event.Name, stderrMsg string, tgt *target.Target, ev testevent.Emitter, err error) error {
	if err := emitEvent(ctx, EventStderr, eventPayload{Msg: stderrMsg}, tgt, ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return err
}
