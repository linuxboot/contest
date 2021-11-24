// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build integration
// +build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/runner"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	mu        sync.Mutex
	currentID int
)

func nextID() int {
	mu.Lock()
	defer mu.Unlock()

	currentID++
	return currentID
}

func runExecPlugin(t *testing.T, ctx xcontext.Context, jsonParams string) error {
	jobID := types.JobID(nextID())
	runID := types.RunID(1)

	ts, err := pluginRegistry.NewTestStep("exec")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	params["bag"] = []test.Param{
		*test.NewParam(jsonParams),
	}

	testSteps := []test.TestStepBundle{
		{TestStep: ts, Parameters: params},
	}

	errCh := make(chan error, 1)

	go func() {
		defer func() {
			if e := recover(); e != nil {
				errCh <- e.(error)
			}
			errCh <- nil
		}()

		tr := runner.NewTestRunner()
		eventsFactory := runner.NewTestStepEventsEmitterFactory(storageEngineVault, jobID, runID, "", 0)
		_, _, err := tr.Run(ctx, &test.Test{TestStepsBundles: testSteps}, targets, eventsFactory, nil)
		if err != nil {
			panic(err)
		}

		ev := storage.NewTestEventFetcher(storageEngineVault)
		events, err := ev.Fetch(ctx, testevent.QueryJobID(jobID), testevent.QueryEventName(target.EventTargetErr))
		if err != nil {
			panic(err)
		}

		if events != nil {
			var payload struct {
				Error string
			}

			if err := json.Unmarshal(*events[0].Data.Payload, &payload); err != nil {
				panic(err)
			}
			panic(fmt.Errorf("step error: %s", payload.Error))
		}
	}()

	select {
	case err := <-errCh:
		return err

	case <-time.After(successTimeout):
		t.Errorf("test should return within timeout: %+v", successTimeout)
	}

	return nil
}

func TestExecPluginLocalSimple(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"echo 42"
			]
		},
		"transport": {
			"proto": "local"
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.NoError(t, err)
}

func TestExecPluginSSHSimple(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"echo 42"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa"
			}
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.NoError(t, err)
}

func TestExecPluginSSHAsyncSimple(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"echo 42"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa",
				"send_binary": false,
				"async": {
					"agent": "/go/src/github.com/linuxboot/contest/exec_agent",
					"time_quota": "20s"
				}
			}
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.NoError(t, err)
}

func TestExecPluginSSHAsyncSendBinary(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"echo 42"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa",
				"send_binary": true,
				"async": {
					"agent": "/go/src/github.com/linuxboot/contest/exec_agent",
					"time_quota": "20s"
				}
			}
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.NoError(t, err)
}

func TestExecPluginLocalTimeout(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sleep",
			"args": [
				"20"
			]
		},
		"transport": {
			"proto": "local"
		},
		"constraints": {
			"time_quota": "1s"
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-zero")
}
func TestExecPluginSSHTimeout(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sleep",
			"args": [
				"20"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa"
			}
		},
		"constraints": {
			"time_quota": "1s"
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)

	// depending on the ssh server impl, this error can be
	// 1. process was killed and has non-zero exit code
	// 2. the context deadline exceeded and process was abandoned
	if !strings.Contains(err.Error(), "non-zero") && !strings.Contains(err.Error(), "deadline exceeded") {
		require.Failf(t, "error '%s' was not killed process or timeout related", err.Error())
	}
}

func TestExecPluginSSHAsyncTimeout(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sleep",
			"args": [
				"20"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa",
				"send_binary": false,
				"async": {
					"agent": "/go/src/github.com/linuxboot/contest/exec_agent",
					"time_quota": "1s"
				}
			}
		}
	}`

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeded time quota")
}

func TestExecPluginLocalCancel(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sleep",
			"args": [
				"20"
			]
		},
		"transport": {
			"proto": "local"
		}
	}`

	ctx, cancel := xcontext.WithTimeout(ctx, time.Second)
	defer cancel()

	err := runExecPlugin(t, ctx, jsonParams)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExecPluginSSHCancel(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sleep",
			"args": [
				"20"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa"
			}
		}
	}`

	ctx, cancel := xcontext.WithTimeout(ctx, time.Second)
	defer cancel()

	err := runExecPlugin(t, ctx, jsonParams)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExecPluginSSHAsyncCancel(t *testing.T) {
	jsonParams := `
	{
		"bin": {
			"path": "/bin/sleep",
			"args": [
				"20"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa",
				"send_binary": true,
				"async": {
					"agent": "/go/src/github.com/linuxboot/contest/exec_agent",
					"time_quota": "25s"
				}
			}
		}
	}`

	ctx, cancel := xcontext.WithTimeout(ctx, time.Second)
	defer cancel()

	err := runExecPlugin(t, ctx, jsonParams)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExecPluginLocalExitMap(t *testing.T) {
	customError := "custom error 42"
	jsonParams := fmt.Sprintf(`
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"exit 42"
			]
		},
		"transport": {
			"proto": "local"
		},
		"exitcode_map": {
			"42": "%s"
		}
	}`, customError)

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)
	require.Contains(t, err.Error(), customError)
}

func TestExecPluginLocalExitMapWithOCP(t *testing.T) {
	customError := "custom error 42"
	jsonParams := fmt.Sprintf(`
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"exit 42"
			]
		},
		"transport": {
			"proto": "local"
		},
		"ocp_output": true,
		"exitcode_map": {
			"42": "%s"
		}
	}`, customError)

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)
	require.Contains(t, err.Error(), customError)
}

func TestExecPluginSSHExitMap(t *testing.T) {
	customError := "custom error 42"
	jsonParams := fmt.Sprintf(`
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"exit 42"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa"
			}
		},
		"exitcode_map": {
			"42": "%s"
		}
	}`, customError)

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)
	require.Contains(t, err.Error(), customError)
}

func TestExecPluginSSHAsyncExitMap(t *testing.T) {
	customError := "custom error 42"
	jsonParams := fmt.Sprintf(`
	{
		"bin": {
			"path": "/bin/sh",
			"args": [
				"-c",
				"exit 42"
			]
		},
		"transport": {
			"proto": "ssh",
			"options": {
				"host": "localhost",
				"user": "root",
				"identity_file": "/root/.ssh/id_rsa",
				"send_binary": false,
				"async": {
					"agent": "/go/src/github.com/linuxboot/contest/exec_agent",
					"time_quota": "20s"
				}
			}
		},
		"exitcode_map": {
			"42": "%s",
			"43": "doesnt exist"
		}
	}`, customError)

	err := runExecPlugin(t, ctx, jsonParams)
	require.Error(t, err)
	require.Contains(t, err.Error(), customError)
}
