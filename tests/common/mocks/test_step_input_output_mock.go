package mocks

import (
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"sync"
)

type TestStepInputOutputMock struct {
	mu           sync.Mutex
	inputTargets []target.Target
	targetsIdx   int

	reportedTargets map[string]error
}

func NewTestStepInputOutputMock(inputTargets []target.Target) *TestStepInputOutputMock {
	return &TestStepInputOutputMock{
		inputTargets:    inputTargets,
		reportedTargets: make(map[string]error),
	}
}

func (ioMock *TestStepInputOutputMock) Get(ctx xcontext.Context) (*target.Target, error) {
	ioMock.mu.Lock()
	defer ioMock.mu.Unlock()

	if ioMock.targetsIdx >= len(ioMock.inputTargets) {
		return nil, nil
	}
	ioMock.targetsIdx++
	return &ioMock.inputTargets[ioMock.targetsIdx-1], nil
}

func (ioMock *TestStepInputOutputMock) Report(ctx xcontext.Context, tgt target.Target, err error) error {
	ioMock.mu.Lock()
	defer ioMock.mu.Unlock()

	ioMock.reportedTargets[tgt.ID] = err
	return nil
}

func (ioMock *TestStepInputOutputMock) GetReportedTargets() map[string]error {
	ioMock.mu.Lock()
	defer ioMock.mu.Unlock()

	result := make(map[string]error)
	for tgtID, err := range ioMock.reportedTargets {
		result[tgtID] = err
	}
	return result
}

var _ test.TestStepInputOutput = (*TestStepInputOutputMock)(nil)
