// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package remote

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type StartMessage struct {
	SessionID string `json:"sid"`
}

type PollMessage struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`

	// ExitCode is non-nil when the controlled process exited
	ExitCode *int `json:"exitcode"`

	// Error is any error encountered while trying to reach the agent
	Error string `json:"error,omitempty"`
}

// SendResponse conveys to the caller a given response object o
func SendResponse(o interface{}) error {
	bytes, err := json.Marshal(o)
	if err != nil {
		return err
	}

	_, err = fmt.Printf("%s\n", string(bytes))
	return err
}

// RecvResponse reads a response object from the given reader
func RecvResponse(r io.Reader, o interface{}) error {
	s := bufio.NewScanner(r)
	if !s.Scan() {
		return fmt.Errorf("no input")
	}
	if s.Err() != nil {
		return s.Err()
	}

	return json.Unmarshal(s.Bytes(), o)
}
