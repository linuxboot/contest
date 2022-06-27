package jobmanager

import (
	"testing"

	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/reporters/noop"
	"github.com/linuxboot/contest/plugins/targetmanagers/targetlist"
	"github.com/linuxboot/contest/plugins/testfetchers/literal"
	"github.com/linuxboot/contest/plugins/teststeps/echo"
	"github.com/stretchr/testify/require"
)

func TestDisabledTestDescriptor(t *testing.T) {
	pr := pluginregistry.NewPluginRegistry(xcontext.Background())
	require.NoError(t, pr.RegisterTestStep(echo.Load()))
	require.NoError(t, pr.RegisterTargetManager(targetlist.Load()))
	require.NoError(t, pr.RegisterTestFetcher(literal.Load()))
	require.NoError(t, pr.RegisterReporter(noop.Load()))

	testFetcherParams1 := `{
	    "TestName": "TestDisabled",
		"Steps": [
			{
				"name": "echo",
				"label": "echotext",
				"parameters": {
					"text": ["Some text1"]
				}
			}
		]
	}`
	testFetcherParams2 := `{
	    "TestName": "TestEnabled",
		"Steps": [
			{
				"name": "echo",
				"label": "echotext",
				"parameters": {
					"text": ["Some text1"]
				}
			}
		]
	}`

	targetManagerAcquireParameters := `{
		"Targets": [
			{
				"ID": "id1",
				"FQDN": "some-host.fna1.dummy-facebook.com"
			}
		]
	}`

	testDescriptors := []*test.TestDescriptor{
		{
			Disabled:                       true,
			TargetManagerName:              "targetList",
			TargetManagerAcquireParameters: []byte(targetManagerAcquireParameters),
			TargetManagerReleaseParameters: []byte("{}"),
			TestFetcherName:                "literal",
			TestFetcherFetchParameters:     []byte(testFetcherParams1),
		},
		{
			TargetManagerName:              "targetList",
			TargetManagerAcquireParameters: []byte(targetManagerAcquireParameters),
			TargetManagerReleaseParameters: []byte("{}"),
			TestFetcherName:                "literal",
			TestFetcherFetchParameters:     []byte(testFetcherParams2),
		},
	}
	jd := job.Descriptor{
		TestDescriptors: testDescriptors,
		JobName:         "Test",
		Reporting: job.Reporting{
			RunReporters: []job.ReporterConfig{
				{Name: "noop"},
			},
		},
	}

	result, err := NewJobFromDescriptor(xcontext.Background(), pr, &jd)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Tests, 1)
	require.Equal(t, result.Tests[0].Name, "TestEnabled")
}

func TestNewJobNoTests(t *testing.T) {
	pr := pluginregistry.NewPluginRegistry(xcontext.Background())
	// require.NoError(t, pr.RegisterTestStep(echo.Load()))
	require.NoError(t, pr.RegisterTargetManager(targetlist.Load()))
	require.NoError(t, pr.RegisterTestFetcher(literal.Load()))
	require.NoError(t, pr.RegisterReporter(noop.Load()))

	testDescriptors := []*test.TestDescriptor{}
	jd := job.Descriptor{
		TestDescriptors: testDescriptors,
		JobName:         "Test",
		Reporting: job.Reporting{
			RunReporters: []job.ReporterConfig{
				{Name: "noop"},
			},
		},
	}

	_, err := NewJobFromDescriptor(xcontext.Background(), pr, &jd)
	require.Error(t, err)
}

