// Copyright (c) Meta, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/linuxboot/contest/pkg/target"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func ptr[K any](v K) *K {
	return &v
}

type mockVars struct {
	mock.Mock
}

func (m *mockVars) Get(targetID string, stepLabel, name string, out interface{}) error {
	args := m.Called(targetID, stepLabel, name, out)
	return args.Error(0)
}

func TestParamExpander(t *testing.T) {
	target := &target.Target{
		ID:   "42",
		FQDN: "42.contest.com",
	}

	t.Run("simple", func(t *testing.T) {
		vars := &mockVars{}
		vars.On("Get", target.ID, "step", "out", mock.Anything).Run(func(args mock.Arguments) {
			outval := reflect.ValueOf(args.Get(3))
			if outval.Kind() != reflect.Ptr {
				panic("expected output to be a pointer type")
			}
			reflect.Indirect(outval).SetString("stepout")
		}).Return(nil)

		type testObj struct {
			ID         string
			FQDN       string
			Derivative string
		}

		input := &testObj{
			ID:         "{{.ID}}",
			FQDN:       "{{ToUpper .FQDN}}",
			Derivative: `{{.ID}}/{{StringVar "step.out"}}`,
		}

		e := NewParamExpander(target, vars)

		var out *testObj
		err := e.ExpandObject(input, &out)
		require.NoError(t, err)

		require.Equal(t, target.ID, out.ID)
		require.Equal(t, strings.ToUpper(target.FQDN), out.FQDN)
		require.Equal(t, fmt.Sprintf("%v/stepout", target.ID), out.Derivative)
		vars.AssertNumberOfCalls(t, "Get", 1)
	})

	t.Run("full", func(t *testing.T) {
		type subObj struct {
			FQDN  string
			Array [1]string
			Slice []string
		}

		type testObj struct {
			ID     string
			Sub    subObj
			IDPtr  *string
			SubPtr *subObj
		}

		input := &testObj{
			ID: "{{.ID}}",
			Sub: subObj{
				FQDN:  "{{.FQDN}}",
				Array: [1]string{"{{.ID}}"},
				Slice: []string{"{{.ID}}", "{{.ID}}"},
			},
			IDPtr: ptr("{{.ID}}"),
			SubPtr: &subObj{
				FQDN:  "{{.FQDN}}",
				Array: [1]string{"{{.FQDN}}"},
				Slice: []string{},
			},
		}

		e := NewParamExpander(target, &mockVars{})

		var out *testObj
		err := e.ExpandObject(input, &out)
		require.NoError(t, err)

		require.Equal(t, target.ID, out.ID)
		require.Equal(t, target.FQDN, out.Sub.FQDN)
		require.Equal(t, target.ID, out.Sub.Array[0])
		require.Len(t, out.Sub.Slice, 2)
		require.Equal(t, target.ID, out.Sub.Slice[0])

		require.NotNil(t, out.IDPtr)
		require.Equal(t, target.ID, *out.IDPtr)

		require.NotNil(t, out.SubPtr)
		require.Equal(t, target.FQDN, out.SubPtr.FQDN)
		require.Equal(t, target.FQDN, out.SubPtr.Array[0])
		require.Len(t, out.SubPtr.Slice, 0)
	})

	t.Run("untypedJSON", func(t *testing.T) {
		input := []byte(`{
			"Map": {
				"id": "{{.ID}}",
				"fqdn": "{{.FQDN}}"
			},
			"Array": [
				"{{.ID}}",
				"{{.ID}}"
			]}
		`)
		var inputObj any
		err := json.Unmarshal(input, &inputObj)
		require.NoError(t, err)

		e := NewParamExpander(target, &mockVars{})

		out := reflect.New(reflect.TypeOf(inputObj))
		err = e.ExpandObject(inputObj, out.Interface())
		require.NoError(t, err)

		// reconstruct into typed
		expanded, err := json.Marshal(out.Interface())
		require.NoError(t, err)

		var outObj struct {
			Map   map[string]string
			Array []string
		}

		err = json.Unmarshal(expanded, &outObj)
		require.NoError(t, err)

		require.Contains(t, outObj.Map, "id")
		require.Equal(t, target.ID, outObj.Map["id"])
		require.Contains(t, outObj.Map, "fqdn")
		require.Equal(t, target.FQDN, outObj.Map["fqdn"])

		require.Len(t, outObj.Array, 2)
		require.Equal(t, target.ID, outObj.Array[0])
		require.Equal(t, target.ID, outObj.Array[1])
	})

	t.Run("nulls", func(t *testing.T) {
		type testObj struct {
			Str  *string
			Intf any
		}

		// both fields null
		input := &testObj{}

		e := NewParamExpander(target, &mockVars{})

		var out *testObj
		err := e.ExpandObject(input, &out)
		require.NoError(t, err)

		require.Nil(t, out.Str)
		require.Nil(t, out.Intf)
	})
}
