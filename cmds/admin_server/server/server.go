package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	adminServerJob "github.com/linuxboot/contest/cmds/admin_server/job"
	"github.com/linuxboot/contest/cmds/admin_server/storage"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
)

var (
	MaxPageSize            uint          = 100
	DefaultPage            uint          = 0
	DefaultDBAccessTimeout time.Duration = 10 * time.Second
)

type Query struct {
	JobID     *uint64    `form:"job_id"`
	Text      *string    `form:"text"`
	LogLevel  *string    `form:"log_level"`
	StartDate *time.Time `form:"start_date" time_format:"2006-01-02T15:04:05.000Z07:00"`
	EndDate   *time.Time `form:"end_date" time_format:"2006-01-02T15:04:05.000Z07:00"`
	PageSize  *uint      `form:"page_size"`
	Page      *uint      `form:"page"`
}

// toStorageQurey returns a storage Query and populates the required fields
func (q *Query) ToStorageQuery() storage.Query {
	storageQuery := storage.Query{
		Page:     DefaultPage,
		PageSize: MaxPageSize,
	}

	storageQuery.JobID = q.JobID
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
	JobID    uint64    `json:"job_id"`
	LogData  string    `json:"log_data"`
	Date     time.Time `json:"date"`
	LogLevel string    `json:"log_level"`
}

func (l *Log) ToStorageLog() storage.Log {
	return storage.Log{
		JobID:    l.JobID,
		LogData:  l.LogData,
		Date:     l.Date,
		LogLevel: l.LogLevel,
	}
}

func toServerLog(l *storage.Log) Log {
	return Log{
		JobID:    l.JobID,
		LogData:  l.LogData,
		Date:     l.Date,
		LogLevel: l.LogLevel,
	}
}

type Result struct {
	Logs     []Log  `json:"logs"`
	Count    uint64 `json:"count"`
	Page     uint   `json:"page"`
	PageSize uint   `json:"page_size"`
}

func toServerResult(r *storage.Result) Result {
	var result Result
	result.Count = r.Count
	result.Page = r.Page
	result.PageSize = r.PageSize

	for _, log := range r.Logs {
		result.Logs = append(result.Logs, toServerLog(&log))
	}
	return result
}

type Tag struct {
	Name      string `json:"name"`
	JobsCount uint   `json:"jobs_count"`
}

func fromStorageTags(storageTags []adminServerJob.Tag) []Tag {
	tags := make([]Tag, 0, len(storageTags))
	for _, tag := range storageTags {
		tags = append(tags, Tag{
			Name:      tag.Name,
			JobsCount: tag.JobsCount,
		})
	}
	return tags
}

type report struct {
	ReporterName string     `json:"reporter_name"`
	Success      *bool      `json:"success"`
	Time         *time.Time `json:"time"`
	Data         *string    `json:"data"`
}
type Job struct {
	JobID  types.JobID `json:"job_id"`
	Report *report     `json:"report"`
}

func fromStorageJobs(storageJobs []adminServerJob.Job) []Job {
	jobs := make([]Job, 0, len(storageJobs))
	for _, job := range storageJobs {
		var r *report
		if job.ReporterName != nil {
			r = &report{
				ReporterName: *job.ReporterName,
				Success:      job.Success,
				Time:         job.ReportTime,
				Data:         job.Data,
			}
		}

		jobs = append(jobs, Job{
			JobID:  job.JobID,
			Report: r,
		})
	}
	return jobs
}

type RouteHandler struct {
	storage    storage.Storage
	jobStorage adminServerJob.Storage
	log        logger.Logger
}

// status is a simple endpoint to check if the serves is alive
func (r *RouteHandler) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "live"})
}

