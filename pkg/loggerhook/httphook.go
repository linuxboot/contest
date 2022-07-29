package loggerhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/server"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/sirupsen/logrus"
)

var (
	DefaultBufferSize = 10
	DefaultLogTimeout = 1 * time.Second
)

type HttpHook struct {
	Addr      string
	logChan   chan server.Log
	closeChan chan struct{}
}

func NewHttpHook(addr string) (*HttpHook, error) {
	url, err := url.ParseRequestURI(addr)
	if err != nil {
		return nil, err
	}
	// add the endpoint to the server addr
	url.Path = path.Join(url.Path, "log")

	hh := HttpHook{
		Addr:      url.String(),
		logChan:   make(chan server.Log, DefaultBufferSize),
		closeChan: make(chan struct{}),
	}

	go hh.logHandler()

	return &hh, nil

}

// this implements logrus Hook interface
func (hh *HttpHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// this implements logrus Hook interface
func (hh *HttpHook) Fire(entry *logrus.Entry) error {
	msg := strings.TrimRight(entry.Message, "\n")
	if msg == "" {
		return nil
	}

	jobId, ok := entry.Data["job_id"]
	jobIdInt, notJobID := jobId.(types.JobID)
	if !ok || !notJobID {
		// to indicate an invalid job id
		jobIdInt = 0
	}

	log := server.Log{
		LogData:  msg,
		JobID:    uint64(jobIdInt),
		LogLevel: entry.Level.String(),
		Date:     entry.Time,
	}

	// timeout is used to not block the service on the logging
	timeout := time.After(DefaultLogTimeout)
	select {
	case hh.logChan <- log:
	// do nothing
	case <-timeout:
		fmt.Fprintf(os.Stderr, "Logging Fire timeout: %v", log)
	}

	return nil
}

// logHandler consumes logChan and pushes the logs to the admin server
func (hh *HttpHook) logHandler() {
	for {
		select {
		case log := <-hh.logChan:
			logJson, err := json.Marshal(log)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Marshal Err: %v", err)
			}
			requestBody := bytes.NewBuffer(logJson)
			_, err = http.Post(hh.Addr, "application/json", requestBody)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Http Logger Err: %v", err)
			}
		case <-hh.closeChan:
			fmt.Fprintf(os.Stderr, "Closing http logger")
			return
		}
	}
}

// Close ends the logHandler goroutine
func (hh *HttpHook) Close() {
	hh.closeChan <- struct{}{}
	// to mark further Close as no-op
	hh.closeChan = nil
}
