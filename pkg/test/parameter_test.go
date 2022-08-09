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

func TestExpandWithNoTargetProvided(t *testing.T) {
	p := NewParam("dummy")

	res, err := p.Expand(nil, nil)
	require.NoError(t, err)
	require.Equal(t, "dummy", res)

	res, err = p.Expand(nil, newStepsVariablesMock())
	require.NoError(t, err)
	require.Equal(t, "dummy", res)
}

func TestStepVariablesExpand(t *testing.T) {
	p := NewParam("{{ StringVar \"step1.string_var\" }}: {{ IntVar \"step1.int_var\" }}")
	svm := newStepsVariablesMock()

	tgt := target.Target{ID: "1"}
	require.NoError(t, svm.add(tgt.ID, "step1", "string_var", "Hello"))
	require.NoError(t, svm.add(tgt.ID, "step1", "int_var", 42))

	res, err := p.Expand(&tgt, svm)
	require.NoError(t, err)
	require.Equal(t, "Hello: 42", res)
}

func TestInvalidStepVariablesExpand(t *testing.T) {
	t.Run("no_dot", func(t *testing.T) {
		p := NewParam("{{ StringVar \"step1string_var\" }}")
		_, err := p.Expand(&target.Target{ID: "1"}, newStepsVariablesMock())
		require.Error(t, err)
	})

	t.Run("just_variable_name", func(t *testing.T) {
		p := NewParam("{{ StringVar \"string_var\" }}")

		svm := newStepsVariablesMock()
		tgt := target.Target{ID: "1"}
		require.NoError(t, svm.add(tgt.ID, "step1", "string_var", "Hello"))

		_, err := p.Expand(&tgt, svm)
		require.Error(t, err)
	})

	t.Run("invalid_variable_name", func(t *testing.T) {
		p := NewParam("{{ StringVar \"step1.22string_var\" }}")

		svm := newStepsVariablesMock()
		tgt := target.Target{ID: "1"}
		// we can add invalid values to our mock
		require.NoError(t, svm.add(tgt.ID, "step1", "22string_var", "Hello"))

		_, err := p.Expand(&tgt, svm)
		require.Error(t, err)
	})
}

type stepsVariablesMock struct {
	variables map[string]map[string]json.RawMessage
}

func newStepsVariablesMock() *stepsVariablesMock {
	return &stepsVariablesMock{
		variables: make(map[string]map[string]json.RawMessage),
	}
}

func (svm *stepsVariablesMock) add(tgtID string, label, name string, in interface{}) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}

	targetVars := svm.variables[tgtID]
	if targetVars == nil {
		targetVars = make(map[string]json.RawMessage)
		svm.variables[tgtID] = targetVars
	}
	targetVars[label+"."+name] = b
	return nil
}

func (svm *stepsVariablesMock) Get(tgtID string, stepLabel, name string, out interface{}) error {
	targetVars := svm.variables[tgtID]
	if targetVars == nil {
		return fmt.Errorf("no target: %s", tgtID)
	}
	b, found := targetVars[stepLabel+"."+name]
	if !found {
		return fmt.Errorf("no variable %s %s", stepLabel, name)
	}
	return json.Unmarshal(b, out)
}
