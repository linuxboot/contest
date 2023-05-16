package ping

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

// Name is the name used to look this plugin up.
var Name = "Ping"

// Events is used by the framework to determine which events this plugin will
// emit. Any emitted event that is not registered here will cause the plugin to
// fail.
var Events = []event.Name{}

const (
	defaultSSHPort          = 22
	defaultTimeoutParameter = "10m"
)

// Ping is used to copy a file over ssh to a destination.
type Ping struct {
	Host    *test.Param
	Port    *test.Param
	Timeout *test.Param
}

// Name returns the plugin name.
func (p Ping) Name() string {
	return Name
}

// Run executes the cmd step.
func (p *Ping) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	log := ctx.Logger()

	// XXX: Dragons ahead! The target (%t) substitution, and function
	// expression evaluations are done at run-time, so they may still fail
	// despite passing at early validation time.
	// If the function evaluations called in validateAndPopulate are not idempotent,
	// the output of the function expressions may be different (e.g. with a call to a
	// backend or a random pool of results)
	// Function evaluation could be done at validation time, but target
	// substitution cannot, because the targets are not known at that time.
	if err := p.validateAndPopulate(params); err != nil {
		return nil, err
	}

	f := func(ctx xcontext.Context, target *target.Target) error {
		host, err := p.Host.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand host parameter: %v", err)
		}

		if len(host) == 0 {
			return fmt.Errorf("hostname is empty, please provide a correct hostname")
		}

		port, err := p.Port.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand port parameter: %v", err)
		}

		timeoutStr, err := p.Timeout.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand timeout parameter %s: %v", timeoutStr, err)
		}

		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("cannot parse timeout paramter: %v", err)
		}

		timeTimeout := time.After(timeout)
		ticker := time.NewTicker(time.Second)

		time.Sleep(time.Second)

		for {
			select {
			case <-timeTimeout:
				return fmt.Errorf("timeout, port %s was not opened in time", port)
			case <-ticker.C:
				conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", host, port))
				if err != nil {
					break
				}
				defer conn.Close()

				log.Infof("successfully knocked on port %s", port)

				return nil
			}
		}
	}

	return teststeps.ForEachTarget(Name, ctx, ch, f)
}

func (p *Ping) validateAndPopulate(params test.TestStepParameters) error {
	var err error
	p.Host = params.GetOne("host")
	if p.Host.IsEmpty() {
		return errors.New("invalid or missing 'host' parameter, must be exactly one string")
	}
	if params.GetOne("port").IsEmpty() {
		p.Port = test.NewParam(strconv.Itoa(defaultSSHPort))
	} else {
		var port int64
		port, err = params.GetInt("port")
		if err != nil {
			return fmt.Errorf("invalid 'port' parameter, not an integer: %v", err)
		}
		if port < 0 || port > 0xffff {
			return fmt.Errorf("invalid 'port' parameter: not in range 0-65535")
		}
	}

	if params.GetOne("timeout").IsEmpty() {
		p.Timeout = test.NewParam(defaultTimeoutParameter)
	} else {
		p.Timeout = params.GetOne("timeout")
	}

	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (p *Ping) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	ctx.Debugf("Params %+v", params)
	return p.validateAndPopulate(params)
}

// New initializes and returns a new SSHCmd test step.
func New() test.TestStep {
	return &Ping{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}
