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

	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/linuxboot/contest/cmds/admin_server/server"
	"github.com/linuxboot/contest/pkg/types"
)

const (
	DefaultBufferSize    = 10
	DefaultMaxBatchSize  = 500000 // size in bytes
	DefaultMaxBatchCount = 100
	DefaultBatchSendFreq = 1 * time.Second
	DefaultLogTimeout    = 1 * time.Second
)

type Config struct {
	Addr          string
	BufferSize    int
	MaxBatchSize  int
	MaxBatchCount int
	BatchSendFreq time.Duration
	LogTimeout    time.Duration
}

// Batch defines a log batch that handles the size in bytes of the logs
type Batch struct {
	addr string
	logs []server.Log
	size uint64
}

func NewBatch(addr string) Batch {
	return Batch{
		addr: addr,
	}
}

func (b *Batch) Add(log server.Log) {
	b.logs = append(b.logs, log)
	b.size += uint64(len(log.LogData))
}

func (b *Batch) Size() uint64 {
	return b.size
}

func (b *Batch) Count() int {
	return len(b.logs)
}

// PostAndReset makes a post request sending hh.batch and reseting the batch
func (b *Batch) PostAndReset() error {
	logJson, err := json.Marshal(b.logs)
	if err != nil {
		return fmt.Errorf("Marshal Err: %v", err)
	}
	requestBody := bytes.NewBuffer(logJson)
	_, err = http.Post(b.addr, "application/json", requestBody)
	if err != nil {
		return fmt.Errorf("Http Logger Err: %v", err)
	}

	b.logs = nil
	b.size = 0
	return nil
}

type HttpHook struct {
	cfg         Config
	batch       Batch
	batchTicker *time.Ticker

	logChan   chan server.Log
	flushChan chan struct{}
	closeChan chan struct{}
}

var _ logger.Hook = (*HttpHook)(nil)

func NewHttpHook(cfg Config) (*HttpHook, error) {
	url, err := url.ParseRequestURI(cfg.Addr)
	if err != nil {
		return nil, err
	}
	// add the endpoint to the server addr
	url.Path = path.Join(url.Path, "log")

	hh := HttpHook{
		cfg:         cfg,
		batch:       NewBatch(url.String()),
		batchTicker: time.NewTicker(cfg.BatchSendFreq),
		logChan:     make(chan server.Log, cfg.BufferSize),
		flushChan:   make(chan struct{}),
		closeChan:   make(chan struct{}),
	}

	go hh.logHandler()

	return &hh, nil

}

// this implements go-belt's Logger Hook interface
func (hh *HttpHook) ProcessLogEntry(entry *logger.Entry) bool {
	msg := strings.TrimRight(entry.Message, "\n")
	if msg == "" {
		return true
	}

	var jobID types.JobID
	entry.Fields.ForEachField(func(f *field.Field) bool {
		switch f.Key {
		case "job_id":
			jobID, _ = f.Value.(types.JobID)
			return false
		}
		return true
	})

	log := server.Log{
		LogData:  msg,
		JobID:    uint64(jobID),
		LogLevel: entry.Level.String(),
		Date:     entry.Timestamp,
	}

	// timeout is used to not block the service on the logging
	timeout := time.After(hh.cfg.LogTimeout)
	select {
	case hh.logChan <- log:
	// do nothing
	case <-timeout:
		fmt.Fprintf(os.Stderr, "Logging ProcessLogEntry timeout: %v", log) // TODO: remove os.Stderr hardcodes
	}

	return true
}

// logHandler consumes logChan and pushes the logs to the admin server
func (hh *HttpHook) logHandler() {
	for {
		select {
		case log := <-hh.logChan:
			hh.batch.Add(log)
			if hh.batch.Count() > hh.cfg.MaxBatchCount || hh.batch.Size() > uint64(hh.cfg.MaxBatchSize) {
				if !hh.doFlush() {
					break
				}
				// if the batch is sent
				// to avoid ticking on an empty batch
				hh.batchTicker.Reset(hh.cfg.BatchSendFreq)
			}
		case <-hh.flushChan:
			if hh.batch.Size() > 0 {
				hh.doFlush()
			}
		case <-hh.batchTicker.C:
			if hh.batch.Size() > 0 {
				hh.doFlush()
			}
		case <-hh.closeChan:
			// if there are logs in the buffered batch, send them
			if hh.batch.Count() > 0 {
				hh.doFlush()
			}
			fmt.Fprintf(os.Stderr, "Closing http logger") // TODO: remove os.Stderr hardcodes
			return
		}
	}
}

func (hh *HttpHook) doFlush() bool {
	err := hh.batch.PostAndReset()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Send Batch failed: %v", err) // TODO: remove os.Stderr hardcodes
	}
	return err == nil
}

func (hh *HttpHook) Flush() {
	hh.flushChan <- struct{}{}
}

// Close ends the logHandler goroutine
func (hh *HttpHook) Close() {
	hh.closeChan <- struct{}{}
	hh.batchTicker.Stop()
	// to mark further Close as no-op
	hh.closeChan = nil
}
