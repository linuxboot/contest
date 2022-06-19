package waitport

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/storage/memory"
)

func TestWaitForTCPPort(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to start listening TCP port: '%v'", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("Failed to close listener: '%v'", err)
		}
	}()

	ctx, cancel := xcontext.WithCancel(xcontext.Background())
	defer cancel()

	m, err := memory.New()
	if err != nil {
		t.Fatalf("could not initialize memory storage: '%v'", err)
	}
	storageEngineVault := storage.NewSimpleEngineVault()
	if err := storageEngineVault.StoreEngine(m, storage.SyncEngine); err != nil {
		t.Fatalf("Failed to set memory storage: '%v'", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		for ctx.Err() == nil {
			conn, err := listener.Accept()
			if err == nil || conn == nil {
				continue
			}
			_ = conn.Close()
		}
	}()

	inCh := make(chan *target.Target, 1)
	testStepChannels := test.TestStepChannels{
		In:  inCh,
		Out: make(chan test.TestStepResult, 1),
	}
	ev := storage.NewTestEventEmitterFetcher(storageEngineVault, testevent.Header{
		JobID:         12345,
		TestName:      "waitport_tests",
		TestStepLabel: "waitport",
	})

	inCh <- &target.Target{
		ID:   "some_id",
		FQDN: "localhost",
	}
	close(inCh)

	params := test.TestStepParameters{
		"protocol":       []test.Param{*test.NewParam("tcp")},
		"port":           []test.Param{*test.NewParam(fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port))},
		"timeout":        []test.Param{*test.NewParam("2m")},
		"check_interval": []test.Param{*test.NewParam("10ms")},
	}

	plugin := &WaitPort{}
	if _, err = plugin.Run(ctx, testStepChannels, ev, nil, params, nil); err != nil {
		t.Errorf("Plugin run failed: '%v'", err)
	}
	wg.Wait()
}
