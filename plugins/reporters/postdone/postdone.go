// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package postdone

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/facebookincubator/contest/pkg/event/testevent"
	"github.com/facebookincubator/contest/pkg/job"
	"github.com/facebookincubator/contest/pkg/logging"
	"github.com/facebookincubator/contest/pkg/xcontext"
)

// Name defines the name of the reporter used within the plugin registry
var Name = "postdone"

var log = logging.GetLogger("reporter/" + strings.ToLower(Name))

// postdone is a reporter that does nothing. Probably only useful for testing.
type postdone struct{}

type RunParameters struct {
	ApiURI string
}

// ValidateRunParameters validates the parameters for the run reporter
func (d *postdone) ValidateRunParameters(params []byte) (interface{}, error) {
	var fp RunParameters
	if err := json.Unmarshal(params, &fp); err != nil {
		return nil, err
	}
	_, err := url.ParseRequestURI(fp.ApiURI)
	if err != nil {
		log.Errorf("ApiURI is not formatted right")
		return fp, err
	}
	return fp, nil
}

// ValidateFinalParameters validates the parameters for the final reporter
func (d *postdone) ValidateFinalParameters(params []byte) (interface{}, error) {
	return nil, nil
}

// Name returns the Name of the reporter
func (d *postdone) Name() string {
	return Name
}

// RunReport calculates the report to be associated with a job run.
func (d *postdone) RunReport(ctx xcontext.Context, parameters interface{}, runStatus *job.RunStatus, ev testevent.Fetcher) (bool, interface{}, error) {
	var fp RunParameters
	b, err := json.Marshal(parameters)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(b, &fp); err != nil { // WORKS??
		return false, nil, err
	}
	data := map[string]interface{}{
		"ID":     runStatus.RunCoordinates.JobID,
		"Status": true,
	}
	json_data, err := json.Marshal(data) //Marshal that data
	if err != nil {
		fmt.Println("Could not parse data to json format.")
	}
	resp, err := http.Post(fp.ApiURI, "application/json", bytes.NewBuffer(json_data)) //HTTP Post all to the API
	if err != nil {
		fmt.Println("Could not post data to API.")
		return false, nil, nil
	}
	if resp.StatusCode != 200 {
		fmt.Println("The HTTP Post responded a statuscode != 200")
		return false, nil, nil
	}
	return true, "I did nothing", nil
}

// FinalReport calculates the final report to be associated to a job.
func (d *postdone) FinalReport(ctx xcontext.Context, parameters interface{}, runStatuses []job.RunStatus, ev testevent.Fetcher) (bool, interface{}, error) {
	return true, nil, nil
}

// New builds a new TargetSuccessReporter
func New() job.Reporter {
	return &postdone{}
}

// Load returns the name and factory which are needed to register the Reporter
func Load() (string, job.ReporterFactory) {
	return Name, New
}
