package dutctl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/9elements/fti/pkg/dutctl"
	"github.com/9elements/fti/pkg/remote_lab/client"
	"github.com/9elements/fti/pkg/tools"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

func (r *TargetRunner) powerCmds(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target) error {
	var (
		err          error
		dutInterface dutctl.DutCtl
		stdout       tools.LogginFunc
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

	err = dutInterface.InitPowerPlugins(stdout)
	if err != nil {
		return fmt.Errorf("Failed to init power plugins: %v\n", err)
	}

	if len(r.ts.Parameter.Args) >= 1 {
		switch r.ts.Parameter.Args[0] {
		case "on":
			if err := dutInterface.PowerOn(); err != nil {
				return fmt.Errorf("Failed to power on: %v\n", err)
			}

			stdoutMsg.WriteString("Successfully powered on DUT.\n")

			if len(r.ts.expectStepParams) != 0 {
				regexList, err := r.getRegexList()
				if err != nil {
					return err
				}

				if err := r.serial(ctx, stdoutMsg, stderrMsg, dutInterface, regexList); err != nil {
					return fmt.Errorf("the expect '%s' was not found in the logs", r.ts.expectStepParams)
				}
			}

		case "off":
			if err := dutInterface.PowerOff(); err != nil {
				return fmt.Errorf("Failed to power off: %v\n", err)
			}

			stdoutMsg.WriteString("Successfully powered off DUT.\n")

		case "powercycle":
			if len(r.ts.Parameter.Args) == 2 {
				reboots, err := strconv.Atoi(r.ts.Parameter.Args[1])
				if err != nil {
					return fmt.Errorf("powercycle amount could not be parsed: %v\n", err)
				}

				regexList, err := r.getRegexList()
				if err != nil {
					return err
				}

				for i := 1; i < reboots; i++ {
					err = dutInterface.PowerOn()
					if err != nil {
						return fmt.Errorf("Failed to power on: %v\n", err)
					}
					if err := r.serial(ctx, stdoutMsg, stderrMsg, dutInterface, regexList); err != nil {
						return fmt.Errorf("the expect '%v' was not found in the logs", r.ts.expectStepParams)
					}

					err = dutInterface.PowerOff()
					if err != nil {
						return fmt.Errorf("Failed to power off: %v\n", err)
					}
				}

				stdoutMsg.WriteString(fmt.Sprintf("Successfully powercycled the DUT '%s'.\n", r.ts.Parameter.Args[1]))
			} else {
				return fmt.Errorf("You have to add only a second argument to specify how often you want to powercycle.")
			}

		default:
			return fmt.Errorf("Failed to execute the power command. The argument '%s' is not valid. Possible values are 'on', 'off' and 'powercycle'.", r.ts.Parameter.Args)
		}
	} else {
		return fmt.Errorf("Failed to execute the power command. Args is empty. Possible values are 'on', 'off' and 'powercycle'.")
	}

	return nil
}
