package sysbench

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps/abstraction/transport"
)

const (
	supportedProto = "ssh"
	privileged     = "sudo"
	sysbench       = "sysbench"
	cmd            = "cpu"
	argument       = "stats"
	jsonFlag       = "--json"
)

type Output struct {
	Result string `json:"result"`
}

type TargetRunner struct {
	ts *TestStep
	ev testevent.Emitter
}

func NewTargetRunner(ts *TestStep, ev testevent.Emitter) *TargetRunner {
	return &TargetRunner{
		ts: ts,
		ev: ev,
	}
}

func (r *TargetRunner) Run(ctx xcontext.Context, target *target.Target) error {
	var outputBuf strings.Builder

	// limit the execution time if specified
	var cancel xcontext.CancelFunc

	if r.ts.Options.Timeout != 0 {
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
		defer cancel()
	} else {
		r.ts.Options.Timeout = xjson.Duration(defaultTimeout)
		ctx, cancel = xcontext.WithTimeout(ctx, time.Duration(r.ts.Options.Timeout))
		defer cancel()
	}

	pe := test.NewParamExpander(target)

	if r.ts.Transport.Proto != supportedProto {
		err := fmt.Errorf("only '%s' is supported as protocol in this teststep", supportedProto)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if len(r.ts.Parameter.Args) == 0 {
		r.ts.Parameter.Args = defaultArgs
	}

	r.ts.writeTestStep(&outputBuf)

	transport, err := transport.NewTransport(r.ts.Transport.Proto, r.ts.Transport.Options, pe)
	if err != nil {
		err := fmt.Errorf("failed to create transport: %w", err)
		outputBuf.WriteString(fmt.Sprintf("%v", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	if err := r.ts.runPerformance(ctx, &outputBuf, transport); err != nil {
		outputBuf.WriteString(fmt.Sprintf("%v\n", err))

		return emitStderr(ctx, outputBuf.String(), target, r.ev, err)
	}

	return emitStdout(ctx, outputBuf.String(), target, r.ev)
}

func (ts *TestStep) runPerformance(ctx xcontext.Context, outputBuf *strings.Builder, transport transport.Transport,
) error {
	proc, err := transport.NewProcess(ctx, sysbench, ts.Parameter.Args, "")
	if err != nil {
		return fmt.Errorf("Failed to create proc: %w", err)
	}

	writeCommand(proc.String(), outputBuf)

	stdoutPipe, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("Failed to pipe stdout: %v", err)
	}

	stderrPipe, err := proc.StderrPipe()
	if err != nil {
		return fmt.Errorf("Failed to pipe stderr: %v", err)
	}

	// try to start the process, if that succeeds then the outcome is the result of
	// waiting on the process for its result; this way there's a semantic difference
	// between "an error occured while launching" and "this was the outcome of the execution"
	outcome := proc.Start(ctx)
	if outcome == nil {
		outcome = proc.Wait(ctx)
	}

	stdout, stderr := getOutputFromReader(stdoutPipe, stderrPipe, outputBuf)

	if len(string(stdout)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stdout:\n%s\n", string(stdout)))
	} else if len(string(stderr)) > 0 {
		outputBuf.WriteString(fmt.Sprintf("Stderr:\n%s\n", string(stderr)))
	}

	if outcome != nil {
		return fmt.Errorf("Failed to get CPU performance data: %v.", outcome)
	}

	if err := ts.parseOutput(outputBuf, stdout); err != nil {
		return fmt.Errorf("failed to parse sysbench output: %v", err)
	}

	return nil
}

// getOutputFromReader reads data from the provided io.Reader instances
// representing stdout and stderr, and returns the collected output as byte slices.
func getOutputFromReader(stdout, stderr io.Reader, outputBuf *strings.Builder) ([]byte, []byte) {
	// Read from the stdout and stderr pipe readers
	outBuffer, err := readBuffer(stdout)
	if err != nil {
		outputBuf.WriteString(fmt.Sprintf("Failed to read from Stdout buffer: %v\n", err))
	}

	errBuffer, err := readBuffer(stderr)
	if err != nil {
		outputBuf.WriteString(fmt.Sprintf("Failed to read from Stderr buffer: %v\n", err))
	}

	return outBuffer, errBuffer
}

// readBuffer reads data from the provided io.Reader and returns it as a byte slice.
// It dynamically accumulates the data using a bytes.Buffer.
func readBuffer(r io.Reader) ([]byte, error) {
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r); err != nil && err != io.EOF {
		return nil, err
	}

	return buf.Bytes(), nil
}

type SysbenchOutput struct {
	CpuSpeed struct {
		EventsPerSecond float64 `json:"events_per_second"`
	} `json:"cpu_speed"`

	GeneralStatistics struct {
		TotalTime   float64 `json:"total_time"`
		TotalEvents int     `json:"total_number_of_events"`
	} `json:"general_statistics"`

	Latency struct {
		Min            float64 `json:"min"`
		Average        float64 `json:"avg"`
		Max            float64 `json:"max"`
		Percentile95th float64 `json:"95th_percentile"`
		Sum            float64 `json:"sum"`
	} `json:"latency"`

	ThreadsFairness struct {
		AverageEventsPerThread        float64 `json:"average_events_per_thread"`
		AverageExecutionTimePerThread float64 `json:"average_execution_time_per_thread"`
	} `json:"threads_fairness"`
}

func (ts *TestStep) parseOutput(outputBuf *strings.Builder, data []byte) error {
	output := SysbenchOutput{}

	// Parse CPU Speed
	reCPUSpeed := regexp.MustCompile(`events per second:\s+([\d.]+)`)
	cpuSpeedMatches := reCPUSpeed.FindSubmatch(data)
	if len(cpuSpeedMatches) > 1 {
		cpuSpeed, err := strconv.ParseFloat(string(cpuSpeedMatches[1]), 64)
		if err != nil {
			return err
		}

		output.CpuSpeed.EventsPerSecond = cpuSpeed
	}

	// Parse General Statistics
	reTotalTime := regexp.MustCompile(`total time:\s+([\d.]+)s`)
	totalTimeMatches := reTotalTime.FindSubmatch(data)
	if len(totalTimeMatches) > 1 {
		totalTime, err := strconv.ParseFloat(string(totalTimeMatches[1]), 64)
		if err != nil {
			return err
		}

		output.GeneralStatistics.TotalTime = totalTime
	}

	reTotalEvents := regexp.MustCompile(`total number of events:\s+(\d+)`)
	totalEventsMatches := reTotalEvents.FindSubmatch(data)
	if len(totalEventsMatches) > 1 {
		totalEvents, err := strconv.Atoi(string(totalEventsMatches[1]))
		if err != nil {
			return err
		}

		output.GeneralStatistics.TotalEvents = totalEvents
	}

	// Parse Latency
	reLatencyMin := regexp.MustCompile(`min:\s+([\d.]+)`)
	latencyMinMatches := reLatencyMin.FindSubmatch(data)
	if len(latencyMinMatches) > 1 {
		latencyMin, err := strconv.ParseFloat(string(latencyMinMatches[1]), 64)
		if err != nil {
			return err
		}

		output.Latency.Min = latencyMin
	}

	reLatencyAvg := regexp.MustCompile(`avg:\s+([\d.]+)`)
	latencyAvgMatches := reLatencyAvg.FindSubmatch(data)
	if len(latencyAvgMatches) > 1 {
		latencyAvg, err := strconv.ParseFloat(string(latencyAvgMatches[1]), 64)
		if err != nil {
			return err
		}

		output.Latency.Average = latencyAvg
	}

	reLatencyMax := regexp.MustCompile(`max:\s+([\d.]+)`)
	latencyMaxMatches := reLatencyMax.FindSubmatch(data)
	if len(latencyMaxMatches) > 1 {
		latencyMax, err := strconv.ParseFloat(string(latencyMaxMatches[1]), 64)
		if err != nil {
			return err
		}

		output.Latency.Max = latencyMax
	}

	reLatencyP95 := regexp.MustCompile(`95th percentile:\s+([\d.]+)`)
	latencyP95Matches := reLatencyP95.FindSubmatch(data)
	if len(latencyP95Matches) > 1 {
		latencyP95, err := strconv.ParseFloat(string(latencyP95Matches[1]), 64)
		if err != nil {
			return err
		}

		output.Latency.Percentile95th = latencyP95
	}

	reLatencySum := regexp.MustCompile(`sum:\s+([\d.]+)`)
	latencySumMatches := reLatencySum.FindSubmatch(data)
	if len(latencySumMatches) > 1 {
		latencySum, err := strconv.ParseFloat(string(latencySumMatches[1]), 64)
		if err != nil {
			return err
		}

		output.Latency.Sum = latencySum
	}

	// Parse Threads Fairness
	reAvgEventsPerThread := regexp.MustCompile(`events \(avg/stddev\):\s+([\d.]+)/([\d.]+)`)
	avgEventsPerThreadMatches := reAvgEventsPerThread.FindSubmatch(data)
	if len(avgEventsPerThreadMatches) > 2 {
		avgEventsPerThread, err := strconv.ParseFloat(string(avgEventsPerThreadMatches[1]), 64)
		if err != nil {
			return err
		}

		output.ThreadsFairness.AverageEventsPerThread = avgEventsPerThread
	}

	reAvgExecutionTimePerThread := regexp.MustCompile(`execution time \(avg/stddev\):\s+([\d.]+)/([\d.]+)`)
	avgExecutionTimePerThreadMatches := reAvgExecutionTimePerThread.FindSubmatch(data)
	if len(avgExecutionTimePerThreadMatches) > 2 {
		avgExecutionTimePerThread, err := strconv.ParseFloat(string(avgExecutionTimePerThreadMatches[1]), 64)
		if err != nil {
			return err
		}

		output.ThreadsFairness.AverageExecutionTimePerThread = avgExecutionTimePerThread
	}

	for _, option := range ts.expectStepParams {
		switch option.Option {
		case "EventsPerSecond":
			if err := parseValue(int(output.CpuSpeed.EventsPerSecond), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Events per second: '%d'.",
				option.Option, int(output.CpuSpeed.EventsPerSecond)))

		case "TotalTime":
			if err := parseValue(int(output.GeneralStatistics.TotalTime), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Total time: '%d'.",
				option.Option, int(output.GeneralStatistics.TotalTime)))

		case "TotalEvents":
			if err := parseValue(int(output.GeneralStatistics.TotalEvents), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Total events: '%d'.",
				option.Option, int(output.GeneralStatistics.TotalEvents)))

		case "LatencyMin":
			if err := parseValue(int(output.Latency.Min), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Minimum Latency: '%d'.",
				option.Option, int(output.Latency.Min)))

		case "LatencyAvg":
			if err := parseValue(int(output.Latency.Average), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Average Latency: '%d'.",
				option.Option, int(output.Latency.Average)))

		case "LatencyMax":
			if err := parseValue(int(output.Latency.Max), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Maximum Latency: '%d'.",
				option.Option, int(output.Latency.Max)))

		case "LatencyP95":
			if err := parseValue(int(output.Latency.Percentile95th), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. P95 Latency: '%d'.",
				option.Option, int(output.Latency.Percentile95th)))

		case "LatencySum":
			if err := parseValue(int(output.Latency.Sum), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Latency sum: '%d'.",
				option.Option, int(output.Latency.Sum)))

		case "AverageEventsPerThread":
			if err := parseValue(int(output.ThreadsFairness.AverageEventsPerThread), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Average Events per thread: '%d'.",
				option.Option, int(output.ThreadsFairness.AverageEventsPerThread)))

		case "AverageExecutionTimePerThread":
			if err := parseValue(int(output.ThreadsFairness.AverageExecutionTimePerThread), option.Value); err != nil {
				return err
			}

			outputBuf.WriteString(fmt.Sprintf("Result for option '%s' is as expected. Average execution time per thread: '%d'.",
				option.Option, int(output.ThreadsFairness.AverageExecutionTimePerThread)))

		default:
			return fmt.Errorf("option '%s' is not supported", option.Option)
		}
	}

	return nil
}

