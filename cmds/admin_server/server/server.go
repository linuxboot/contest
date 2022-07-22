package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/linuxboot/contest/cmds/admin_server/storage"
	"github.com/linuxboot/contest/pkg/xcontext"
)

var (
	MaxPageSize uint = 100
	DefaultPage uint = 0
)

type Query struct {
	Text      *string    `form:"text"`
	LogLevel  *string    `form:"logLevel"`
	StartDate *time.Time `form:"startDate" time_format:"2006-01-02T15:04:05.000Z07:00"`
	EndDate   *time.Time `form:"endDate" time_format:"2006-01-02T15:04:05.000Z07:00"`
	PageSize  *uint      `form:"pageSize"`
	Page      *uint      `form:"page"`
}

// toStorageQurey returns a storage Query and populates the required fields
func (q *Query) ToStorageQuery() storage.Query {
	storageQuery := storage.Query{
		Page:     DefaultPage,
		PageSize: MaxPageSize,
	}

	storageQuery.Text = q.Text
	storageQuery.LogLevel = q.LogLevel
	storageQuery.StartDate = q.StartDate
	storageQuery.EndDate = q.EndDate

	if q.Page != nil {
		storageQuery.Page = *q.Page
	}

	if q.PageSize != nil && *q.PageSize < MaxPageSize {
		storageQuery.PageSize = *q.PageSize
	}

	return storageQuery
}

type Log struct {
	JobID    uint64    `json:"jobID"`
	LogData  string    `json:"logData"`
	Date     time.Time `json:"date"`
	LogLevel string    `json:"logLevel"`
}

func (l *Log) ToStorageLog() storage.Log {
	return storage.Log{
		JobID:    l.JobID,
		LogData:  l.LogData,
		Date:     l.Date,
		LogLevel: l.LogLevel,
	}
}

type RouteHandler struct {
	storage storage.Storage
}

// status is a simple endpoint to check if the serves is alive
func (r *RouteHandler) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "live"})
}

// addLog inserts a new log entry inside the database
func (r *RouteHandler) addLog(c *gin.Context) {
	var log Log
	if err := c.Bind(&log); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "err", "msg": "badly formatted log"})
		fmt.Fprintf(os.Stderr, "Err while binding request body %v", err)
		return
	}

	err := r.storage.StoreLog(log.ToStorageLog())
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInsert):
			c.JSON(http.StatusInternalServerError, gin.H{"status": "err", "msg": "error while storing the log"})
		case errors.Is(err, storage.ErrReadOnlyStorage):
			c.JSON(http.StatusNotImplemented, gin.H{"status": "err", "msg": "not supported action"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "err", "msg": "unknown server error"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// geLogs gets logs form the db based on the filters
func (r *RouteHandler) getLogs(c *gin.Context) {
	var query Query
	if err := c.BindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "err", "msg": fmt.Sprintf("bad formatted query %v", err)})
		return
	}

	result, err := r.storage.GetLogs(query.ToStorageQuery())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "err", "msg": "error while getting the logs"})
		return
	}

	c.JSON(http.StatusOK, result)
}

func initRouter(ctx xcontext.Context, rh RouteHandler) *gin.Engine {

	r := gin.New()
	r.Use(gin.Logger())

	// serve the frontend app
	r.Use(static.Serve("/", static.LocalFile("./frontend/dist", true)))

	r.GET("/status", rh.status)
	r.POST("/log", rh.addLog)
	r.GET("/log", rh.getLogs)

	return r
}

func Serve(ctx xcontext.Context, port int, storage storage.Storage) error {
	log := ctx.Logger()

	routeHandler := RouteHandler{
		storage: storage,
	}
	router := initRouter(ctx, routeHandler)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: router,
	}

	go func() {
		<-ctx.Done()
		// on cancel close the server
		log.Debugf("Closing the server")
		if err := server.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing the server: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return ctx.Err()
}
