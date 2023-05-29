// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/linuxboot/contest/plugins/reporters/noop"
	"github.com/linuxboot/contest/plugins/reporters/targetsuccess"
	"github.com/linuxboot/contest/plugins/targetlocker/dblocker"
	"github.com/linuxboot/contest/plugins/targetmanagers/targetlist"
	"github.com/linuxboot/contest/plugins/testfetchers/literal"
	"github.com/linuxboot/contest/plugins/teststeps/cmd"
	"github.com/linuxboot/contest/plugins/teststeps/sleep"
	"github.com/linuxboot/contest/plugins/teststeps/variables"
	"github.com/linuxboot/contest/plugins/teststeps/waitport"
	testsCommon "github.com/linuxboot/contest/tests/common"
	"github.com/linuxboot/contest/tests/common/goroutine_leak_check"
	"github.com/linuxboot/contest/tests/integ/common"
	"github.com/linuxboot/contest/tests/plugins/targetlist_with_state"

	"github.com/linuxboot/contest/cmds/clients/contestcli/cli"
	"github.com/linuxboot/contest/cmds/contest/server"
)

// NB: When adding a test here you need to invoke it explicitly from docker/contest/tests.sh

var (
	ctx = logging.WithBelt(context.Background(), logger.LevelDebug)
)

type E2ETestSuite struct {
	suite.Suite

	dbURI string
	st    storage.Storage

	serverPort int
	serverSigs chan<- os.Signal
	serverDone <-chan struct{}
}

func (ts *E2ETestSuite) SetupSuite() {
	// Find a random available port to use.
	ln, err := net.Listen("tcp", ":0")
	require.NoError(ts.T(), err)
	parts := strings.Split(ln.Addr().String(), ":")
	ts.serverPort, _ = strconv.Atoi(parts[len(parts)-1])
	ln.Close()
}

func (ts *E2ETestSuite) TearDownSuite() {
	logging.Infof(ctx, "Teardown")
	if !ts.T().Failed() { // Only check for goroutine leaks if otherwise ok.
		require.NoError(ts.T(), goroutine_leak_check.CheckLeakedGoRoutines())
	}
}

func (ts *E2ETestSuite) SetupTest() {
	ts.dbURI = common.GetDatabaseURI()
	logging.Infof(ctx, "DB URI: %s", ts.dbURI)
	st, err := common.NewStorage()
	require.NoError(ts.T(), err)
	ts.st = st
	require.NoError(ts.T(), st.(storage.ResettableStorage).Reset())
	tl, err := dblocker.New(common.GetDatabaseURI())
	require.NoError(ts.T(), err)
	_ = tl.ResetAllLocks(ctx)
	tl.Close()
}

func (ts *E2ETestSuite) TearDownTest() {
	if ts.st != nil {
		ts.st.Close()
	}
}

func (ts *E2ETestSuite) startServer(extraArgs ...string) {
	args := []string{
		fmt.Sprintf("--listenAddr=localhost:%d", ts.serverPort),
		"--logLevel=debug",
		"--dbURI", ts.dbURI,
	}
	args = append(args, extraArgs...)
	serverSigs := make(chan os.Signal)
	serverDone := make(chan struct{})
	serverErr := make(chan error, 1)
	go func() {
		pc := server.PluginConfig{
			TargetManagerLoaders: []target.TargetManagerLoader{
				targetlist.Load,
				targetlist_with_state.Load,
			},
			TestFetcherLoaders: []test.TestFetcherLoader{literal.Load},
			TestStepLoaders:    []test.TestStepLoader{cmd.Load, sleep.Load, waitport.Load, variables.Load},
			ReporterLoaders:    []job.ReporterLoader{targetsuccess.Load, noop.Load},
		}
		err := server.Main(&pc, "contest", args, serverSigs)
		serverErr <- err
		close(serverDone)
	}()
	ts.serverDone = serverDone
	ts.serverSigs = serverSigs
	for i := 0; i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
		conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", ts.serverPort))
		if err != nil {
			continue
		}
		conn.Close()
		logging.Infof(ctx, "Server is up")
		return
	}
	err := <-serverErr
	if err != nil {
		require.NoError(ts.T(), fmt.Errorf("Server failed to initialize: %w", err))
	} else {
		require.NoError(ts.T(), fmt.Errorf("Server failed to initialize but no error was returned"))
	}
}

