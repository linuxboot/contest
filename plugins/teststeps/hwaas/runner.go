package hwaas

import (
	"fmt"
	"io"
	"net/http"
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

const (
	power    = "power"
	flash    = "flash"
	keyboard = "keyboard"
)

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
	case power:
		if err := r.ts.powerCmds(ctx, &outputBuf); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	case flash:
		if err := r.ts.flashCmds(ctx, &outputBuf); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	case keyboard:
		if err := r.ts.keyboardCmds(ctx, &outputBuf); err != nil {
			outputBuf.WriteString(fmt.Sprintf("%v\n", err))

			return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
		}

	default:
		err := fmt.Errorf("Command '%s' is not valid. Possible values are 'power', 'flash' and 'keyboard'.", params.Parameter.Command)
		outputBuf.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

// HTTPRequest triggerers a http request and returns the response. The parameter that can be set are:
// method: can be every http method
// endpoint: api endpoint that shall be requested
// body: the body of the request
func HTTPRequest(ctx xcontext.Context, method string, endpoint string, body io.Reader) (*http.Response, error) {
	client := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
