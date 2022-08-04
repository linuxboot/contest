package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linuxboot/contest/cmds/admin_server/server"
	mongoStorage "github.com/linuxboot/contest/cmds/admin_server/storage/mongo"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/pkg/xcontext/bundles/logrusctx"
	"github.com/linuxboot/contest/pkg/xcontext/logger"
)

var (
	flagSet      *flag.FlagSet
	flagPort     *int
	flagDBURI    *string
	flagTLSCert  *string
	flagTLSKey   *string
	flagLogLevel *string
)

func initFlags(cmd string) {
	flagSet = flag.NewFlagSet(cmd, flag.ContinueOnError)
	flagPort = flagSet.Int("port", 8000, "Port to init the admin server on")
	flagDBURI = flagSet.String("dbURI", "mongodb://localhost:27017", "Database URI")
	flagTLSCert = flagSet.String("tlsCert", "", "Path to the tls cert file")
	flagTLSKey = flagSet.String("tlsKey", "", "Path to the tls key file")
	flagLogLevel = flagSet.String("logLevel", "debug", "A log level, possible values: debug, info, warning, error, panic, fatal")

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

	logLevel, err := logger.ParseLogLevel(*flagLogLevel)
	if err != nil {
		exitWithError(err, 1)
	}

	ctx, cancel := logrusctx.NewContext(logLevel, logging.DefaultOptions()...)
	defer cancel()

	storageCtx, cancel := xcontext.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	storage, err := mongoStorage.NewMongoStorage(storageCtx, *flagDBURI)
	if err != nil {
		exitWithError(err, 1)
	}
	closeCtx, cancel := xcontext.WithTimeout(xcontext.Background(), 10*time.Second)
	defer cancel()
	defer storage.Close(closeCtx)

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

	if err := server.Serve(ctx, *flagPort, storage, nil, tlsConfig); err != nil {
		exitWithError(fmt.Errorf("server err: %w", err), 1)
	}
}
