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
	var outputBuf strings.Builder

	// limit the execution time if specified
	var cancel xcontext.CancelFunc

	if r.ts.Options.Timeout == 0 {
		r.ts.Options.Timeout = xjson.Duration(defaultTimeout)
	}

	ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
	defer cancel()

	pe := test.NewParamExpander(target)

	var params inputStepParams

	if err := pe.ExpandObject(r.ts.inputStepParams, &params); err != nil {
		return err
	}

	writeTestStep(r.ts, &outputBuf)
	writeCommand(params.Parameter.Command, params.Parameter.Args, &outputBuf)

	switch params.Parameter.Command {
	case "power":
		if err := r.ts.powerCmds(ctx, &outputBuf); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	case "flash":
		if err := r.ts.flashCmds(ctx, &outputBuf); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	default:
		err := fmt.Errorf("Command '%s' is not valid. Possible values are 'power' and 'flash'.", params.Parameter.Command)
		outputBuf.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}
