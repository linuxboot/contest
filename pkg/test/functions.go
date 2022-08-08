// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package test

import (
	"fmt"
	"strings"
	"sync"
)

// funcMap is a map between function name and its implementation.
var funcMap = map[string]interface{}{
	// some common pre-sets
	"ToUpper": strings.ToUpper,
	"ToLower": strings.ToLower,
	"Title":   strings.Title,
}
var funcMapMutex sync.Mutex

// getFuncMap returns a copy of funcMap that can be passed to Template.Funcs.
// The map is copied so it can be passed safely even if the original is modified.
func getFuncMap() map[string]interface{} {
	mapCopy := make(map[string]interface{}, len(funcMap))
	funcMapMutex.Lock()
	defer funcMapMutex.Unlock()
	for k, v := range funcMap {
		mapCopy[k] = v
	}
	return mapCopy
}

func parseLabelDotVariable(labelDotVariable string) (string, string, error) {
	parts := strings.SplitN(labelDotVariable, ".", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("not 'label.variable' format of '%s'", labelDotVariable)
	}
	stepLabel, varname := parts[0], parts[1]
	if err := CheckIdentifier(stepLabel); err != nil {
		return "", "", fmt.Errorf("invalid step label: '%s': %w", stepLabel, err)
	}
	if err := CheckIdentifier(varname); err != nil {
		return "", "", fmt.Errorf("invalid variable name: '%s': %w", varname, err)
	}
	return stepLabel, varname, nil
}

func registerStepVariableAccessor(fm map[string]interface{}, tgtID string, vars StepsVariablesReader) {
	fm["StringVar"] = func(labelDotVariable string) (string, error) {
		stepLabel, varName, err := parseLabelDotVariable(labelDotVariable)
		if err != nil {
			return "", err
		}

		var s string
		if err := vars.Get(tgtID, stepLabel, varName, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	fm["IntVar"] = func(labelDotVariable string) (int, error) {
		stepLabel, varName, err := parseLabelDotVariable(labelDotVariable)
		if err != nil {
			return 0, err
		}
		var i int
		if err := vars.Get(tgtID, stepLabel, varName, &i); err != nil {
			return 0, err
		}
		return i, nil
	}
}

// RegisterFunction registers a template function suitable for text/template.
// It can be either a func(string) string or a func(string) (string, error),
// hence it's passed as an empty interface.
func RegisterFunction(name string, fn interface{}) error {
	funcMapMutex.Lock()
	defer funcMapMutex.Unlock()
	if funcMap == nil {
		funcMap = make(map[string]interface{})
	}
	if _, ok := funcMap[name]; ok {
		return fmt.Errorf("function '%s' is already registered", name)
	}
	funcMap[name] = fn
	return nil
}

// UnregisterFunction unregisters a previously registered function
func UnregisterFunction(name string) error {
	funcMapMutex.Lock()
	defer funcMapMutex.Unlock()
	ok := false
	if funcMap != nil {
		_, ok = funcMap[name]
		if ok {
			delete(funcMap, name)
		}
	}
	if !ok {
		return fmt.Errorf("function '%s' is not registered", name)
	}
	return nil
}
