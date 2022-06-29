package runner

import (
	"encoding/json"

	"github.com/benbjohnson/clock"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/storage/memory"
	"github.com/linuxboot/contest/plugins/targetlocker/inmemory"
	"github.com/linuxboot/contest/tests/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MemoryStorageEngine struct {
	Storage            storage.ResettableStorage
	StorageEngineVault *storage.SimpleEngineVault
}

func NewMemoryStorageEngine() (*MemoryStorageEngine, error) {
	ms, err := memory.New()
	if err != nil {
		return nil, err
	}

	storageEngineVault := storage.NewSimpleEngineVault()
	if err := storageEngineVault.StoreEngine(ms, storage.SyncEngine); err != nil {
		return nil, err
	}

	return &MemoryStorageEngine{
		Storage:            ms,
		StorageEngineVault: storageEngineVault,
	}, nil
}

func (mse *MemoryStorageEngine) GetStepEvents(ctx xcontext.Context, testName string, stepLabel string) string {
	return common.GetTestEventsAsString(ctx, mse.Storage, testName, nil, &stepLabel)
}

func (mse *MemoryStorageEngine) GetTargetEvents(ctx xcontext.Context, testName string, targetID string) string {
	return common.GetTestEventsAsString(ctx, mse.Storage, testName, &targetID, nil)
}

func (mse *MemoryStorageEngine) GetTestEvents(ctx xcontext.Context, testName string) string {
	return common.GetTestEventsAsString(ctx, mse.Storage, testName, nil, nil)
}

type BaseTestSuite struct {
	suite.Suite

	PluginRegistry *pluginregistry.PluginRegistry
	MemoryStorage  *MemoryStorageEngine
}

func (s *BaseTestSuite) SetupTest() {
	storageEngine, err := NewMemoryStorageEngine()
	require.NoError(s.T(), err)
	s.MemoryStorage = storageEngine

	target.SetLocker(inmemory.New(clock.New()))

	s.PluginRegistry = pluginregistry.NewPluginRegistry(xcontext.Background())
}

func (s *BaseTestSuite) TearDownTest() {
	target.SetLocker(nil)
}

func (s *BaseTestSuite) RegisterStateFullStep(
	runFunction func(ctx xcontext.Context, ch test.TestStepChannels, ev testevent.Emitter,
		stepsVars test.StepsVariables, params test.TestStepParameters,
		resumeState json.RawMessage) (json.RawMessage, error),
	validateFunction func(ctx xcontext.Context, params test.TestStepParameters) error) error {

	return s.PluginRegistry.RegisterTestStep(stateFullStepName, func() test.TestStep {
		return &stateFullStep{
			runFunction:      runFunction,
			validateFunction: validateFunction,
		}
	}, nil)
}

func (s *BaseTestSuite) NewStep(
	ctx xcontext.Context,
	label, name string,
	params test.TestStepParameters,
) test.TestStepBundle {
	td := test.TestStepDescriptor{
		Name:       name,
		Label:      label,
		Parameters: params,
	}
	sb, err := s.PluginRegistry.NewTestStepBundle(ctx, td)
	require.NoError(s.T(), err)
	return *sb
}
