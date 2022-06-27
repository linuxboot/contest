// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build integration_storage
// +build integration_storage

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
    "Reporting": {
        "FinalReporters": [
            {
                "Name": "noop"
            }
        ],
        "RunReporters": [
            {
                "Name": "TargetSuccess",
                "Parameters": {
                    "SuccessExpression": "=80%"
                }
            },
            {
                "Name": "Noop"
            }
        ]
    },
    "RunInterval": "3s",
    "Runs": 3,
    "Tags": [
        "test",
        "csv"
    ],
    "TestDescriptors": [
        {
            "TargetManagerAcquireParameters": {
                "Targets": [
                    {
                        "ID": "1234",
                        "Name": "example.org"
                    }
                ]
            },
            "TargetManagerName": "TargetList",
            "TargetManagerReleaseParameters": {},
            "TestFetcherFetchParameters": {
                "Steps": [ {{ .Steps }} ],
                "TestName": "Literal test"
            },
            "TestFetcherName": "literal"
        }
    ]
}`))

var testSerializedTemplate = template.Must(template.New("testSerialized").Parse(`[[ {{ . }} ]]`))

type templateData struct {
	Steps   string
	Version string
}

func descriptorMust(data *templateData) string {
	var buf bytes.Buffer
	if err := jobDescriptorTemplate.Execute(&buf, data); err != nil {
		panic(err)
	}
	return buf.String()
}

func testSerializedMust(data string) string {
	var buf bytes.Buffer
	if err := testSerializedTemplate.Execute(&buf, data); err != nil {
		panic(err)
	}
	return buf.String()
}

var steps = `
{
   "Name":"cmd",
   "Label":"some label",
   "Parameters":{
      "args":[
	 "Title={{ Title .Name }}, ToUpper={{ ToUpper .Name }}"
      ],
      "executable":[
	 "echo"
      ]
   }
}
`

var jobDescriptor = descriptorMust(&templateData{Steps: steps, Version: jobDescriptorVersion})
var testSerialized = testSerializedMust(steps)
