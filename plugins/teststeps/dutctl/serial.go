package dutctl

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/9elements/fti/pkg/dutctl"
	"github.com/9elements/fti/pkg/remote_lab/client"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

var timeout time.Time

func (r *TargetRunner) serialCmds(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target) error {
	var (
		dutInterface dutctl.DutCtl
		err          error
	)

	dutInterface, err = client.NewDutCtl("", false, r.ts.Parameter.Host, false, "", 0, 2)
	if err != nil {
		// Try insecure on port 10000
		if strings.Contains(r.ts.Parameter.Host, ":10001") {
			r.ts.Parameter.Host = strings.Split(r.ts.Parameter.Host, ":")[0] + ":10000"
		}

		dutInterface, err = client.NewDutCtl("", false, r.ts.Parameter.Host, false, "", 0, 2)
		if err != nil {
			return err
		}
	}

	defer func() {
		if dutInterface != nil {
			dutInterface.Close()
		}
	}()

	regexList, err := r.getRegexList()
	if err != nil {
		return err
	}

	if err := r.serial(ctx, stdoutMsg, stderrMsg, dutInterface, regexList); err != nil {
		return err
	}

	return nil
}

func (r *TargetRunner) serial(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, dutInterface dutctl.DutCtl, regexList []*regexp.Regexp) error {
	timeout = time.Now().Add(time.Duration(r.ts.Options.Timeout))

	err := dutInterface.InitSerialPlugins()
	if err != nil {
		return fmt.Errorf("Failed to init serial plugins: %v\n", err)
	}

	iface, err := dutInterface.GetSerial(0)
	if err != nil {
		return fmt.Errorf("Failed to get serial: %v\n", err)
	}

	// Write in into serial
	if r.ts.Parameter.Input != "" {
		if _, err := iface.Write([]byte(r.ts.Parameter.Input)); err != nil {
			return fmt.Errorf("Error writing '%s' to dutctl: %w", r.ts.Parameter.Input, err)
		}

		stdoutMsg.WriteString(fmt.Sprintf("Wrote '%s' to the DUT.\n", r.ts.Parameter.Input))
	}

	if len(r.ts.expectStepParams) > 0 {
		dst, err := os.Create("/tmp/dutctlserial")
		if err != nil {
			return fmt.Errorf("Creating serial dst file failed: %v", err)
		}
		defer dst.Close()

		go func(ctx xcontext.Context) {
			defer func() {
				iface.Close()
			}()
			for {
				select {
				case <-ctx.Done():
					stdoutMsg.WriteString("\n")
					return
				default:
					_, err = io.Copy(dst, iface)
					if err != nil {
						stderrMsg.WriteString(fmt.Sprintf("Failed to copy data from serial to output: %v.\n", err))
					}
				}
			}
		}(ctx)

		stdoutMsg.WriteString("Greping serial from the DUT with the help of the provided regexpressions.\n")

		foundAll := false

		for {
			serial, err := ioutil.ReadFile("/tmp/dutctlserial")
			if err != nil {
				return fmt.Errorf("Failed to read serial file: %v", err)
			}

			if time.Now().After(timeout) {
				ctx.Done()
				r.writeMatches(stdoutMsg, stderrMsg, serial, regexList)
				r.writeSerial(stdoutMsg, stderrMsg, serial)

				return fmt.Errorf("Timed out after %s.", r.ts.Options.Timeout.String())
			}

			foundAll = true

			for _, re := range regexList {
				matches := re.FindAll(serial, -1)
				if len(matches) == 0 {
					foundAll = false
				}
			}

			if foundAll {
				r.writeMatches(stdoutMsg, stderrMsg, serial, regexList)
				r.writeSerial(stdoutMsg, stderrMsg, serial)

				ctx.Done()

				return nil
			}

			time.Sleep(time.Second)
		}
	}

	return nil
}

func (r *TargetRunner) writeMatches(stdoutMsg, stderrMsg *strings.Builder, serial []byte, regexList []*regexp.Regexp) {
	for reIndex, re := range regexList {
		matches := re.FindAllSubmatch(serial, -1)
		if len(matches) == 0 {
			stderrMsg.WriteString(fmt.Sprintf("Could not find the expected regex '%s' in Stdout.\n", r.ts.expectStepParams[reIndex].Regex))
		} else {
			stdoutMsg.WriteString(fmt.Sprintf("Found the expected regex '%s' in Stdout. All matches listed here:\n", r.ts.expectStepParams[reIndex].Regex))
			for maIndex, match := range matches {
				stdoutMsg.WriteString(fmt.Sprintf("Match %d: '%s'\n", maIndex+1, match[0]))
			}
		}
	}

}

func (r *TargetRunner) writeSerial(stdoutMsg, stderrMsg *strings.Builder, serial []byte) {
	stdoutMsg.WriteString(fmt.Sprintf("\nSerial Output:\n%s\n", string(serial)))
	stderrMsg.WriteString(fmt.Sprintf("\nSerial Output:\n%s\n", string(serial)))
}

func (r *TargetRunner) getRegexList() ([]*regexp.Regexp, error) {
	regexList := make([]*regexp.Regexp, len(r.ts.expectStepParams))

	for index := range r.ts.expectStepParams {
		re, err := regexp.Compile(r.ts.expectStepParams[index].Regex)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse the regex '%s': %v", r.ts.expectStepParams[index].Regex, err)
		}

		regexList[index] = re
	}

	return regexList, nil
}