func TestVariablesReferToExistingStepLabels(t *testing.T) {
	pr := pluginregistry.NewPluginRegistry(xcontext.Background())
	require.NoError(t, pr.RegisterTestStep(echo.Load()))
	require.NoError(t, pr.RegisterTargetManager(targetlist.Load()))
	require.NoError(t, pr.RegisterTestFetcher(literal.Load()))
	require.NoError(t, pr.RegisterReporter(noop.Load()))

	targetManagerAcquireParameters := `{
		"Targets": [
			{
				"ID": "id1",
				"FQDN": "some-host.fna1.dummy-facebook.com"
			}
		]
	}`

	t.Run("correct_label", func(t *testing.T) {
		testParams := `{
			"TestName": "TestVariables",
			"Steps": [
				{
					"name": "echo",
					"label": "echo1",
					"parameters": {
						"text": ["Some text1"]
					}
				},
				{
					"name": "echo",
					"label": "echo2",
					"parameters": {
						"text": ["Some text1"]
					},
					"variablesmapping": {
						"output_var": "echo1.message"
					}
				}
			]
		}`

		testDescriptors := []*test.TestDescriptor{
			{
				TargetManagerName:              "targetList",
				TargetManagerAcquireParameters: []byte(targetManagerAcquireParameters),
				TargetManagerReleaseParameters: []byte("{}"),
				TestFetcherName:                "literal",
				TestFetcherFetchParameters:     []byte(testParams),
			},
		}

		jd := job.Descriptor{
			TestDescriptors: testDescriptors,
			JobName:         "Test",
			Reporting: job.Reporting{
				RunReporters: []job.ReporterConfig{
					{Name: "noop"},
				},
			},
		}

		result, err := NewJobFromDescriptor(xcontext.Background(), pr, &jd)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Tests, 1)
		require.Equal(t, "TestVariables", result.Tests[0].Name)
	})

	t.Run("unexisting_label", func(t *testing.T) {
		testParams := `{
			"TestName": "TestVariables",
			"Steps": [
				{
					"name": "echo",
					"label": "echo1",
					"parameters": {
						"text": ["Some text1"]
					}
				},
				{
					"name": "echo",
					"label": "echo2",
					"parameters": {
						"text": ["Some text1"]
					},
					"variablesmapping": {
						"output_var": "noecho.message"
					}
				}
			]
		}`

		testDescriptors := []*test.TestDescriptor{
			{
				TargetManagerName:              "targetList",
				TargetManagerAcquireParameters: []byte(targetManagerAcquireParameters),
				TargetManagerReleaseParameters: []byte("{}"),
				TestFetcherName:                "literal",
				TestFetcherFetchParameters:     []byte(testParams),
			},
		}

		jd := job.Descriptor{
			TestDescriptors: testDescriptors,
			JobName:         "Test",
			Reporting: job.Reporting{
				RunReporters: []job.ReporterConfig{
					{Name: "noop"},
				},
			},
		}

		_, err := NewJobFromDescriptor(xcontext.Background(), pr, &jd)
		require.Error(t, err)
	})
}

func TestNewJobNoTestSteps(t *testing.T) {
	pr := pluginregistry.NewPluginRegistry(xcontext.Background())
	// require.NoError(t, pr.RegisterTestStep(echo.Load()))
	require.NoError(t, pr.RegisterTargetManager(targetlist.Load()))
	require.NoError(t, pr.RegisterTestFetcher(literal.Load()))
	require.NoError(t, pr.RegisterReporter(noop.Load()))

	testFetcherParams := `{
	    "TestName": "TestDisabled",
		"Steps": [
		]
	}`

	targetManagerAcquireParameters := `{
		"Targets": [
			{
				"ID": "id1",
				"FQDN": "some-host.example.com"
			}
		]
	}`

	testDescriptors := []*test.TestDescriptor{
		{
			TargetManagerName:              "targetList",
			TargetManagerAcquireParameters: []byte(targetManagerAcquireParameters),
			TargetManagerReleaseParameters: []byte("{}"),
			TestFetcherName:                "literal",
			TestFetcherFetchParameters:     []byte(testFetcherParams),
		},
	}
	jd := job.Descriptor{
		TestDescriptors: testDescriptors,
		JobName:         "Test",
		Reporting: job.Reporting{
			RunReporters: []job.ReporterConfig{
				{Name: "noop"},
			},
		},
	}

	_, err := NewJobFromDescriptor(xcontext.Background(), pr, &jd)
	require.Error(t, err)
}
