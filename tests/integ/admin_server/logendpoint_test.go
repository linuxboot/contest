// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

//go:build integration
// +build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/server"
	mongoStorage "github.com/linuxboot/contest/cmds/admin_server/storage/mongo"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func setupCleanDB(uri string) (*mongo.Client, error) {
	client, err := mongo.NewClient(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *flagOperationTimeout)
	defer cancel()
	err = client.Connect(ctx)
	if err != nil {
		return nil, err
	}

	ctx, cancel = context.WithTimeout(context.Background(), *flagOperationTimeout)
	defer cancel()
	err = client.Database(mongoStorage.DefaultDB).Drop(ctx)

	return client, err
}

func submitLogs(addr string, logs []server.Log) error {
	batchJson, err := json.Marshal(logs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Marshal Err: %v", err)
	}
	requestBody := bytes.NewBuffer(batchJson)
	_, err = http.Post(addr, "application/json", requestBody)

	return err
}

func getAllLogs(t *testing.T, db *mongo.Client) []mongoStorage.Log {
	cur, err := db.Database(mongoStorage.DefaultDB).
		Collection(mongoStorage.DefaultCollection).
		Find(context.Background(), bson.D{{}})
	if err != nil {
		t.Fatal(err)
	}

	var dbLogs []mongoStorage.Log
	for cur.Next(context.Background()) {
		var log mongoStorage.Log
		err := cur.Decode(&log)
		if err != nil {
			t.Fatal(err)
		}
		dbLogs = append(dbLogs, log)
	}
	return dbLogs
}

func TestBatchPush(t *testing.T) {
	var (
		logData  = "test log push"
		loglevel = "info"
		date     = time.Now()
		logsNum  = 100
		logs     []server.Log
	)

	for i := 0; i < logsNum; i++ {
		logs = append(logs, server.Log{
			LogData:  fmt.Sprintf("%s %d", logData, i),
			LogLevel: loglevel,
			Date:     date,
		})
	}

	db, err := setupCleanDB(*flagMongoEndpoint)
	if err != nil {
		t.Fatal(err)
	}

	err = submitLogs(*flagAdminEndpoint, logs)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *flagOperationTimeout)
	defer cancel()
	defer db.Disconnect(ctx)

	dbLogs := getAllLogs(t, db)

	require.Equal(t, len(logs), len(dbLogs))
	assertEqualResults(t, dbLogs, logs)
}
