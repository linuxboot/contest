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
	"flag"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/storage"
	mongoAdapter "github.com/linuxboot/contest/cmds/admin_server/storage/mongo"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	flagAdminEndpoint = flag.String("adminServer", "http://adminserver:8000/log", "admin server log push endpoint")
	flagMongoEndpoint = flag.String("mongoDBURI", "mongodb://mongostorage:27017", "mongodb URI")
)

func setupCleanDB(uri string) (*mongo.Client, error) {
	client, err := mongo.NewClient(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = client.Connect(ctx)
	if err != nil {
		return nil, err
	}

	err = client.Database(mongoAdapter.DefaultDB).Drop(context.Background())

	return client, err
}

func submitLog(addr string, log storage.Log) error {
	logJson, err := json.Marshal(log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Marshal Err: %v", err)
	}
	requestBody := bytes.NewBuffer(logJson)
	_, err = http.Post(addr, "application/json", requestBody)

	return err
}

func getAllLogs(t *testing.T, db *mongo.Client) []mongoAdapter.Log {
	cur, err := db.Database(mongoAdapter.DefaultDB).
		Collection(mongoAdapter.DefaultCollection).
		Find(context.Background(), bson.D{{}})
	if err != nil {
		t.Fatal(err)
	}

	var dbLogs []mongoAdapter.Log
	for cur.Next(context.Background()) {
		var log mongoAdapter.Log
		err := cur.Decode(&log)
		if err != nil {
			t.Fatal(err)
		}
		dbLogs = append(dbLogs, log)
	}
	return dbLogs
}

func TestLogPush(t *testing.T) {
	var (
		logData  = "test log push"
		loglevel = "info"
		logDate  = time.Now()
	)
	log := storage.Log{
		LogData:  logData,
		LogLevel: loglevel,
		Date:     logDate,
	}

	db, err := setupCleanDB(*flagMongoEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Disconnect(context.Background())

	err = submitLog(*flagAdminEndpoint, log)
	if err != nil {
		t.Fatal(err)
	}

	dbLogs := getAllLogs(t, db)

	require.Equal(t, 1, len(dbLogs))
	require.Equal(t, logData, dbLogs[0].LogData)
	require.Equal(t, loglevel, dbLogs[0].LogLevel)
}
