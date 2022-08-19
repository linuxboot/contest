package storage

import (
	"errors"
	"time"

	"github.com/linuxboot/contest/pkg/xcontext"
)

var (
	ErrReadOnlyStorage = errors.New("error read only storage")
	ErrInsert          = errors.New("error inserting into the db")
	ErrConstructQuery  = errors.New("error forming db query from api query")
	ErrQuery           = errors.New("error querying from the database")
)

var (
	DefaultTimestampFormat = "2006-01-02T15:04:05.000Z07:00"
)

type Storage interface {
	StoreLogs(ctx xcontext.Context, logs []Log) error
	GetLogs(ctx xcontext.Context, query Query) (*Result, error)
}

// Log defines the basic log info pushed by the server
type Log struct {
	JobID    uint64
	LogData  string
	Date     time.Time
	LogLevel string
}

// Query defines the different options to filter with
type Query struct {
	JobID     *uint64
	LogData   *string
	LogLevel  *string
	StartDate *time.Time
	EndDate   *time.Time
	PageSize  uint
	Page      uint
}

//Result defines the expected result returned from the db
type Result struct {
	Logs     []Log
	Count    uint64
	Page     uint
	PageSize uint
}
