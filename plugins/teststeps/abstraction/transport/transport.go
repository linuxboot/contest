package transport

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
)

type Process interface {
	Start(ctx xcontext.Context) error
	Wait(ctx xcontext.Context) error

	StdoutPipe() (io.Reader, error)
	StderrPipe() (io.Reader, error)

	String() string
}

type Copy interface {
	Copy(ctx xcontext.Context) error

	String() string
}

type Transport interface {
	NewProcess(ctx xcontext.Context, bin string, args []string) (Process, error)
	NewCopy(ctx xcontext.Context, source, destination string, recursive bool) (Copy, error)
}

func NewTransport(proto string, configSource json.RawMessage, expander *test.ParamExpander) (Transport, error) {
	switch proto {
	case "local":
		return NewLocalTransport(), nil

	case "ssh":
		configTempl := DefaultSSHTransportConfig()
		if err := json.Unmarshal(configSource, &configTempl); err != nil {
			return nil, fmt.Errorf("unable to deserialize transport options: %w", err)
		}

		var config SSHTransportConfig
		if err := expander.ExpandObject(configTempl, &config); err != nil {
			return nil, err
		}

		return NewSSHTransport(config), nil

	default:
		return nil, fmt.Errorf("no such transport: %v", proto)
	}
}

// ExitError is returned by Process.Wait when the controlled process exited with
// a non-zero exit code (depending on transport)
type ExitError struct {
	ExitCode int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with non-zero code: %d", e.ExitCode)
}
