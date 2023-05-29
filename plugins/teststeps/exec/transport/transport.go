// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/linuxboot/contest/pkg/test"
)

type Transport interface {
	NewProcess(ctx context.Context, bin string, args []string) (Process, error)
}

// ExitError is returned by Process.Wait when the controlled process exited with
// a non-zero exit code (depending on transport)
type ExitError struct {
	ExitCode int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with non-zero code: %d", e.ExitCode)
}

type Process interface {
	Start(ctx context.Context) error
	Wait(ctx context.Context) error

	StdoutPipe() (io.Reader, error)
	StderrPipe() (io.Reader, error)

	String() string
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
