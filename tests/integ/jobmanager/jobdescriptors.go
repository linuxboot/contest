// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build integration || integration_storage
// +build integration integration_storage

package test

import (
	"bytes"
	"text/template"

	"github.com/linuxboot/contest/pkg/job"
)

var jobDescriptorVersion = job.CurrentDescriptorVersion()
var jobDescriptorTemplate = template.Must(template.New("jobDescriptor").Parse(`
{
    "JobName": "test job",
    "Version": "{{ .Version }}",
    "ReporterName": "TargetSuccess",
    "ReporterParameters": {
        "SuccessExpression": ">10%"
    },
    "Runs": {{ .Runs }},
    "RunInterval": "{{ .RunInterval }}",
    "Tags": [
        "integration_testing" {{ .ExtraTags }}
    ],
    "TestDescriptors": [
        {
            "TargetManagerName": "TargetList",
            "TargetManagerAcquireParameters": {
                "Targets": [
                    {
                        "ID": "id1",
                        "Name": "hostname1.example.com"

                    },
                    {
                        "ID": "id2",
                        "Name": "hostname2.example.com"
                    }
                ]
            },
            "TargetManagerReleaseParameters": {},
            "TestFetcherName": "literal",
            {{ .Def }}
        }
    ],
    "Reporting": {
        "RunReporters": [
            {
                "Name": "TargetSuccess",
                "Parameters": {
                    "SuccessExpression": ">0%"
                }
            }
        ]
    }
}
`))

type templateData struct {
	Version     string
	Runs        int
	RunInterval string
	ExtraTags   string
	Def         string
}

// Decouple the template from the function in order not to repeate logic
func descriptorMust2(t *template.Template, data *templateData) string {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		panic(err)
	}
	return buf.String()
}

func descriptorMust(t *template.Template, def string) string {
	return descriptorMust2(t, &templateData{Version: jobDescriptorVersion, Runs: 1, RunInterval: "1s", Def: def})
}

var testStepsNoop = `
    "TestFetcherFetchParameters": {
        "Steps": [
            {
                "name": "noop",
                "label": "noop_label",
                "parameters": {}
            }
        ],
        "TestName": "IntegrationTest: noop"
    }`
var jobDescriptorNoop = descriptorMust(jobDescriptorTemplate, testStepsNoop)
var jobDescriptorNoop2 = descriptorMust2(
	jobDescriptorTemplate,
	&templateData{Version: jobDescriptorVersion, Runs: 1, RunInterval: "1s", Def: testStepsNoop, ExtraTags: `, "foo"`},
)

var jobDescriptorSlowEcho = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "slowecho",
               "label": "slowecho_label",
               "parameters": {
                 "sleep": ["0.5"],
                 "text": ["Hello world"]
               }
           }
       ],
       "TestName": "IntegrationTest: slow echo"
   }`)

var jobDescriptorFailure = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "fail",
               "label": "fail_label",
               "parameters": {}
           }
       ],
       "TestName": "IntegrationTest: fail"
   }`)

var jobDescriptorCrash = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "crash",
               "label": "crash_label",
               "parameters": {}
           }
       ],
       "TestName": "IntegrationTest: crash"
   }`)

var jobDescriptorHang = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "noreturn",
               "label": "noreturn_label",
               "parameters": {}
           }
       ],
       "TestName": "IntegrationTest: noreturn"
   }`)

var jobDescriptorNoLabel = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "noop",
               "parameters": {}
           }
       ],
       "TestName": "IntegrationTest: no_label"
   }`)

var jobDescriptorLabelDuplication = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "noop",
               "label": "some_label_here",
               "parameters": {}
           },
           {
               "name": "noreturn",
               "label": "some_label_here",
               "parameters": {}
           }
       ],
       "TestName": "TestTestStepLabelDuplication"
   }`)

var jobDescriptorNullStep = descriptorMust(jobDescriptorTemplate, `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "noop",
               "label": "some_label_here",
               "parameters": {}
           },
           null
       ],
       "TestName": "IntegrationTest: null TestStep"
   }`)

