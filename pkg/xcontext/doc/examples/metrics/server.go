// This file is agnostic of which metrics implementation do we use.

package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/linuxboot/contest/pkg/xcontext"
)

type server struct {
	BaseContext xcontext.Context
}

func newServer(ctx xcontext.Context) *server {
	return &server{BaseContext: ctx}
}

func (srv *server) Hello(w http.ResponseWriter, r *http.Request) {
	ctx := srv.BaseContext
	metrics := ctx.Metrics()

	reqCount := metrics.IntGauge("concurrent_requests")
	reqCount.Add(1)
	defer reqCount.Add(-1)

	metrics.Count("hello_requests").Add(1)
	srv.writeResponse(ctx, w, http.StatusOK, []byte("Hello!\n"))
}

func (srv *server) Sleep(w http.ResponseWriter, r *http.Request) {
	ctx := srv.BaseContext
	metrics := ctx.Metrics()

	reqCount := metrics.IntGauge("concurrent_requests")
	reqCount.Add(1)
	defer reqCount.Add(-1)

	secsVals := r.URL.Query()["secs"]
	if len(secsVals) != 1 {
		metrics.WithTag("error", "wrong_amount_of_secs").Count("errors").Add(1)
		srv.writeResponse(ctx, w, http.StatusBadRequest, []byte("wrong amount of 'secs' parameter\n"))
		return
	}
	sleepSeconds, err := strconv.ParseFloat(secsVals[0], 64)
	if err != nil {
		metrics.WithTag("error", "invalid_secs").Count("errors").Add(1)
		srv.writeResponse(ctx, w, http.StatusBadRequest, []byte("invalid value of the 'secs' parameter\n"))
		return
	}
	sleepDuration := time.Duration(sleepSeconds * float64(time.Second))

	time.Sleep(sleepDuration)
	metrics.Count("slept_ns").Add(uint64(sleepDuration.Nanoseconds()))
	srv.writeResponse(ctx, w, http.StatusOK, []byte("done\n"))
}

func (srv *server) writeResponse(
	ctx xcontext.Context,
	w http.ResponseWriter,
	status int,
	body []byte,
) {
	ctx = ctx.WithTag("status", status)
	ctx.Metrics().Count("response").Add(1)

	w.WriteHeader(status)
	_, err := w.Write(body)
	if err != nil {
		ctx.Errorf("%v", err)
	}
}