func (ts *E2ETestSuite) startJob(descriptorFile string) types.JobID {
	// No jobs to begin with.
	var listResp api.ListResponse
	_, err := ts.runClient(&listResp, "list")
	require.NoError(ts.T(), err)
	require.Empty(ts.T(), listResp.Data.JobIDs)

	// Start a job.
	var resp api.StartResponse
	_, err = ts.runClient(&resp, "start", "-Y", descriptorFile)
	require.NoError(ts.T(), err)
	logging.Infof(ctx, "%+v", resp)
	require.NotEqual(ts.T(), 0, resp.Data.JobID)
	return resp.Data.JobID
}

func (ts *E2ETestSuite) stopServer(timeout time.Duration) error {
	if ts.serverSigs == nil {
		return nil
	}
	logging.Infof(ctx, "Stopping server...")
	var err error
	select {
	case ts.serverSigs <- syscall.SIGTERM:
	default:
	}
	close(ts.serverSigs)
	shutdownTimeout := 5 * time.Second
	select {
	case <-ts.serverDone:
	case <-time.After(shutdownTimeout):
		err = fmt.Errorf("Server failed to exit within %s", shutdownTimeout)
	}
	ts.serverSigs = nil
	ts.serverDone = nil
	logging.Infof(ctx, "Server stopped, err %v", err)
	return err
}

func (ts *E2ETestSuite) runClient(resp interface{}, extraArgs ...string) (string, error) {
	args := []string{
		fmt.Sprintf("--addr=http://localhost:%d", ts.serverPort),
	}
	args = append(args, extraArgs...)
	stdout := &bytes.Buffer{}
	err := cli.CLIMain("contestcli", args, stdout)
	if err == nil && resp != nil {
		err = json.Unmarshal(stdout.Bytes(), resp)
		if err != nil {
			err = fmt.Errorf("%w, output:\n%s", err, stdout.String())
		}
	}
	return stdout.String(), err
}

func (ts *E2ETestSuite) TestCLIErrors() {
	var err error
	_, err = ts.runClient(nil, "--help")
	require.NoError(ts.T(), err)
	_, err = ts.runClient(nil)
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "INVALID")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "start", "NOTFOUND")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "start", "--invalid")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "stop")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "stop", "123")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "status")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "status", "123")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "retry")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "retry", "123")
	require.Error(ts.T(), err)
	_, err = ts.runClient(nil, "version")
	require.Error(ts.T(), err)
}

func (ts *E2ETestSuite) TestSimple() {
	ts.startServer()
	jobID := ts.startJob("test-simple.yaml")
	{ // Wait for the job to finish
		var resp api.StatusResponse
		for i := 1; i < 5; i++ {
			time.Sleep(1 * time.Second)
			stdout, err := ts.runClient(&resp, "status", fmt.Sprintf("%d", jobID))
			require.NoError(ts.T(), err)
			require.Nil(ts.T(), resp.Err, "error: %s", resp.Err)
			logging.Infof(ctx, "Job %d state %s", jobID, resp.Data.Status.State)
			if resp.Data.Status.State == string(job.EventJobCompleted) {
				logging.Debugf(ctx, "Job %d status: %s", jobID, stdout)
				break
			}
		}
		require.Equal(ts.T(), string(job.EventJobCompleted), resp.Data.Status.State)
	}
	{ // Verify step output.
		es := testsCommon.GetJobEventsAsString(ctx, ts.st, jobID, []event.Name{
			cmd.EventCmdStdout, target.EventTargetAcquired, target.EventTargetReleased,
		})
		logging.Debugf(ctx, "%s", es)
		require.Equal(ts.T(),
			fmt.Sprintf(`
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetAcquired]}
{[%d 1 Test 1 0 Test1Step1][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 1 Test 1 0 Test1Step2][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 2, target T1\\n\"}"]}
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetReleased]}
{[%d 1 Test 2 0 ][Target{ID: "T2"} TargetAcquired]}
{[%d 1 Test 2 0 Test2Step1][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 1, target T2\\n\"}"]}
{[%d 1 Test 2 0 Test2Step2][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 2, target T2\\n\"}"]}
{[%d 1 Test 2 0 ][Target{ID: "T2"} TargetReleased]}
{[%d 2 Test 1 0 ][Target{ID: "T1"} TargetAcquired]}
{[%d 2 Test 1 0 Test1Step1][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 2 Test 1 0 Test1Step2][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 2, target T1\\n\"}"]}
{[%d 2 Test 1 0 ][Target{ID: "T1"} TargetReleased]}
{[%d 2 Test 2 0 ][Target{ID: "T2"} TargetAcquired]}
{[%d 2 Test 2 0 Test2Step1][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 1, target T2\\n\"}"]}
{[%d 2 Test 2 0 Test2Step2][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 2, target T2\\n\"}"]}
{[%d 2 Test 2 0 ][Target{ID: "T2"} TargetReleased]}
`, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID),
			es,
		)
	}
	require.NoError(ts.T(), ts.stopServer(5*time.Second))
}

