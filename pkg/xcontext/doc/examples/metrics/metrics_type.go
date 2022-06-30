package main

import (
	"flag"
	"fmt"
	"strings"
)

type MetricsType int

const (
	MetricsTypeUndefined = MetricsType(iota)
	MetricsTypeSimple
	MetricsTypePrometheus
	MetricsTypeTSMetrics
	endOfMetricsType
)

var _ flag.Value = (*MetricsType)(nil)

// String implements flag.Value
func (t MetricsType) String() string {
	switch t {
	case MetricsTypeUndefined:
		return "undefined"
	case MetricsTypeSimple:
		return "simple"
	case MetricsTypePrometheus:
		return "prometheus"
	case MetricsTypeTSMetrics:
		return "tsmetrics"
	}
	return fmt.Sprintf("unknown_%d", int(t))
}

// Set implements flag.Value
func (t *MetricsType) Set(in string) error {
	in = strings.Trim(strings.ToLower(in), " ")
	for c := MetricsType(0); c < endOfMetricsType; c++ {
		if c.String() == in {
			*t = c
			return nil
		}
	}
	return fmt.Errorf("unknown metrics type: '%s'", t)
}
