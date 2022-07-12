//go:build integration
// +build integration

package test

import (
	"flag"
	"time"
)

var (
	flagAdminEndpoint    = flag.String("adminServer", "http://adminserver:8000/log", "admin server log push endpoint")
	flagMongoEndpoint    = flag.String("mongoDBURI", "mongodb://mongostorage:27017", "mongodb URI")
	flagOperationTimeout = flag.Duration("operationTimeout", time.Duration(10*time.Second), "operation timeout duration")
)
