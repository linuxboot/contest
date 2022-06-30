// Here we implement a simple HTTP service, which also uses xcontext to handle
// trafficstars metrics

// This file knows about which metrics implementation do we use, since it is
// the glue code file `main.go`.

package main

import (
	"bytes"
	"flag"
	"log"
	"net/http"

	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
	_ "github.com/linuxboot/contest/pkg/xcontext/logger/logadapter/zap"
	promadapter "github.com/linuxboot/contest/pkg/xcontext/metrics/prometheus"
	"github.com/linuxboot/contest/pkg/xcontext/metrics/simplemetrics"
	tsmadapter "github.com/linuxboot/contest/pkg/xcontext/metrics/tsmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	tsmetrics "github.com/xaionaro-go/metrics"
	"github.com/xaionaro-go/statuspage"
	"go.uber.org/zap"
)

func main() {
	metricsType := MetricsTypePrometheus
	flag.Var(&metricsType, "metrics-type", "choose: prometheus, tsmetrics, simple")
	flag.Parse()

	zapLogger, err := zap.NewProduction()
	if err != nil {
		log.Fatal(err)
	}

	var prometheusRegistry *prometheus.Registry
	ctx := xcontext.Background().WithLogger(logger.ConvertLogger(zapLogger))
	switch metricsType {
	case MetricsTypeSimple:
		ctx = ctx.WithMetrics(simplemetrics.New())
	case MetricsTypePrometheus:
		prometheusRegistry = prometheus.NewRegistry()
		ctx = ctx.WithMetrics(promadapter.New(prometheusRegistry, prometheusRegistry))
	case MetricsTypeTSMetrics:
		ctx = ctx.WithMetrics(tsmadapter.New())
	default:
		ctx.Fatalf("unknown metrics type: %s", metricsType)
	}

	srv := newServer(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.Hello)
	mux.HandleFunc("/sleep", srv.Sleep)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		switch metricsType {
		case MetricsTypeSimple:
			exportSimpleMetrics(w)
		case MetricsTypePrometheus:
			exportPrometheusMetrics(ctx, w, prometheusRegistry)
		case MetricsTypeTSMetrics:
			tsmetrics.SetDefaultTags(tsmetrics.Tags{})
			exportTSMetrics(w, ctx.Metrics().(*tsmadapter.Metrics))
		default:
			ctx.Fatalf("unknown metrics type: %s", metricsType)
		}
	})
	httpSrv := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	ctx.Infof("binding to %s", httpSrv.Addr)
	ctx.Fatalf("%v", httpSrv.ListenAndServe())
}

func exportSimpleMetrics(w http.ResponseWriter) {
	// TBD

	w.WriteHeader(http.StatusNotImplemented)
}

func exportPrometheusMetrics(ctx xcontext.Context, w http.ResponseWriter, registry *prometheus.Registry) {
	metrics, err := registry.Gather()
	if err != nil {
		ctx.Errorf("%v", ctx)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.FmtText)
	for _, metric := range metrics {
		if err := enc.Encode(metric); err != nil {
			ctx.Errorf("%v", ctx)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(buf.Bytes())
	if err != nil {
		ctx.Errorf("%v", err)
	}
}

func exportTSMetrics(w http.ResponseWriter, metrics *tsmadapter.Metrics) {
	statuspage.WriteMetricsPrometheus(w, metrics.Registry)
}
