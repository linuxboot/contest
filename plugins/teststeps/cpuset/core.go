package cpuset

import (
	"fmt"
	"strings"

	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
)

const (
	activate   = "activate"
	deactivate = "deactivate"
)

// coreCmds is a helper function to call into the different core commands
func (ts *TestStep) coreCmd(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder, transport transport.Transport) error {
	if ts.Parameter.Args[0] != activate && ts.Parameter.Args[0] != deactivate {
		return fmt.Errorf("Wrong argument for command '%s'. Your options for this command are: '%s' and '%s'.",
			ts.Parameter.Command, activate, deactivate)
	}

	if len(ts.Parameter.Args) >= 1 {
		switch ts.Parameter.Args[0] {
		case activate:
			if err := ts.setCore(ctx, stdoutMsg, stderrMsg, transport, "--on"); err != nil {
				return err
			}

			return nil

		case deactivate:
			if err := ts.setCore(ctx, stdoutMsg, stderrMsg, transport, "--off"); err != nil {
				return err
			}

			return nil

		default:
			return fmt.Errorf("failed to execute the '%s' command. The argument '%s' is not valid. Possible values are '%s' and '%s'.",
				core, ts.Parameter.Args[0], activate, deactivate)
		}

	} else {
		return fmt.Errorf("failed to execute the '%s' command. Args is empty. Possible values are '%s' and '%s'.",
			core, activate, deactivate)
	}
}

func (ts *TestStep) setCore(ctx xcontext.Context, stdoutMsg, stderrMsg *strings.Builder,
	transp transport.Transport, statusFlag string,
) error {
	for _, core := range ts.Parameter.Cores {
		if core == 0 {
			stdoutMsg.WriteString("Stdout:\nCore '0' cannot be activated/deactivated.\n")
			stderrMsg.WriteString("Stderr:\nCore '0' cannot be activated/deactivated.\n")

			continue
		}

		args := []string{
			ts.Parameter.ToolPath,
			cmd,
			"switch",
			fmt.Sprintf("--core=%d", core),
			statusFlag,
			jsonFlag,
		}

		proc, err := transp.NewProcess(ctx, privileged, args, "")
		if err != nil {
			return fmt.Errorf("Failed to create proc: %w", err)
		}

		stdoutPipe, err := proc.StdoutPipe()
		if err != nil {
			return fmt.Errorf("Failed to pipe stdout: %v", err)
		}

		stderrPipe, err := proc.StderrPipe()
		if err != nil {
			return fmt.Errorf("Failed to pipe stderr: %v", err)
		}

		// try to start the process, if that succeeds then the outcome is the result of
		// waiting on the process for its result; this way there's a semantic difference
		// between "an error occured while launching" and "this was the outcome of the execution"
		outcome := proc.Start(ctx)
		if outcome == nil {
			outcome = proc.Wait(ctx)
		}

		stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe)

		if outcome != nil {
			stderrMsg.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
			return fmt.Errorf("Failed to '%s' core '%d': %v.", ts.Parameter.Args[0], core, outcome)
		}

		stdoutMsg.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
		stderrMsg.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
	}

	return nil
}