func (ts *E2ETestSuite) TestVariables() {
	ts.startServer()
	jobID := ts.startJob("test-variables.yaml")

	{ // Wait for the job to finish
		var resp api.StatusResponse
		for i := 1; i < 5; i++ {
			time.Sleep(1 * time.Second)
			stdout, err := ts.runClient(&resp, "status", fmt.Sprintf("%d", jobID))
			require.NoError(ts.T(), err)
			require.Nil(ts.T(), resp.Err, "error: %s", resp.Err)
			logging.Infof(ctx, "Job %d state %s", jobID, resp.Data.Status.State)
			if resp.Data.Status.State == string(job.EventJobCompleted) {
				logging.Debugf(ctx, "Job %d status: %s", jobID, stdout)
				break
			}
		}
		require.Equal(ts.T(), string(job.EventJobCompleted), resp.Data.Status.State)
	}
	{ // Verify step output.
		es := testsCommon.GetJobEventsAsString(ctx, ts.st, jobID, []event.Name{
			cmd.EventCmdStdout, target.EventTargetAcquired, target.EventTargetReleased,
		})
		logging.Debugf(ctx, "%s", es)
		require.Equal(ts.T(),
			fmt.Sprintf(`
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetAcquired]}
{[%d 1 Test 1 0 cmdstep][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Hello\\n\"}"]}
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetReleased]}
`, jobID, jobID, jobID),
			es,
		)
	}
	require.NoError(ts.T(), ts.stopServer(5*time.Second))
}

func (ts *E2ETestSuite) TestPauseResume() {
	ts.startServer("--pauseTimeout=60s", "--resumeJobs")
	jobID := ts.startJob("test-resume.yaml")

	start := time.Now()
	{ // Stop/start the server up to 20 times or until the job completes.
		var resp api.StatusResponse
	wait_loop:
		for i := 1; i < 20; i++ {
			time.Sleep(1 * time.Second)
			_, err := ts.runClient(&resp, "status", fmt.Sprintf("%d", jobID))
			require.NoError(ts.T(), err)
			require.Nil(ts.T(), resp.Err, "error: %s", resp.Err)
			logging.Infof(ctx, "Job %d state %s", jobID, resp.Data.Status.State)
			switch resp.Data.Status.State {
			case string(job.EventJobCompleted):
				logging.Debugf(ctx, "Job %d completed after %d restarts", jobID, i)
				break wait_loop
			case string(job.EventJobFailed):
				require.Failf(ts.T(), "job failed", "Job %d failed after %d restarts", jobID, i)
				return
			}
			require.NoError(ts.T(), ts.stopServer(5*time.Second))
			ts.startServer("--pauseTimeout=60s", "--resumeJobs")
		}
		require.Equal(ts.T(), string(job.EventJobCompleted), resp.Data.Status.State)
	}
	finish := time.Now()
	{ // Verify step output.
		es := testsCommon.GetJobEventsAsString(ctx, ts.st, jobID, []event.Name{
			cmd.EventCmdStdout, target.EventTargetAcquired, target.EventTargetReleased,
		})
		logging.Debugf(ctx, "%s", es)
		require.Equal(ts.T(),
			fmt.Sprintf(`
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetAcquired]}
{[%d 1 Test 1 0 Test1Step1][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 1 Test 1 0 Test1Step4][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 3, target T1\\n\"}"]}
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetReleased]}
{[%d 1 Test 2 0 ][Target{ID: "T2"} TargetAcquired]}
{[%d 1 Test 2 0 Test2Step1][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 1, target T2\\n\"}"]}
{[%d 1 Test 2 0 Test2Step2][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"\"}"]}
{[%d 1 Test 2 0 Test2Step3][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 3, target T2\\n\"}"]}
{[%d 1 Test 2 0 ][Target{ID: "T2"} TargetReleased]}
{[%d 2 Test 1 0 ][Target{ID: "T1"} TargetAcquired]}
{[%d 2 Test 1 0 Test1Step1][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 2 Test 1 0 Test1Step4][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 3, target T1\\n\"}"]}
{[%d 2 Test 1 0 ][Target{ID: "T1"} TargetReleased]}
{[%d 2 Test 2 0 ][Target{ID: "T2"} TargetAcquired]}
{[%d 2 Test 2 0 Test2Step1][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 1, target T2\\n\"}"]}
{[%d 2 Test 2 0 Test2Step2][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"\"}"]}
{[%d 2 Test 2 0 Test2Step3][Target{ID: "T2"} CmdStdout &"{\"Msg\":\"Test 2, Step 3, target T2\\n\"}"]}
{[%d 2 Test 2 0 ][Target{ID: "T2"} TargetReleased]}
`, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID),
			es,
		)
	}
	require.NoError(ts.T(), ts.stopServer(5*time.Second))
	// Shouldn't take more than 20 seconds. If it does, it most likely means state is not saved properly.
	require.Less(ts.T(), finish.Sub(start), 20*time.Second)
}

