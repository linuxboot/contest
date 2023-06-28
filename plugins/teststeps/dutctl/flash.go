package dutctl

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/9elements/fti/pkg/dut"
	"github.com/9elements/fti/pkg/dutctl"
	"github.com/9elements/fti/pkg/remote_lab/client"
	"github.com/9elements/fti/pkg/tools"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

func (r *TargetRunner) flashCmds(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, target *target.Target) error {
	var (
		err          error
		dutInterface dutctl.DutCtl
		flashOptions dut.FlashOptions
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

	if err = dutInterface.InitPowerPlugins(stdout); err != nil {
		return fmt.Errorf("Failed to init power plugins: %v\n", err)
	}

	if err = dutInterface.InitFlashPlugins(stdout); err != nil {
		return fmt.Errorf("Failed to init programmer plugins: %v\n", err)
	}

	if len(r.ts.Parameter.Args) >= 2 {
		if r.ts.Parameter.Args[1] == "" {
			return fmt.Errorf("No file was set to read, write or verify: %v\n", err)
		}

		switch r.ts.Parameter.Args[0] {
		case "read":
			s, err := dutInterface.FlashSupportsRead()
			if err != nil {
				return fmt.Errorf("Error calling FlashSupportsRead\n")
			}
			if !s {
				return fmt.Errorf("Programmer doesn't support read op\n")
			}

			rom, err := dutInterface.FlashRead()
			if err != nil {
				return fmt.Errorf("Fail to read: %v\n", err)
			}

			if err = ioutil.WriteFile(r.ts.Parameter.Args[1], rom, 0o660); err != nil {
				return fmt.Errorf("Failed to write file: %v\n", err)
			}

			stdoutMsg.WriteString(fmt.Sprintf("Successfully read flash into '%s'.\n", r.ts.Parameter.Args[1]))

		case "write":
			rom, err := ioutil.ReadFile(r.ts.Parameter.Args[1])
			if err != nil {
				return fmt.Errorf("File '%s' could not be read successfully: %v\n", r.ts.Parameter.Args[1], err)
			}

			if err = dutInterface.FlashWrite(rom, &flashOptions); err != nil {
				return fmt.Errorf("Failed to write rom: %v\n", err)
			}

			stdoutMsg.WriteString(fmt.Sprintf("Successfully written flash from '%s'.\n", r.ts.Parameter.Args[1]))

		case "verify":
			s, err := dutInterface.FlashSupportsVerify()
			if err != nil {
				return fmt.Errorf("FlashSupportsVerify returned error: %v\n", err)
			}

			if !s {
				return fmt.Errorf("Programmer doesn't support verify op\n")
			}

			rom, err := ioutil.ReadFile(r.ts.Parameter.Args[1])
			if err != nil {
				return fmt.Errorf("File could not be read successfully: %v", err)
			}

			if err = dutInterface.FlashVerify(rom); err != nil {
				return fmt.Errorf("Failed to verify: %v\n", err)
			}

			stdoutMsg.WriteString(fmt.Sprintf("Successfully verified flash against '%s'.\n", r.ts.Parameter.Args[1]))

		default:
			return fmt.Errorf("Failed to execute the flash command. The argument '%s' is not valid. Possible values are 'read /path/to/binary', 'write /path/to/binary' and 'verify /path/to/binary'.", r.ts.Parameter.Args)
		}
	} else {
		return fmt.Errorf("Failed to execute the power command. Args is not valid. Possible values are 'read /path/to/binary', 'write /path/to/binary' and 'verify /path/to/binary'.")
	}

	return nil
}
