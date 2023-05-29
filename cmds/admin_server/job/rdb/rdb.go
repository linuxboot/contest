package rdb

import (
	"context"
	"fmt"

	"github.com/google/go-safeweb/safesql"
	adminServerJob "github.com/linuxboot/contest/cmds/admin_server/job"
	"github.com/linuxboot/contest/pkg/logging"
)

var (
	tagsStmt = safesql.New(`SELECT tag, COUNT(tag) FROM job_tags WHERE tag REGEXP CONCAT('.*',?,'.*') GROUP BY tag`)
	jobStmt  = safesql.New(`SELECT t.job_id, r.reporter_name, r.report_time, r.data FROM job_tags t LEFT JOIN final_reports r ON t.job_id = r.job_id WHERE t.tag = ?`)
)

// SQL defines a struct that wraps a db connection to job sql database
type Storage struct {
	db *safesql.DB
}

func New(dbURI, driveName string) (*Storage, error) {

	if driveName == "" {
		driveName = "mysql"
	}

	db, err := safesql.Open(driveName, dbURI)
	if err != nil {
		return nil, fmt.Errorf("error while initializing the db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("error while connecting to the db: %w", err)
	}

	return &Storage{
		db: &db,
	}, nil
}

// GetTags returns tags that has a tag matches tagPattern
func (r *Storage) GetTags(ctx context.Context, tagPattern string) ([]adminServerJob.Tag, error) {
	var resultErr error
	res := []adminServerJob.Tag{}
	doneChan := make(chan struct{})

	go func(doneChan chan<- struct{}) {
		defer func() {
			doneChan <- struct{}{}
		}()

		rows, err := r.db.Query(tagsStmt, tagPattern)
		if err != nil {
			resultErr = fmt.Errorf("error while listing projects with tag like %s (sql: %q): %w", tagPattern, tagsStmt, err)
			return
		}
		defer func() {
			err = rows.Close()
			if err != nil {
				logging.Errorf(ctx, "error while closing the rows reader: %w", err)
			}
		}()

		for rows.Next() {
			if rows.Err() != nil {
				resultErr = fmt.Errorf("error while reading the rows from query result: %w", err)
				return
			}

			var tag adminServerJob.Tag
			if err := rows.Scan(&tag.Name, &tag.JobsCount); err != nil {
				resultErr = fmt.Errorf("error while scaning query result (sql: %q): %w", tagsStmt, err)
				return
			}
			res = append(res, tag)
		}
	}(doneChan)

	for {
		select {
		case <-doneChan:
			return res, resultErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// GetJobs returns jobs with final report if exists that are under a given tagName
func (r *Storage) GetJobs(ctx context.Context, tagName string) ([]adminServerJob.Job, error) {
	var resultErr error
	res := []adminServerJob.Job{}
	doneChan := make(chan struct{})

	go func(doneChan chan<- struct{}) {
		defer func() {
			doneChan <- struct{}{}
		}()

		rows, err := r.db.Query(jobStmt, tagName)
		if err != nil {
			resultErr = fmt.Errorf("error while listing jobs with tag %s (sql: %q): %w", tagName, jobStmt, err)
			return
		}
		defer func() {
			err = rows.Close()
			if err != nil {
				logging.Errorf(ctx, "error while closing the rows reader: %w", err)
			}
		}()

		for rows.Next() {
			if rows.Err() != nil {
				resultErr = fmt.Errorf("error while reading the rows from query result: %w", err)
				return
			}

			var job adminServerJob.Job
			if err := rows.Scan(&job.JobID, &job.ReporterName, &job.ReportTime, &job.Data); err != nil {
				resultErr = fmt.Errorf("error while scaning the job (sql: %q): %w", jobStmt, err)
				return
			}
			res = append(res, job)
		}
	}(doneChan)

	for {
		select {
		case <-doneChan:
			return res, resultErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (r *Storage) Close() error {
	return r.db.Close()
}
