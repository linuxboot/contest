// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/linuxboot/contest/pkg/target"
	"github.com/stretchr/testify/require"
)

func TestParameterExpand(t *testing.T) {
	validExprs := [][4]string{
		// expression, target FQDN, target ID, expected result
		[4]string{"{{ ToLower .FQDN }}", "www.slackware.IT", "1234", "www.slackware.it"},
		[4]string{"{{ .FQDN }}", "www.slackware.IT", "2345", "www.slackware.IT"},
		[4]string{"{{ .FQDN }}", "www.slackware.it", "3456", "www.slackware.it"},
		[4]string{"fqdn={{ .FQDN }}, id={{ .ID }}", "blah", "12345", "fqdn=blah, id=12345"},
	}
	for _, x := range validExprs {
		p := NewParam(x[0])
		res, err := p.Expand(&target.Target{FQDN: x[1], ID: x[2]}, nil)
		require.NoError(t, err, x[0])
		require.Equal(t, x[3], res, x[0])
	}
}

func TestParameterExpandUserFunctions(t *testing.T) {
	require.Error(t, UnregisterFunction("NoSuchFunction"))
	customFunc := func(a ...string) (string, error) {
		if len(a) == 0 {
			return "", errors.New("no params")
		}
		return strings.Title(a[0]), nil
	}
	require.NoError(t, RegisterFunction("CustomFunc", customFunc))
	validExprs := [][4]string{
		// expression, target FQDN, target ID, expected result
		[4]string{"{{ CustomFunc .FQDN }}", "slackware.it", "1234", "Slackware.It"},
		[4]string{"{{ CustomFunc .ID }}", "slackware.it", "a1234a", "A1234a"},
	}
	for _, x := range validExprs {
		p := NewParam(x[0])
		res, err := p.Expand(&target.Target{FQDN: x[1], ID: x[2]}, nil)
		require.NoError(t, err, x[0])
		require.Equal(t, x[3], res, x[0])
	}
	require.NoError(t, UnregisterFunction("CustomFunc"))
	require.Error(t, UnregisterFunction("NoSuchFunction"))
}

func TestStepVariablesExpand(t *testing.T) {
	p := NewParam("{{ StringVar \"string_var\" }}: {{ IntVar \"int_var\" }}")
	svm := newStepsVariablesMock()

	tgt := target.Target{ID: "1"}
	require.NoError(t, svm.Add(tgt.ID, "string_var", "Hello"))
	require.NoError(t, svm.Add(tgt.ID, "int_var", 42))

	res, err := p.Expand(&tgt, svm)
	require.NoError(t, err)
	require.Equal(t, "Hello: 42", res)
}

type stepsVariablesMock struct {
	variables map[string]map[string]json.RawMessage
}

func newStepsVariablesMock() *stepsVariablesMock {
	return &stepsVariablesMock{
		variables: make(map[string]map[string]json.RawMessage),
	}
}

func (svm *stepsVariablesMock) Add(tgtID string, name string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	targetVars := svm.variables[tgtID]
	if targetVars == nil {
		targetVars = make(map[string]json.RawMessage)
		svm.variables[tgtID] = targetVars
	}
	targetVars[name] = b
	return nil
}

func (svm *stepsVariablesMock) Get(tgtID string, name string, out interface{}) error {
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
