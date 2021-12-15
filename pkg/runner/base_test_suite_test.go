package runner

import (
	"encoding/json"

	"github.com/benbjohnson/clock"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/targetlocker/inmemory"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type BaseTestSuite struct {
	suite.Suite

	pluginRegistry  *pluginregistry.PluginRegistry
	internalStorage *MemoryStorageEngine
}

func (s *BaseTestSuite) SetupTest() {
	storageEngine, err := NewMemoryStorageEngine()
	require.NoError(s.T(), err)
	s.internalStorage = storageEngine

	target.SetLocker(inmemory.New(clock.New()))

	s.pluginRegistry = pluginregistry.NewPluginRegistry(xcontext.Background())
}

func (s *BaseTestSuite) TearDownTest() {
	target.SetLocker(nil)
}

func (s *BaseTestSuite) RegisterStateFullStep(
	runFunction func(
		ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters,
		ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error),
	validateFunction func(ctx xcontext.Context, params test.TestStepParameters) error) error {

	return s.pluginRegistry.RegisterTestStep(stateFullStepName, func() test.TestStep {
		return &stateFullStep{
			runFunction:      runFunction,
			validateFunction: validateFunction,
		}
	}, nil)
}

func (s *BaseTestSuite) NewStep(label, name string, params test.TestStepParameters) test.TestStepBundle {
	td := test.TestStepDescriptor{
		Name:       name,
		Label:      label,
		Parameters: params,
	}
	sb, err := s.pluginRegistry.NewTestStepBundle(ctx, td)
	require.NoError(s.T(), err)
	return *sb
}
