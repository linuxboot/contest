// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"errors"
	"strings"
	"testing"

	"github.com/linuxboot/contest/pkg/target"
	"github.com/stretchr/testify/require"
)

func TestParameterExpand(t *testing.T) {
	validExprs := [][4]string{
		// expression, target FQDN, target ID, expected result
		{"{{ ToLower .FQDN }}", "www.slackware.IT", "1234", "www.slackware.it"},
		{"{{ .FQDN }}", "www.slackware.IT", "2345", "www.slackware.IT"},
		{"{{ .FQDN }}", "www.slackware.it", "3456", "www.slackware.it"},
		{"fqdn={{ .FQDN }}, id={{ .ID }}", "blah", "12345", "fqdn=blah, id=12345"},
	}
	for _, x := range validExprs {
		p := NewParam(x[0])
		res, err := p.Expand(&target.Target{FQDN: x[1], ID: x[2]})
		require.NoError(t, err, x[0])
		require.Equal(t, x[3], res, x[0])
	}
}

//nolint:staticcheck
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
		{"{{ CustomFunc .FQDN }}", "slackware.it", "1234", "Slackware.It"},
		{"{{ CustomFunc .ID }}", "slackware.it", "a1234a", "A1234a"},
	}
	for _, x := range validExprs {
		p := NewParam(x[0])
		res, err := p.Expand(&target.Target{FQDN: x[1], ID: x[2]})
		require.NoError(t, err, x[0])
		require.Equal(t, x[3], res, x[0])
	}
	require.NoError(t, UnregisterFunction("CustomFunc"))
	require.Error(t, UnregisterFunction("NoSuchFunction"))
}
