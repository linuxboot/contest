//go:build integration_admin
// +build integration_admin

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/go-safeweb/safesql"
	"github.com/linuxboot/contest/cmds/admin_server/server"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/stretchr/testify/require"
)

const (
	insertJobStmt    = "insert into jobs (name, descriptor, extended_descriptor, requestor, server_id, request_time) values (?, ?, ?, ?, ?, ?)"
	insertJobTagStmt = "insert into job_tags (job_id, tag) values (?, ?)"
)

func InitCleanDBConn(uri string) (*safesql.DB, error) {
	db, err := safesql.Open("mysql", uri)
	if err != nil {
		return nil, fmt.Errorf("error while opening db conn: %w", err)
	}

	if _, err := db.Exec(safesql.New("TRUNCATE TABLE job_tags")); err != nil {
		return nil, fmt.Errorf("error while cleaning table job_tags: %w", err)
	}

	if _, err := db.Exec(safesql.New("TRUNCATE TABLE jobs")); err != nil {
		return nil, fmt.Errorf("error while cleaning table jobs: %w", err)
	}
	return &db, nil
}

func AddJobsWithTags(db *safesql.DB, jobNames []string, tags []string) ([]types.JobID, error) {
	var ids []types.JobID
	for i, name := range jobNames {
		res, err := db.Exec(
			safesql.New(insertJobStmt),
			name,
			fmt.Sprintf("descriptor placeholder %d", i),
			fmt.Sprintf("extend descriptor placeholder %d", i),
			fmt.Sprintf("requestor %d", i),
			fmt.Sprintf("server id %d", i),
			time.Now(),
		)
		if err != nil {
			return nil, fmt.Errorf("error while inserting tmp jobs for testing: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("error while getting the last inserted id: %w", err)
		}
		ids = append(ids, types.JobID(id))
	}

	for _, tag := range tags {
		for _, id := range ids {
			if _, err := db.Exec(safesql.New(insertJobTagStmt), id, tag); err != nil {
				return nil, fmt.Errorf("error while inserting job tag: %w", err)
			}
		}
	}

	return ids, nil
}

func getJobs(addr, searchTag string) ([]types.JobID, int, error) {
	url, err := url.ParseRequestURI(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("error while parsing the url(%v): %w", addr, err)
	}
	url.Path = path.Join(url.Path, fmt.Sprint(searchTag), "jobs")

	res, err := http.Get(url.String())
	if err != nil {
		return nil, 0, fmt.Errorf("error while Sending GET request: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, nil
	}

	var jobs []server.Job
	err = json.NewDecoder(res.Body).Decode(&jobs)
	if err != nil {
		return nil, 0, fmt.Errorf("error while parsing request body: %w", err)
	}

	var jobIDs []types.JobID
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.JobID)
	}
	return jobIDs, res.StatusCode, nil
}

func getTags(addr, searchTag string) ([]server.Tag, int, error) {
	url, err := url.Parse(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("error while parsing the url(%v): %w", addr, err)
	}

	q := url.Query()
	q.Set("text", searchTag)
	url.RawQuery = q.Encode()

	res, err := http.Get(url.String())
	if err != nil {
		return nil, 0, fmt.Errorf("error while Sending GET request: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, nil
	}

	var projects []server.Tag
	err = json.NewDecoder(res.Body).Decode(&projects)
	if err != nil {
		return nil, 0, fmt.Errorf("error while parsing request body: %w", err)
	}

	return projects, res.StatusCode, nil
}

func TestGetJobs(t *testing.T) {
	testCases := []struct {
		testName  string
		names     []string
		tags      []string
		searchTag string
	}{
		{
			testName:  "base-case",
			names:     []string{"job1", "job2", "job3"},
			tags:      []string{"tag1"},
			searchTag: "tag1",
		},
		{
			testName:  "not-existing-tag",
			names:     []string{},
			tags:      []string{},
			searchTag: "AnyThing",
		},
	}

	db, err := InitCleanDBConn(*flagContestDBURI)
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(tt *testing.T) {
			jobIDs, err := AddJobsWithTags(db, testCase.names, testCase.tags)
			if err != nil {
				tt.Fatal(err)
			}

			serverJobIDs, statusCode, err := getJobs(*flagAdminProjectEndpoint, testCase.searchTag)
			if err != nil {
				t.Fatal(err)
			}

			require.Equal(t, statusCode, http.StatusOK)
			require.Equal(t, len(jobIDs), len(serverJobIDs))
			require.Equal(t, jobIDs, serverJobIDs)
		})
	}

	t.Run("bad-request", func(tt *testing.T) {
		_, statusCode, err := getJobs(*flagAdminProjectEndpoint, "*_?tag")
		if err != nil {
			tt.Fatal(err)
		}
		require.Equal(t, http.StatusBadRequest, statusCode)
	})
}

func TestGetTags(t *testing.T) {
	testCases := []struct {
		testName  string
		names     []string
		tags      []string
		searchTag string
		expected  []server.Tag
	}{
		{
			testName:  "base-case",
			names:     []string{"job1", "job2", "job3"},
			tags:      []string{"tag1", "tag2", "not1"},
			searchTag: "ta",
			expected: []server.Tag{
				{Name: "tag1", JobsCount: 3},
				{Name: "tag2", JobsCount: 3},
			},
		},
		{
			testName:  "not-existing-tag",
			names:     []string{},
			searchTag: "AnyThing",
			expected:  []server.Tag{},
		},
	}

	db, err := InitCleanDBConn(*flagContestDBURI)
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(tt *testing.T) {
			_, err := AddJobsWithTags(db, testCase.names, testCase.tags)
			if err != nil {
				tt.Fatal(err)
			}

			serverProjects, statusCode, err := getTags(*flagAdminProjectEndpoint, testCase.searchTag)
			if err != nil {
				t.Fatal(err)
			}

			require.Equal(t, statusCode, http.StatusOK)
			require.Equal(t, len(testCase.expected), len(serverProjects))
			require.Equal(t, testCase.expected, serverProjects)
		})
	}
}
