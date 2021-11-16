package jobmanager

import (
	"encoding/json"
	"github.com/insomniacslk/xjson"
	"testing"
	"time"

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

func TestTestDescriptorParameters(t *testing.T) {
	testFetcherParams := `{
	    "TestName": "SomeTest",
		"Steps": [
			{
				"name": "echo",
				"label": "echo text",
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
		JobName:         "Test",
		Tags:            []string{"tag1", "tag2"},
		Runs:            5,
		RunUntilSucceed: true,
		RunInterval:     xjson.Duration(time.Minute),
		TestDescriptors: testDescriptors,
		Reporting: job.Reporting{
			RunReporters: []job.ReporterConfig{
				{Name: "noop"},
			},
		},
	}

	pr := simplePluginRegister(t)
	job, err := NewJobFromDescriptor(xcontext.Background(), pr, &jd)
	require.NoError(t, err)
	require.NotNil(t, job)

	require.Equal(t, jd.JobName, job.Name)
	require.Equal(t, jd.Tags, job.Tags)
	require.Equal(t, jd.Runs, job.Runs)
	require.Equal(t, jd.RunUntilSucceed, job.RunUntilSucceed)
	require.Equal(t, time.Duration(jd.RunInterval), job.RunInterval)

	require.Len(t, job.Tests, 1)
	require.Equal(t, "SomeTest", job.Tests[0].Name)

	require.Len(t, job.Tests[0].TestStepsBundles, 1)
	require.Equal(t, "echo text", job.Tests[0].TestStepsBundles[0].TestStepLabel)

	require.Equal(t, test.TestStepParameters{
		"text": []test.Param{
			{
				RawMessage: json.RawMessage("\"Some text1\""),
			},
		},
	}, job.Tests[0].TestStepsBundles[0].Parameters, 1)
}

func TestDisabledTestDescriptor(t *testing.T) {
	pr := simplePluginRegister(t)

	testFetcherParams1 := `{
	    "TestName": "TestDisabled",
		"Steps": [
			{
				"name": "echo",
				"label": "echo text",
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
				"label": "echo text",
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
				"FQDN": "some-host.example.com"
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
	pr := simplePluginRegister(t)

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

func TestNewJobNoTestSteps(t *testing.T) {
	pr := simplePluginRegister(t)

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

func simplePluginRegister(t *testing.T) *pluginregistry.PluginRegistry {
	pr := pluginregistry.NewPluginRegistry(xcontext.Background())
	require.NoError(t, pr.RegisterTestStep(echo.Load()))
	require.NoError(t, pr.RegisterTargetManager(targetlist.Load()))
	require.NoError(t, pr.RegisterTestFetcher(literal.Load()))
	require.NoError(t, pr.RegisterReporter(noop.Load()))
	return pr
}
