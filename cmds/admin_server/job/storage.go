package job

import (
	"time"

	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// DB wraps a job database
type Storage interface {
	GetTags(ctx xcontext.Context, tagPattern string) ([]Tag, error)
	GetJobs(ctx xcontext.Context, projectName string) ([]Job, error)
}

// Tag contains metadata about jobs under a given tag
type Tag struct {
	Name string
	// number of jobs with under this tag
	JobsCount uint
}

// Job contains final report data about that job_id
type Job struct {
	JobID types.JobID
	// fields for the final report of the job if it exists.
	ReporterName *string
	ReportTime   *time.Time
	Success      *bool
	Data         *string
}
