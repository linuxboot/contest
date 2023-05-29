//go:build flakytests

package signaling

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGC(t *testing.T) {
	// parent
	ctx := context.Background()
	ctx, _ = WithSignal(ctx, unitTestCustomSignal0)

	gc := func() {
		runtime.GC()
		runtime.Gosched()
		runtime.GC()
		runtime.Gosched()
	}

	// mem stats before the child
	var (
		memStatsBefore runtime.MemStats
		memStatsAfter  runtime.MemStats
	)
	gc()
	runtime.ReadMemStats(&memStatsBefore)

	// short-living child
	WithSignal(ctx, unitTestCustomSignal1)

	// mem stats after the child
	gc()
	runtime.ReadMemStats(&memStatsAfter)

	require.Equal(t, memStatsBefore.HeapInuse, memStatsAfter.HeapInuse)
}
