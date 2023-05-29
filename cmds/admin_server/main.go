package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	// this import registers mysql driver for safesql to use
	_ "github.com/go-sql-driver/mysql"
	"github.com/linuxboot/contest/cmds/admin_server/job/rdb"
	"github.com/linuxboot/contest/cmds/admin_server/server"
	mongoStorage "github.com/linuxboot/contest/cmds/admin_server/storage/mongo"
	"github.com/linuxboot/contest/pkg/logging"

	"github.com/facebookincubator/go-belt/tool/logger"
)

var (
	flagSet          *flag.FlagSet
	flagPort         *int
	flagDBURI        *string
	flagContestDBURI *string
	flagTLSCert      *string
	flagTLSKey       *string
	logLevel         = logger.LevelDebug
)

func initFlags(cmd string) {
	flagSet = flag.NewFlagSet(cmd, flag.ContinueOnError)
	flagPort = flagSet.Int("port", 8000, "Port to init the admin server on")
	flagDBURI = flagSet.String("dbURI", "mongodb://localhost:27017", "Database URI")
	flagContestDBURI = flagSet.String("contestdbURI", "contest:contest@tcp(localhost:3306)/contest_integ?parseTime=true", "Contest Database URI")
	flagTLSCert = flagSet.String("tlsCert", "", "Path to the tls cert file")
	flagTLSKey = flagSet.String("tlsKey", "", "Path to the tls key file")
	flagSet.Var(&logLevel, "logLevel", "A log level, possible values: debug, info, warning, error, panic, fatal")

}

// exitWithError prints the `err` to the stdErr
// exits with code `code`
func exitWithError(err error, code int) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(code)
}

func main() {
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT)

	initFlags(os.Args[0])
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		exitWithError(err, 1)
	}

	ctx := logging.WithBelt(context.Background(), logLevel)

	storageCtx, storageCtxCancel := context.WithTimeout(ctx, 10*time.Second)
	defer storageCtxCancel()

	storage, err := mongoStorage.NewMongoStorage(storageCtx, *flagDBURI)
	if err != nil {
		exitWithError(err, 1)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer storage.Close(closeCtx)

	var jobStorage *rdb.Storage
	logging.Debugf(ctx, "init contest db connection %v \n", *flagContestDBURI)
	jobStorage, err = rdb.New(*flagContestDBURI, "mysql")
	if err != nil {
		exitWithError(err, 1)
	}
	defer jobStorage.Close()

	go func() {
		<-sigs
		cancel()
	}()

	var tlsConfig *tls.Config
	if *flagTLSCert != "" && *flagTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(*flagTLSCert, *flagTLSKey)
		if err != nil {
			exitWithError(err, 1)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	if err := server.Serve(ctx, *flagPort, storage, jobStorage, nil, tlsConfig); err != nil {
		exitWithError(fmt.Errorf("server err: %w", err), 1)
	}
}