var nullTestTemplate = template.Must(template.New("jobDescriptor").Parse(`
{
    "JobName": "test job",
    "Version": "{{ .Version }}",
    "Tags": [
        "integration_testing"
    ],
    "TestDescriptors": [
        null
    ],
    "Reporting": {
        "RunReporters": [
            {
                "Name": "TargetSuccess",
                "Parameters": {
                    "SuccessExpression": ">0%"
                }
            }
        ]
    }
}
`))
var jobDescriptorNullTest = descriptorMust2(nullTestTemplate, &templateData{Version: jobDescriptorVersion})

var jobDescriptorBadTag = descriptorMust2(jobDescriptorTemplate, &templateData{
	Version:     jobDescriptorVersion,
	Runs:        2,
	RunInterval: "1s",
	ExtraTags:   `, "a bad one"`,
	Def: `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "slowecho",
               "label": "slowecho_label",
               "parameters": {
                 "sleep": ["2"],
                 "text": ["Hello world"]
               }
           }
       ],
       "TestName": "IntegrationTest: slow echo"
   }`,
})

var jobDescriptorInternalTag = descriptorMust2(jobDescriptorTemplate, &templateData{
	Version:     jobDescriptorVersion,
	Runs:        2,
	RunInterval: "1s",
	ExtraTags:   `, "_foo"`,
	Def: `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "slowecho",
               "label": "slowecho_label",
               "parameters": {
                 "sleep": ["2"],
                 "text": ["Hello world"]
               }
           }
       ],
       "TestName": "IntegrationTest: slow echo"
   }`,
})

var jobDescriptorDuplicateTag = descriptorMust2(jobDescriptorTemplate, &templateData{
	Version:     jobDescriptorVersion,
	Runs:        2,
	RunInterval: "1s",
	ExtraTags:   `, "qwe", "qwe"`,
	Def: `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "slowecho",
               "label": "slowecho_label",
               "parameters": {
                 "sleep": ["2"],
                 "text": ["Hello world"]
               }
           }
       ],
       "TestName": "IntegrationTest: slow echo"
   }`,
})

var jobDescriptorSlowEcho2 = descriptorMust2(jobDescriptorTemplate, &templateData{
	Version:     jobDescriptorVersion,
	Runs:        2,
	RunInterval: "0.5s",
	Def: `
   "TestFetcherFetchParameters": {
       "Steps": [
           {
               "name": "slowecho",
               "label": "Step 1",
               "parameters": {
                 "sleep": ["0.5"],
                 "text": ["Hello step 1"]
               }
           },
           {
               "name": "slowecho",
               "label": "Step 2",
               "parameters": {
                 "sleep": ["0"],
                 "text": ["Hello step 2"]
               }
           }
       ],
       "TestName": "IntegrationTest: resume"
   }`,
})

var readmetaTemplate = template.Must(template.New("jobDescriptor").Parse(`
{
    "JobName": "test job",
    "Version": "{{ .Version }}",
    "Runs": 1,
    "RunInterval": "5s",
    "Tags": [
        "integration_testing"
    ],
    "TestDescriptors": [
        {
            "TargetManagerName": "readmeta",
            "TargetManagerAcquireParameters": {},
            "TargetManagerReleaseParameters": {},
            "TestFetcherName": "literal",
            "TestFetcherFetchParameters": {
                "Steps": [
                    {
                        "name": "readmeta",
                        "label": "readmeta_label",
                        "parameters": {}
                    }
                ],
                "TestName": "IntegrationTest: noop"
            }
        }
    ],
    "Reporting": {
        "RunReporters": [
            {
                "Name": "readmeta",
                "Parameters": {}
            }
        ],
        "FinalReporters": [
            {
                "Name": "readmeta",
                "Parameters": {}
            }
        ]
    }
}
`))
var jobDescriptorReadmeta = descriptorMust2(readmetaTemplate, &templateData{Version: jobDescriptorVersion})