const (
	bigger  = ">"
	smaller = "<"
	equal   = "="
)

// parseValue parses an integer value 'have' against an expected pattern 'expect' and checks if the 'have' value satisfies the expectation.
// The function supports three cases for the 'expect' pattern: "<30" (value should be less than 30), ">30" (value should be greater than 30),
// and "30-40" (value should be between 30 and 40, inclusive).
func parseValue(have int, expect string) error {
	match, _ := regexp.MatchString(`^(\d+)-(\d+)$`, expect)

	if match {
		// Case for between 2 numbers.
		regex := regexp.MustCompile(`^(\d+)-(\d+)$`)
		match := regex.FindStringSubmatch(expect)

		min, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("failed to convert '%s' into a number: %v", expect, err)
		}

		max, err := strconv.Atoi(match[2])
		if err != nil {
			return fmt.Errorf("failed to convert '%s' into a number: %v", expect, err)
		}

		if have < min || have > max {
			return fmt.Errorf("the value is not in expected range, have: '%d', want: '%d-%d'", have, min, max)
		}
	} else {
		// Cases for below and above.
		regex := regexp.MustCompile(`^([<>=])(\d+)$`)
		match := regex.FindStringSubmatch(expect)

		if len(match) > 1 {
			operator := match[1]
			value, err := strconv.Atoi(match[2])
			if err != nil {
				return fmt.Errorf("failed to convert '%s' into a number: %v", expect, err)
			}

			switch operator {
			case smaller:
				if have > value {
					return fmt.Errorf("value is not as expected, have: '%d', want: %s '%d'", have, operator, value)
				}
			case bigger:
				if have < value {
					return fmt.Errorf("value is not as expected, have: '%d', want: %s '%d'", have, operator, value)
				}
			case equal:
				if have != value {
					return fmt.Errorf("value is not as expected, have: '%d', want: %s '%d'", have, operator, value)
				}
			default:
				return fmt.Errorf("wrong operator, valid operators are '%s', '%s' and '%s'", smaller, bigger, equal)
			}
		} else {
			return fmt.Errorf("value seems to be malformed.")
		}

	}

	return nil
}
