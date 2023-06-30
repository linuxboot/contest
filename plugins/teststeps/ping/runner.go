package ping

import (
	"fmt"
	"net"
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

type Error struct {
	Msg string `json:"error"`
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

	var inputParams inputStepParams

	if err := pe.ExpandObject(r.ts.inputStepParams, &inputParams); err != nil {
		err := fmt.Errorf("failed to expand input parameter: %v", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if inputParams.Parameter.Port == 0 {
		inputParams.Parameter.Port = defaultPort
	}

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)

	// for any ambiguity, outcome is an error interface, but it encodes whether the process
	// was launched sucessfully and it resulted in a failure; err means the launch failed
	if err := r.runPing(ctx, &stdoutMsg, &stderrMsg, target, inputParams); err != nil {
		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return nil
}

func (r *TargetRunner) runPing(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target,
	inputParams inputStepParams) error {
	// Set timeout
	timeTimeout := time.After(time.Duration(inputParams.Options.Timeout))
	ticker := time.NewTicker(time.Second)

	writeCommand(fmt.Sprintf("'%s:%d'", inputParams.Parameter.Host, inputParams.Parameter.Port), stdoutMsg, stderrMsg)

	for {
		select {
		case <-timeTimeout:
			err := fmt.Errorf("Timeout, port %d was not opened in time.", inputParams.Parameter.Port)

			stderrMsg.WriteString(fmt.Sprintf("Ping Output:\n%s", err.Error()))

			return err

		case <-ticker.C:
			conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", inputParams.Parameter.Host, inputParams.Parameter.Port))
			if err != nil {
				break
			}
			defer conn.Close()

			stdoutMsg.WriteString(fmt.Sprintf("Ping Output:\nSuccessfully pinged '%s' on port '%d'", inputParams.Parameter.Host, inputParams.Parameter.Port))

			return nil
		}
	}
}
