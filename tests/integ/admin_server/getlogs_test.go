// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build integration
// +build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/server"
	"github.com/linuxboot/contest/cmds/admin_server/storage"
	mongoStorage "github.com/linuxboot/contest/cmds/admin_server/storage/mongo"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var logLevels = []string{"panic", "fatal", "error", "warning", "info", "debug"}

func randomText() string {
	rand.Seed(time.Now().UnixNano())

	ran_str := make([]byte, 10)

	for i := 0; i < 10; i++ {
		ran_str[i] = byte(65 + rand.Intn(25))
	}

	return string(ran_str)
}

// generateRandomLogs generate `count` logs and inserts them into `db`
func generateRandomLogs(db *mongo.Client, count int) error {
	collection := db.Database(mongoStorage.DefaultDB).Collection(mongoStorage.DefaultCollection)

	var (
		jobID = 1
		date  = time.Now()
		logs  []interface{}
	)
	for i := 0; i < count; i++ {
		level := logLevels[rand.Intn(len(logLevels))]
		data := randomText()

		logs = append(logs, mongoStorage.Log{
			JobID:    uint64(jobID),
			LogLevel: level,
			LogData:  data,
			Date:     date,
		})
		if rand.Intn(3) == 0 {
			jobID += 1
		}
		if rand.Intn(2) == 0 {
			date = date.Add(time.Second)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *flagOperationTimeout)
	defer cancel()
	inserted, err := collection.InsertMany(ctx, logs)
	if err != nil {
		return err
	}
	fmt.Printf("%d inserted: %+v \n", len(inserted.InsertedIDs), inserted)

	return nil
}

func queryLogsEndpoint(query server.Query, retrieveAll bool) (*storage.Result, int, error) {
	u, err := url.Parse(*flagAdminEndpoint)
	if err != nil {
		return nil, 0, err
	}

	var (
		result           storage.Result
		statusCode       int
		currentPage      uint = 0
		continueRetrieve bool = true
	)

	if query.Page != nil {
		currentPage = *query.Page
	}

	for continueRetrieve {
		var tmp storage.Result

		q := u.Query()
		if query.JobID != nil {
			q.Set("job_id", strconv.FormatUint(*query.JobID, 10))
		}
		if query.Text != nil {
			q.Set("text", *query.Text)
		}
		if query.StartDate != nil {
			q.Set("start_date", query.StartDate.Format(storage.DefaultTimestampFormat))
		}
		if query.EndDate != nil {
			q.Set("end_date", query.EndDate.Format(storage.DefaultTimestampFormat))
		}
		if query.LogLevel != nil {
			q.Set("log_level", *query.LogLevel)
		}
		if query.Page != nil {
			q.Set("page", strconv.FormatUint(uint64(currentPage), 10))
		}
		if query.PageSize != nil {
			q.Set("page_size", strconv.FormatUint(uint64(*query.PageSize), 10))
		}

		u.RawQuery = q.Encode()
		res, err := http.Get(u.String())
		if err != nil {
			return nil, 0, err
		}
		statusCode = res.StatusCode

		err = json.NewDecoder(res.Body).Decode(&tmp)
		if err != nil {
			return nil, 0, err
		}

		result.Logs = append(result.Logs, tmp.Logs...)
		result.Count = tmp.Count

		currentPage += 1
		continueRetrieve = retrieveAll && (int64(result.Count)-int64((currentPage)*(*query.PageSize)) > 0)
		fmt.Print(result.Count, currentPage, query.PageSize)
	}

	return &result, statusCode, nil
}

// assertEqualResults checks that the db and api results
// have the same length and contains the same logdata
func assertEqualResults(t *testing.T, expected []storage.Log, actual []storage.Log) {
	// check length
	if len(expected) != len(actual) {
		t.Fatal(fmt.Errorf("api and db results dose not have the same length: %d != %d", len(expected), len(actual)))
		return
	}

	// it will be used to index the logs by there data
	// as its randomly generated, so it should be unique
	table := make(map[string]bool)
	// populate the tabel
	for _, log := range expected {
		table[log.LogData] = true
	}

	// check equality
	for _, log := range actual {
		if _, ok := table[log.LogData]; !ok {
			t.Fatal(fmt.Errorf("LogData (%s) dose not exist", log.LogData))
			return
		}
	}
}

func TestLogQuery(t *testing.T) {
	fmt.Print(*flagMongoEndpoint)
	db, err := setupCleanDB(*flagMongoEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *flagOperationTimeout)
	defer cancel()
	defer db.Disconnect(ctx)

	collection := db.Database(mongoStorage.DefaultDB).Collection(mongoStorage.DefaultCollection)

	err = generateRandomLogs(db, 120)
	if err != nil {
		t.Fatal(err)
	}

	/*
		This range should intersect with logs dates.
		we started generating logs with time.Now and then added 1s with probability 0.5.
		Given that for the test the database is local so the insert will not take much,
		generating more than 4 logs should intersect with this date range
	*/
	var (
		startDate              = time.Now().Add(-2 * time.Second)
		endDate                = time.Now().Add(1 * time.Second)
		searchText      string = "a"
		emptySearchText string = ""
		jobID           uint64 = 3
		page            uint   = 0
		pageSize        uint   = 20
		logLevel        string = "info"
		logLevels       string = "info,debug"
	)

	cases := []struct {
		name     string
		apiQuery server.Query
		dbQuery  bson.M
	}{
		{
			name: "jobID-search",
			apiQuery: server.Query{
				JobID:    &jobID,
				Page:     &page,
				PageSize: &pageSize,
			},
			dbQuery: bson.M{
				"jobid": jobID,
			},
		},
		{
			name: "text-search",
			apiQuery: server.Query{
				Text:     &searchText,
				Page:     &page,
				PageSize: &pageSize,
			},
			dbQuery: bson.M{
				"log_data": bson.M{"$regex": primitive.Regex{Pattern: "a", Options: "ig"}},
			},
		},
		{
			name: "logLevel-search",
			apiQuery: server.Query{
				Text:     &emptySearchText,
				LogLevel: &logLevel,
				Page:     &page,
				PageSize: &pageSize,
			},
			dbQuery: bson.M{
				"log_level": "info",
			},
		},
		{
			name: "multiple-log-levels",
			apiQuery: server.Query{
				Text:     &emptySearchText,
				LogLevel: &logLevels,
				Page:     &page,
				PageSize: &pageSize,
			},
			dbQuery: bson.M{
				"log_level": bson.M{"$in": []string{"info", "debug"}},
			},
		},
		{
			name: "start-date",
			apiQuery: server.Query{
				Text:      &emptySearchText,
				StartDate: &startDate,
				Page:      &page,
				PageSize:  &pageSize,
			},
			dbQuery: bson.M{
				"date": bson.M{
					"$gte": primitive.NewDateTimeFromTime(startDate),
				},
			},
		},
		{
			name: "end-date",
			apiQuery: server.Query{
				Text:     &emptySearchText,
				EndDate:  &endDate,
				Page:     &page,
				PageSize: &pageSize,
			},
			dbQuery: bson.M{
				"date": bson.M{
					"$lte": primitive.NewDateTimeFromTime(endDate),
				},
			},
		},
		{
			name: "date-range",
			apiQuery: server.Query{
				Text:      &emptySearchText,
				EndDate:   &endDate,
				StartDate: &startDate,
				Page:      &page,
				PageSize:  &pageSize,
			},
			dbQuery: bson.M{
				"date": bson.M{
					"$lte": primitive.NewDateTimeFromTime(endDate),
					"$gte": primitive.NewDateTimeFromTime(startDate),
				},
			},
		},
		{
			name: "combined-query",
			apiQuery: server.Query{
				Text:      &searchText,
				StartDate: &startDate,
				EndDate:   &endDate,
				LogLevel:  &logLevel,
				Page:      &page,
				PageSize:  &pageSize,
			},
			dbQuery: bson.M{
				"log_data": bson.M{"$regex": primitive.Regex{Pattern: "a", Options: "ig"}},
				"date": bson.M{
					"$lte": primitive.NewDateTimeFromTime(endDate),
					"$gte": primitive.NewDateTimeFromTime(startDate),
				},
				"log_level": "info",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(tt *testing.T) {
			// get logs from endpoint
			result, _, err := queryLogsEndpoint(testCase.apiQuery, true)
			if err != nil {
				tt.Fatal(err)
			}
			// get logs from database
			var dbLogs []storage.Log
			ctx, cancel := context.WithTimeout(context.Background(), *flagOperationTimeout)
			defer cancel()
			cur, err := collection.Find(ctx, testCase.dbQuery)
			if err != nil {
				tt.Fatal(err)
			}

			ctx, cancel = context.WithTimeout(context.Background(), *flagOperationTimeout)
			defer cancel()
			err = cur.All(ctx, &dbLogs)
			if err != nil {
				tt.Fatal(err)
			}

			assertEqualResults(t, dbLogs, result.Logs)

		})
	}

	t.Run("no-paging", func(t *testing.T) {
		// get logs from endpoint
		res, statusCode, err := queryLogsEndpoint(server.Query{
			Text:     &searchText,
			PageSize: &pageSize,
		}, false)
		if err != nil {
			t.Fatal(err)
		}

		require.Equal(t, http.StatusOK, statusCode)
		require.Equal(t, 0, int(res.Page))
	})

	t.Run("no-filters", func(t *testing.T) {
		// get logs from endpoint
		res, statusCode, err := queryLogsEndpoint(server.Query{}, false)
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), *flagOperationTimeout)
		defer cancel()
		count, err := collection.CountDocuments(ctx, bson.D{})
		if err != nil {
			t.Fatal(err)
		}

		require.Equal(t, http.StatusOK, statusCode)
		require.Equal(t, count, int64(res.Count))
		// the length of the logs should be the maxPageSize (default page size)
		// as the len(all logs) is 120 and the there is no pageSize in the query
		require.Equal(t, int(server.MaxPageSize), len(res.Logs))
	})
}
