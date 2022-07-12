package storage

import (
	"errors"
	"time"
)

var (
	ErrReadOnlyStorage = errors.New("error read only storage")
	ErrInsert          = errors.New("error inserting into the db")
)

type Storage interface {
	StoreLog(Log) error
	GetLogs(Query) ([]Log, error)

	Close() error
}

// Log defines the basic log info pushed by the server
type Log struct {
	JobID    uint64    `json:"jobID"`
	LogData  string    `json:"logData"`
	Date     time.Time `json:"date"`
	LogLevel string    `json:"logLevel"`
}

// Query defines the different options to filter with
type Query struct {
	Text      string
	LogLevel  string
	StartDate string
	EndDate   string
	Page      int
}