func (ts *E2ETestSuite) TestRetries() {
	type testDescriptorCustomisation struct {
		WaitPort int
	}

	ts.startServer()

	waitPort, err := getFreePort()
	require.NoError(ts.T(), err)

	testDescrTemplate, err := os.ReadFile("test-retry.yaml")
	require.NoError(ts.T(), err)

	templ, err := template.New("test-description").Delims("[[", "]]").Parse(string(testDescrTemplate))
	require.NoError(ts.T(), err)

	tmpFile, err := os.CreateTemp("", "")
	require.NoError(ts.T(), err)
	defer func() {
		assert.NoError(ts.T(), tmpFile.Close())
		assert.NoError(ts.T(), os.Remove(tmpFile.Name()))
	}()
	require.NoError(ts.T(), templ.Execute(tmpFile, testDescriptorCustomisation{WaitPort: waitPort}))

	jobID := ts.startJob(tmpFile.Name())

	<-time.After(5 * time.Second)
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", waitPort))
	require.NoError(ts.T(), err)
	defer func() {
		if err := listener.Close(); err != nil {
			assert.NoError(ts.T(), err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	{ // Wait for the job to finish
		var resp api.StatusResponse
		for i := 1; i < 20; i++ {
			time.Sleep(1 * time.Second)
			stdout, err := ts.runClient(&resp, "status", fmt.Sprintf("%d", jobID))
			require.NoError(ts.T(), err)
			require.Nil(ts.T(), resp.Err, "error: %s", resp.Err)
			logging.Infof(ctx, "Job %d state %s", jobID, resp.Data.Status.State)
			if resp.Data.Status.State == string(job.EventJobCompleted) {
				logging.Debugf(ctx, "Job %d status: %s", jobID, stdout)
				break
			}
		}
		require.Equal(ts.T(), string(job.EventJobCompleted), resp.Data.Status.State)
	}

	{ // Verify step output.
		es := testsCommon.GetJobEventsAsString(ctx, ts.st, jobID, []event.Name{
			cmd.EventCmdStdout, target.EventTargetAcquired, target.EventTargetReleased,
			target.EventTargetOut, target.EventTargetErr})
		logging.Debugf(ctx, "%s", es)
		require.Equal(ts.T(),
			fmt.Sprintf(`
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetAcquired]}
{[%d 1 Test 1 0 Step1][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 1 Test 1 0 Step1][Target{ID: "T1"} TargetOut]}
{[%d 1 Test 1 0 Step2][Target{ID: "T1"} TargetErr &"{\"Error\":\"context deadline exceeded\"}"]}
{[%d 1 Test 1 0 ][Target{ID: "T1"} TargetReleased]}
{[%d 1 Test 1 1 ][Target{ID: "T1"} TargetAcquired]}
{[%d 1 Test 1 1 Step1][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 1 Test 1 1 Step1][Target{ID: "T1"} TargetOut]}
{[%d 1 Test 1 1 Step2][Target{ID: "T1"} TargetOut]}
{[%d 1 Test 1 1 Step3][Target{ID: "T1"} CmdStdout &"{\"Msg\":\"Test 1, Step 1, target T1\\n\"}"]}
{[%d 1 Test 1 1 Step3][Target{ID: "T1"} TargetOut]}
{[%d 1 Test 1 1 ][Target{ID: "T1"} TargetReleased]}
`, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID, jobID),
			es,
		)
	}

	require.NoError(ts.T(), ts.stopServer(5*time.Second))
}

func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, fmt.Errorf("failed to start listening TCP port: '%v'", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func TestE2E(t *testing.T) {
	suite.Run(t, &E2ETestSuite{serverPort: 8888})
}
