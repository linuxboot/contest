package copy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
	sshTransport "github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
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

	transport, err := transport.NewTransport(params.Transport.Proto, params.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("Failed to create transport: %w", err)
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	writeTestStep(r.ts, &stdoutMsg, &stderrMsg)

	if err := r.runCopy(ctx, &stdoutMsg, &stderrMsg, target, transport, params); err != nil {
		stderrMsg.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, EventStderr, stderrMsg.String(), target, r.ev, err)
	}

	if err := emitEvent(ctx, EventStdout, eventPayload{Msg: stdoutMsg.String()}, target, r.ev); err != nil {
		return fmt.Errorf("cannot emit event: %v", err)
	}

	return nil
}

func (r *TargetRunner) runCopy(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target,
	transport transport.Transport, params inputStepParams,
) error {
	copy, err := transport.NewCopy(ctx, params.Parameter.SrcPath, params.Parameter.DstPath, r.ts.Parameter.Recursive)
	if err != nil {
		return fmt.Errorf("Failed to copy data to target: %v", err)
	}

	if params.Transport.Proto == "ssh" {
		config := sshTransport.DefaultSSHTransportConfig()
		if err := json.Unmarshal(params.Transport.Options, &config); err != nil {
			return fmt.Errorf("Failed to unmarshal Transport options: %v", err)
		}

		writeCommand(copy.String(), fmt.Sprintf("%s:%d", config.Host, config.Port), stdoutMsg, stderrMsg)
	} else {
		writeCommand(copy.String(), "localhost", stdoutMsg, stderrMsg)
	}

	if err := copy.Copy(ctx); err != nil {
		return fmt.Errorf("Failed to copy data: %v", err)
	}

	writeCommandOutput(stdoutMsg, fmt.Sprintf("Successfully copied %s to %s", params.Parameter.SrcPath, params.Parameter.DstPath))

	return nil
}
