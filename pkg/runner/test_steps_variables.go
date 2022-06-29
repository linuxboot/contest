package runner

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/linuxboot/contest/pkg/test"
)

type testStepsVariables struct {
	existingSteps map[string]struct{}

	lock                   sync.Mutex
	stepsVariablesByTarget map[string]map[string]stepVariables
}

func newTestStepsVariables(bundles []test.TestStepBundle) (*testStepsVariables, error) {
	result := &testStepsVariables{
		existingSteps:          make(map[string]struct{}),
		stepsVariablesByTarget: make(map[string]map[string]stepVariables),
	}
	for _, bs := range bundles {
		if len(bs.TestStepLabel) == 0 {
			continue
		}
		if _, found := result.existingSteps[bs.TestStepLabel]; found {
			return nil, fmt.Errorf("duplication of test step label: '%s'", bs.TestStepLabel)
		}
		result.existingSteps[bs.TestStepLabel] = struct{}{}
	}
	return result, nil
}

func (tsv *testStepsVariables) initTargetStepsVariables(tgtID string, stepsVars map[string]stepVariables) error {
	if _, found := tsv.stepsVariablesByTarget[tgtID]; found {
		return fmt.Errorf("duplication of target ID: '%s'", tgtID)
	}
	if stepsVars == nil {
		tsv.stepsVariablesByTarget[tgtID] = make(map[string]stepVariables)
	} else {
		tsv.stepsVariablesByTarget[tgtID] = stepsVars
	}
	return nil
}

func (tsv *testStepsVariables) getTargetStepsVariables(tgtID string) (map[string]stepVariables, error) {
	tsv.lock.Lock()
	defer tsv.lock.Unlock()

	vars, found := tsv.stepsVariablesByTarget[tgtID]
	if !found {
		return nil, fmt.Errorf("target ID: '%s' is not found", tgtID)
	}

	// make a copy in case of buggy steps that can still be using it
	result := make(map[string]stepVariables)
	for name, value := range vars {
		result[name] = value
	}
	return result, nil
}

func (tsv *testStepsVariables) Add(tgtID string, stepLabel string, name string, value json.RawMessage) error {
	if err := tsv.checkInput(tgtID, stepLabel, name); err != nil {
		return err
	}

	// tsv.stepsVariablesByTarget is a readonly map, though its values may change
	targetSteps := tsv.stepsVariablesByTarget[tgtID]
	tsv.lock.Lock()
	defer tsv.lock.Unlock()

	stepVars, found := targetSteps[stepLabel]
	if !found {
		stepVars = make(stepVariables)
		targetSteps[stepLabel] = stepVars
	}
	stepVars[name] = value
	return nil
}

func (tsv *testStepsVariables) Get(tgtID string, stepLabel string, name string) (json.RawMessage, error) {
	if err := tsv.checkInput(tgtID, stepLabel, name); err != nil {
		return nil, err
	}

	targetSteps := tsv.stepsVariablesByTarget[tgtID]
	tsv.lock.Lock()
	defer tsv.lock.Unlock()

	stepVars, found := targetSteps[stepLabel]
	if !found {
		return nil, fmt.Errorf("step '%s' didn't emit any variables yet", stepLabel)
	}
	value, found := stepVars[name]
	if !found {
		return nil, fmt.Errorf("step '%s' didn't emit variable '%s' yet for target '%s'", stepLabel, name, tgtID)
	}
	return value, nil
}

func (tsv *testStepsVariables) checkInput(tgtID string, stepLabel string, name string) error {
	if len(stepLabel) == 0 {
		return fmt.Errorf("empty step name")
	}
	if len(tgtID) == 0 {
		return fmt.Errorf("empty target ID")
	}
	if len(name) == 0 {
		return fmt.Errorf("empty variable name")
	}
	return nil
}

type stepVariablesAccessor struct {
	stepLabel string
	tsv       *testStepsVariables
}

func newStepVariablesAccessor(stepLabel string, tsv *testStepsVariables) *stepVariablesAccessor {
	return &stepVariablesAccessor{
		stepLabel: stepLabel,
		tsv:       tsv,
	}
}

func (sva *stepVariablesAccessor) Add(tgtID string, name string, in interface{}) error {
	if len(sva.stepLabel) == 0 {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("failed to serialize variable: %v", in)
	}
	return sva.tsv.Add(tgtID, sva.stepLabel, name, b)
}

func (sva *stepVariablesAccessor) Get(tgtID string, stepLabel, name string, out interface{}) error {
	b, err := sva.tsv.Get(tgtID, stepLabel, name)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

var _ test.StepsVariables = (*stepVariablesAccessor)(nil)
