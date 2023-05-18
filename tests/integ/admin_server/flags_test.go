//go:build integration_admin
// +build integration_admin

package test

import (
	"flag"
	"time"
)

var (
	flagAdminEndpoint        = flag.String("adminServer", "http://adminserver:8000/log", "admin server log push endpoint")
	flagAdminProjectEndpoint = flag.String("ProjectEndpoint", "http://adminserver:8000/tag", "admin server project query endpoint")
	flagMongoEndpoint        = flag.String("mongoDBURI", "mongodb://mongostorage:27017", "mongodb URI")
	flagContestDBURI         = flag.String("ContestDBURI", "contest:contest@tcp(dbstorage:3306)/contest_integ?parseTime=true", "contest db URI")
	flagOperationTimeout     = flag.Duration("operationTimeout", time.Duration(10*time.Second), "operation timeout duration")
)
