package hwaas

import (
	"fmt"
	"strings"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type TargetRunner struct {
	ts *TestStep
	ev testevent.Emitter
}

func NewTargetRunner(ts *TestStep, ev testevent.Emitter) *TargetRunner {
	return &TargetRunner{
		ts: ts,
		ev: ev,
	}
}

func (r *TargetRunner) Run(ctx xcontext.Context, target *target.Target) error {
	var stdoutMsg, stderrMsg strings.Builder

	ctx.Infof("Executing on target %s", target)

	// limit the execution time if specified
	var cancel xcontext.CancelFunc

	if r.ts.Options.Timeout != 0 {
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
		defer cancel()
	} else {
		r.ts.Options.Timeout = xjson.Duration(defaultTimeout)
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
		defer cancel()
	}

	pe := test.NewParamExpander(target)

	var params inputStepParams

	if err := pe.ExpandObject(r.ts.inputStepParams, &params); err != nil {
		return err
	}

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)
	writeCommand(params.Parameter.Command, params.Parameter.Args, &stdoutMsg, &stderrMsg)

	stdoutMsg.WriteString("Stdout:\n")
	stderrMsg.WriteString("Stderr:\n")

	switch params.Parameter.Command {
	case "power":
		if err := r.powerCmds(ctx, &stdoutMsg, &stderrMsg, target, params.Parameter.Args); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	case "flash":
		if err := r.flashCmds(ctx, &stdoutMsg, &stderrMsg, target, params.Parameter.Args); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	default:
		err := fmt.Errorf("Command '%s' is not valid. Possible values are 'power' and 'flash'.", params.Parameter.Args)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("Failed to emit event: %v", err)
	}

	return nil
}
