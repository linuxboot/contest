// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package remote

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendResponseSimple(t *testing.T) {
	prev := os.Stdout
	defer func() {
		os.Stdout = prev
	}()

	r, w, _ := os.Pipe()
	os.Stdout = w

	result := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		result <- buf.Bytes()
	}()

	sid := "42"
	err := SendResponse(&StartMessage{sid})
	require.NoError(t, err)

	// done with capturing stdout
	w.Close()

	var recv StartMessage
	err = json.Unmarshal(<-result, &recv)
	require.NoError(t, err)

	require.Equal(t, sid, recv.SessionID)
}

func TestRecvResponseSimple(t *testing.T) {
	msg := StartMessage{"42"}
	data, err := json.Marshal(&msg)
	require.NoError(t, err)

	var recv StartMessage
	err = RecvResponse(bytes.NewReader(data), &recv)
	require.NoError(t, err)

	require.Equal(t, msg, recv)
}
