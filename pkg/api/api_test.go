// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package api

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/linuxboot/contest/pkg/job"

	"github.com/facebookincubator/go-belt/tool/logger"

	"github.com/stretchr/testify/require"
)

var ctx = logger.CtxWithLogger(context.Background(), logger.Default())

func TestOptions(t *testing.T) {
	eventTimeout := 3141592654 * time.Second
	serverID := "myUnitTestServerID"
	api, err := New(
		OptionEventTimeout(eventTimeout),
		OptionServerID(serverID),
	)
	require.NoError(t, err)
	require.Equal(t, eventTimeout, api.Config.EventTimeout)
	require.Equal(t, serverID, api.serverID)
}

type dummyEventMsg struct{}

func (d dummyEventMsg) Requestor() EventRequestor {
	return "unit-test"
}

func TestEventTimeout(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		apiInstance, err := New(OptionServerID("unit-test"), OptionEventTimeout(time.Nanosecond))
		require.NoError(t, err)
		t.Run("Status", func(t *testing.T) {
			startTime := time.Now()
			resp, err := apiInstance.Status(ctx, "unit-test", 0)
			require.Error(t, err)
			require.Nil(t, resp.Data)
			require.Less(t, time.Since(startTime).Nanoseconds(), DefaultEventTimeout.Nanoseconds())
		})

		t.Run("SendReceiveEvent", func(t *testing.T) {
			startTime := time.Now()
			resp, err := apiInstance.SendReceiveEvent(&Event{
				RespCh: make(chan *EventResponse),
				Msg:    dummyEventMsg{},
			}, nil)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Less(t, time.Since(startTime).Nanoseconds(), DefaultEventTimeout.Nanoseconds())
		})
	})

	t.Run("noTimeout", func(t *testing.T) {
		apiInstance, err := New(OptionServerID("unit-test"))
		require.NoError(t, err)

		respExpected := &EventResponse{
			Requestor: dummyEventMsg{}.Requestor(),
			JobID:     1,
			Status: &job.Status{
				Name: "unit-test",
			},
		}

		ctx, cancelFunc := context.WithCancel(ctx)
		defer cancelFunc()
		go func() {
			for {
				select {
				case <-ctx.Done():
				case ev := <-apiInstance.Events:
					runtime.Gosched()
					ev.RespCh <- respExpected
				}
			}
		}()
		resp, err := apiInstance.Status(ctx, "unit-test", 0)
		require.NoError(t, err)
		require.IsType(t, ResponseDataStatus{}, resp.Data)
		require.Equal(t, resp.Data.(ResponseDataStatus).Status, respExpected.Status)
	})
}