// addLogs inserts log's batches into the database
func (r *RouteHandler) addLogs(c *gin.Context) {
	var logs []Log
	if err := c.Bind(&logs); err != nil {
		c.JSON(http.StatusBadRequest, makeRestErr("badly formatted logs"))
		r.log.Errorf("Err while binding request body %v", err)
		return
	}

	storageLogs := make([]storage.Log, 0, len(logs))
	for _, log := range logs {
		storageLogs = append(storageLogs, log.ToStorageLog())
	}

	ctx, cancel := xcontext.WithTimeout(xcontext.Background(), DefaultDBAccessTimeout)
	defer cancel()
	ctx = ctx.WithLogger(r.log)
	err := r.storage.StoreLogs(ctx, storageLogs)
	if err != nil {
		r.log.Errorf("Err while storing logs: %v", err)
		switch {
		case errors.Is(err, storage.ErrInsert):
			c.JSON(http.StatusInternalServerError, makeRestErr("error while storing the batch"))
		case errors.Is(err, storage.ErrReadOnlyStorage):
			c.JSON(http.StatusNotImplemented, makeRestErr("not supported action"))
		default:
			c.JSON(http.StatusInternalServerError, makeRestErr("unknown server error"))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// geLogs gets logs form the db based on the filters
func (r *RouteHandler) getLogs(c *gin.Context) {
	var query Query
	if err := c.BindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, makeRestErr("bad formatted query %v", err))
		r.log.Errorf("Err while binding request body %v", err)
		return
	}

	ctx, cancel := xcontext.WithTimeout(xcontext.Background(), DefaultDBAccessTimeout)
	defer cancel()
	ctx = ctx.WithLogger(r.log)
	result, err := r.storage.GetLogs(ctx, query.ToStorageQuery())
	if err != nil {
		c.JSON(http.StatusInternalServerError, makeRestErr("error while getting the logs"))
		r.log.Errorf("Err while getting logs from storage: %v", err)
		return
	}

	c.JSON(http.StatusOK, toServerResult(result))
}

// getTags gets the tags with similar name to text
func (r *RouteHandler) getTags(c *gin.Context) {
	var query struct {
		Text string `form:"text"`
	}
	if err := c.BindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, makeRestErr("bad formatted query %v", err))
		r.log.Errorf("Err while binding request body %v", err)
		return
	}

	ctx, cancel := xcontext.WithTimeout(xcontext.Background(), DefaultDBAccessTimeout)
	defer cancel()
	ctx = ctx.WithLogger(r.log)
	res, err := r.jobStorage.GetTags(ctx, query.Text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, makeRestErr("error while getting the projects"))
		return
	}

	c.JSON(http.StatusOK, fromStorageTags(res))
}

// getJobs gets the jobs with final report -if it exists- under a given project name as a url parameter
func (r *RouteHandler) getJobs(c *gin.Context) {
	projectName := c.Param("name")
	if err := job.CheckTags([]string{projectName}, false); err != nil {
		c.JSON(http.StatusBadRequest, makeRestErr("bad formatted job tag %v", err))
		return
	}

	ctx, cancel := xcontext.WithTimeout(xcontext.Background(), DefaultDBAccessTimeout)
	defer cancel()
	ctx = ctx.WithLogger(r.log)
	res, err := r.jobStorage.GetJobs(ctx, projectName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, makeRestErr("error while getting the jobs"))
		return
	}

	c.JSON(http.StatusOK, fromStorageJobs(res))
}

func makeRestErr(format string, args ...any) gin.H {
	return gin.H{"status": "err", "msg": fmt.Sprintf(format, args...)}
}

func initRouter(ctx xcontext.Context, rh RouteHandler, middlewares []gin.HandlerFunc) *gin.Engine {

	r := gin.New()
	r.Use(gin.Logger())

	// add the middlewares
	for _, hf := range middlewares {
		r.Use(hf)
	}

	r.GET("/status", rh.status)
	r.POST("/log", rh.addLogs)
	r.GET("/log", rh.getLogs)
	r.GET("/tag", rh.getTags)
	r.GET("/tag/:name/jobs", rh.getJobs)

	// serve the frontend app
	r.StaticFS("/app", FS(false))

	return r
}

func Serve(ctx xcontext.Context, port int, storage storage.Storage, jobStorage adminServerJob.Storage, middlewares []gin.HandlerFunc, tlsConfig *tls.Config) error {
	routeHandler := RouteHandler{
		storage:    storage,
		jobStorage: jobStorage,
		log:        ctx.Logger(),
	}
	router := initRouter(ctx, routeHandler, middlewares)
	server := &http.Server{
		Addr:      fmt.Sprintf(":%d", port),
		Handler:   router,
		TLSConfig: tlsConfig,
	}

	go func() {
		<-ctx.Done()
		// on cancel close the server
		ctx.Debugf("Closing the server")
		if err := server.Close(); err != nil {
			ctx.Errorf("Error closing the server: %v", err)
		}
	}()

	var err error
	if tlsConfig != nil {
		err = server.ListenAndServeTLS("", "")
	} else {
		err = server.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return ctx.Err()
}
