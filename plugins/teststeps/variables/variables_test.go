package variables

import (
	"encoding/json"
	"fmt"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/plugins/storage/memory"
	"sync"
	"testing"

	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/stretchr/testify/require"
)

func TestCreation(t *testing.T) {
	obj := New()
	require.NotNil(t, obj)
	require.Equal(t, Name, obj.Name())
}

func TestValidateParameters(t *testing.T) {
	obj := New()
	require.NotNil(t, obj)

	require.NoError(t, obj.ValidateParameters(xcontext.Background(), nil))
	require.NoError(t, obj.ValidateParameters(xcontext.Background(), test.TestStepParameters{
		"var1": []test.Param{
			{
				json.RawMessage("123"),
			},
		},
	}))
	// invalid variable name
	require.Error(t, obj.ValidateParameters(xcontext.Background(), test.TestStepParameters{
		"var var": []test.Param{
			{
				json.RawMessage("123"),
			},
		},
	}))
	// invalid value
	require.Error(t, obj.ValidateParameters(xcontext.Background(), test.TestStepParameters{
		"var1": []test.Param{
			{
				json.RawMessage("ALALALALA[}"),
			},
		},
	}))
}

func TestVariablesEmission(t *testing.T) {
	ctx, cancel := xcontext.WithCancel(xcontext.Background())
	defer cancel()

	obj := New()
	require.NotNil(t, obj)

	in := make(chan *target.Target, 1)
	out := make(chan test.TestStepResult, 1)

	m, err := memory.New()
	if err != nil {
		t.Fatalf("could not initialize memory storage: '%v'", err)
	}
	storageEngineVault := storage.NewSimpleEngineVault()
	if err := storageEngineVault.StoreEngine(m, storage.SyncEngine); err != nil {
		t.Fatalf("Failed to set memory storage: '%v'", err)
	}
	ev := storage.NewTestEventEmitterFetcher(storageEngineVault, testevent.Header{
		JobID:         12345,
		TestName:      "variables_tests",
		TestStepLabel: "variables",
	})

	svm := newStepsVariablesMock()

	tgt := target.Target{ID: "id1"}
	in <- &tgt
	close(in)

	state, err := obj.Run(ctx, test.TestStepChannels{In: in, Out: out}, ev, svm, test.TestStepParameters{
		"str_variable": []test.Param{
			{
				json.RawMessage("\"dummy\""),
			},
		},
		"int_variable": []test.Param{
			{
				json.RawMessage("123"),
			},
		},
		"complex_variable": []test.Param{
			{
				json.RawMessage("{\"name\":\"value\"}"),
			},
		},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, state)

	stepResult := <-out
	require.Equal(t, tgt, *stepResult.Target)
	require.NoError(t, stepResult.Err)

	var strVar string
	require.NoError(t, svm.Get(tgt.ID, "str_variable", &strVar))
	require.Equal(t, "dummy", strVar)

	var intVar int
	require.NoError(t, svm.Get(tgt.ID, "int_variable", &intVar))
	require.Equal(t, 123, intVar)

	var complexVar dummyStruct
	require.NoError(t, svm.Get(tgt.ID, "complex_variable", &complexVar))
	require.Equal(t, dummyStruct{Name: "value"}, complexVar)
}

type dummyStruct struct {
	Name string `json:"name"`
}

type stepsVariablesMock struct {
	mu        sync.Mutex
	variables map[string]map[string]json.RawMessage
}

func newStepsVariablesMock() *stepsVariablesMock {
	return &stepsVariablesMock{
		variables: make(map[string]map[string]json.RawMessage),
	}
}

func (svm *stepsVariablesMock) AddRaw(tgtID string, name string, b json.RawMessage) error {
	svm.mu.Lock()
	defer svm.mu.Unlock()

	targetVars := svm.variables[tgtID]
	if targetVars == nil {
		targetVars = make(map[string]json.RawMessage)
		svm.variables[tgtID] = targetVars
	}
	targetVars[name] = b
	return nil
}

func (svm *stepsVariablesMock) Add(tgtID string, name string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return svm.AddRaw(tgtID, name, b)
}

func (svm *stepsVariablesMock) Get(tgtID string, name string, out interface{}) error {
	svm.mu.Lock()
	defer svm.mu.Unlock()

	targetVars := svm.variables[tgtID]
	if targetVars == nil {
		return fmt.Errorf("no target: %s", tgtID)
	}
	b, found := targetVars[name]
	if !found {
		return fmt.Errorf("no variable: %s", name)
	}
	return json.Unmarshal(b, out)
}
