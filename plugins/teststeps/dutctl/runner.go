package dutctl

import (
	"fmt"
	"os"
	"path/filepath"
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
		err := fmt.Errorf("failed to expand input parameter: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if r.ts.Parameter.Host != "" {
		var certFile string

		fp := "/" + filepath.Join("etc", "fti", "keys", "ca-cert.pem")
		if _, err := os.Stat(fp); err == nil {
			certFile = fp
		}

		if !strings.Contains(r.ts.Parameter.Host, ":") {
			// Add default port
			if certFile == "" {
				r.ts.Parameter.Host += ":10000"
			} else {
				r.ts.Parameter.Host += ":10001"
			}
		}
	}

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)
	writeCommand(params.Parameter.Command, params.Parameter.Args, &stdoutMsg, &stderrMsg)

	stdoutMsg.WriteString("Stdout:\n")
	stderrMsg.WriteString("Stderr:\n")

	switch r.ts.Parameter.Command {
	case "power":
		if err := r.powerCmds(ctx, &stdoutMsg, &stderrMsg, target); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}
	case "flash":
		if err := r.flashCmds(ctx, &stdoutMsg, &stderrMsg, target); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	case "serial":
		if err := r.serialCmds(ctx, &stdoutMsg, &stderrMsg, target); err != nil {
			stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
		}

	default:
		return fmt.Errorf("Command '%s' is not valid. Possible values are 'power', 'flash' and 'serial'.", params.Parameter.Command)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return nil
}
