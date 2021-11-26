// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package memory

import (
	"sort"
	"testing"
	"time"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
	"github.com/stretchr/testify/require"
)

var (
	ctx = logrusctx.NewContext(logger.LevelDebug)
)

func TestMemory_GetTestEvents(t *testing.T) {
	stor, err := New()
	require.NoError(t, err)

	ev0 := testevent.Event{
		EmitTime: time.Now(),
		Header: &testevent.Header{
			JobID:         1,
			RunID:         2,
			TestName:      "3",
			TestStepLabel: "4",
		},
		Data: &testevent.Data{},
	}
	err = stor.StoreTestEvent(ctx, ev0)
	require.NoError(t, err)

	ev1 := testevent.Event{
		EmitTime: time.Now(),
		Header: &testevent.Header{
			JobID:         1,
			RunID:         5,
			TestName:      "test_name",
			TestStepLabel: "test_label_1",
		},
		Data: &testevent.Data{},
	}
	err = stor.StoreTestEvent(ctx, ev1)
	require.NoError(t, err)

	ev2 := testevent.Event{
		EmitTime: time.Now(),
		Header: &testevent.Header{
			JobID:         1,
			RunID:         5,
			TestName:      "test_name",
			TestStepLabel: "test_label_2",
		},
		Data: &testevent.Data{},
	}
	err = stor.StoreTestEvent(ctx, ev2)
	require.NoError(t, err)

	query, err := testevent.BuildQuery(
		testevent.QueryRunID(5),
		testevent.QueryTestName("test_name"),
	)
	require.NoError(t, err)

	evs, err := stor.GetTestEvents(ctx, query)
	require.NoError(t, err)

	require.Len(t, evs, 2)
	sort.Slice(evs, func(i, j int) bool {
		return evs[i].SequenceID < evs[j].SequenceID
	})

	requireEqualExpectSequenceID := func(t *testing.T, lev testevent.Event, rev testevent.Event) {
		lev.SequenceID = 0
		rev.SequenceID = 0
		require.Equal(t, lev, rev)
	}
	requireEqualExpectSequenceID(t, ev1, evs[0])
	requireEqualExpectSequenceID(t, ev2, evs[1])
}
