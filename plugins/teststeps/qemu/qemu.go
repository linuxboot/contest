package qemu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	expect "github.com/google/goexpect"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/multiwriter"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
)

const (
	defaultTimeout = "10m"
	defaultNproc   = "3"
	defaultMemory  = "5000"
)

// Name of the plugin
var Name = "Qemu"

var Events = []event.Name{}

type Qemu struct {
	executable *test.Param
	firmware   *test.Param
	nproc      *test.Param
	mem        *test.Param
	image      *test.Param
	logfile    *test.Param
	timeout    *test.Param
	steps      []test.Param
}

// Needed for the Teststep interface. Returns a Teststep instance.
func New() test.TestStep {
	return &Qemu{}
}

// Needed for the Teststep interface. Returns the Name, the New() Function and
// the events the teststep can emit (which are no events).
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}

// Name returns the name of the Step
func (e Qemu) Name() string {
	return Name
}

// ValidateParameters validates the parameters that will be passed to the Run
// and Resume methods of the test step.
func (ts *Qemu) ValidateParameters(_ xcontext.Context, params test.TestStepParameters) error {
	if ts.executable = params.GetOne("executable"); ts.executable.IsEmpty() {
		return fmt.Errorf("No Qemu executable given")
	}

	if ts.firmware = params.GetOne("firmware"); ts.firmware.IsEmpty() {
		return errors.New("Missing 'firmware' field in qemu parameters")
	}

	if ts.nproc = params.GetOne("nproc"); ts.nproc.IsEmpty() {
		ts.nproc = test.NewParam(defaultNproc)
	}
	if ts.mem = params.GetOne("mem"); ts.mem.IsEmpty() {
		ts.mem = test.NewParam(defaultMemory)
	}
	if ts.timeout = params.GetOne("timeout"); ts.timeout.IsEmpty() {
		ts.timeout = test.NewParam(defaultTimeout)
	}

	ts.image = params.GetOne("image")

	ts.steps = params.Get("steps")

	ts.logfile = params.GetOne("logfile")

	return nil
}

// A more appropiatly named wrapper around the mandatory ValidateParameters function.
func (q *Qemu) validateAndPopulate(ctx xcontext.Context, params test.TestStepParameters) error {
	return q.ValidateParameters(ctx, params)
}

// Run starts the Qemu instance for each target and interacts with the qemu instance
// through the expect and send steps.
func (q *Qemu) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	log := ctx.Logger()

	if err := q.ValidateParameters(ctx, params); err != nil {
		return nil, err
	}
	f := func(ctx xcontext.Context, target *target.Target) error {
		targetTimeout, err := q.timeout.Expand(target)
		if err != nil {
			return err
		}

		targetLogfile, err := q.logfile.Expand(target)
		if err != nil {
			return err
		}

		targetImage, err := q.image.Expand(target)
		if err != nil {
			return err
		}

		targetQemu, err := q.executable.Expand(target)
		if err != nil {
			return err
		}

		// basic checks whether the executable is usable
		if abs := filepath.IsAbs(targetQemu); abs == false {
			_, err := exec.LookPath(targetQemu)
			if err != nil {
				return fmt.Errorf("unable to find qemu executable in PATH: %w", err)
			}
		}

		targetFirmware, err := q.firmware.Expand(target)
		if err != nil {
			return err
		}

		targetMem, err := q.mem.Expand(target)
		if err != nil {
			return err
		}

		targetNproc, err := q.nproc.Expand(target)
		if err != nil {
			return err
		}

		globalTimeout, err := time.ParseDuration(targetTimeout)
		if err != nil {
			return fmt.Errorf("Could not Parse %v as Timeout: %w", targetTimeout, err)
		}

		// no graphical output and no network access
		command := []string{targetQemu, "-nographic", "-nic", "none", "-bios", targetFirmware}
		qemuOpts := []string{"-m", targetMem, "-smp", targetNproc}

		command = append(command, qemuOpts...)
		if targetImage != "" {
			command = append(command, targetImage)
		}

		var logfile *os.File
		if targetLogfile != "" {

			logfile, err = os.Create(targetLogfile)
			if err != nil {
				return fmt.Errorf("Could not create Logfile: %w", err)
			}
			defer logfile.Close()
		} else {
			logfile, err = os.OpenFile("/dev/null", os.O_WRONLY, fs.ModeDevice)
			if err != nil {
				return fmt.Errorf("Could not redirect output to '/dev/null': %w", err)
			}
			defer logfile.Close()
		}

		mw := multiwriter.NewMultiWriter()
		if ctx.Writer() != nil {
			mw.AddWriter(ctx.Writer())
		}
		mw.AddWriter(logfile)

		gExpect, errchan, err := expect.SpawnWithArgs(
			command,
			globalTimeout,
			expect.Tee(mw),
			expect.CheckDuration(time.Minute),
			expect.PartialMatch(false),
			expect.SendTimeout(globalTimeout),
		)
		if err != nil {
			return fmt.Errorf("Could not start qemu: %w", err)
		}

		log.Infof("Started Qemu with command: %v", command)

		defer gExpect.Close()

		defer func() {
			select {
			case err = <-errchan:
				log.Errorf("Error from Qemu: %v", err)
			default:

			}
		}()

		// struct to capture expect and send strings from json.
		type expector struct {
			Expect  string
			Send    string
			Timeout string
		}

		// loop over all steps and expect/ send the given strings
		for _, interaction := range q.steps {

			dst := new(expector)

			jsString, err := interaction.Expand(target)
			if err != nil {
				return err
			}

			interactionParam := test.NewParam(jsString)
			js := interactionParam.JSON()

			if err := json.Unmarshal(js, dst); err != nil {
				return fmt.Errorf("Could not Unmarshal steps: %w", err)
			}

			// Expect and Send fields must not both be empty
			if dst.Expect+dst.Send == "" {
				return fmt.Errorf("%s is not a valid step statement", interaction.String())
			}

			// process expect step
			if dst.Expect != "" {

				var timeout time.Duration
				if dst.Timeout == "" {
					timeout = globalTimeout
				} else {
					if timeout, err = time.ParseDuration(dst.Timeout); err != nil {
						return fmt.Errorf("Could not parse timeout '%s' for step: '%s'. %w", dst.Timeout, dst.Expect, err)
					}
				}

				if _, _, err := gExpect.Expect(regexp.MustCompile(dst.Expect), timeout); err != nil {
					return fmt.Errorf("Error while expecting '%s': %w", dst.Expect, err)
				}

				log.Debugf("Completed expect step: '%v' with timeout: %v \n", dst.Expect, timeout.String())

			}

			// process send step
			if dst.Send != "" {

				if err := gExpect.Send(dst.Send + "\n"); err != nil {
					return fmt.Errorf("Unable to send '%s': %w", dst.Send, err)
				}

				// notify the user if the timeout field is used incorrectly
				if dst.Expect == "" && dst.Timeout != "" {
					log.Warnf("The Timeout %v for send step: %v will be ignored.", dst.Timeout, dst.Send)
				}

				log.Debugf("Completed Send Step: '%v'", dst.Send)
			}
		}

		log.Infof("Matching steps successful")
		return nil
	}
	return teststeps.ForEachTarget(Name, ctx, ch, f)
}
