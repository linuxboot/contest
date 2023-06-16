// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package server

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/benbjohnson/clock"

	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/config"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/jobmanager"
	"github.com/linuxboot/contest/pkg/loggerhook"
	"github.com/linuxboot/contest/pkg/logging"
	"github.com/linuxboot/contest/pkg/pluginregistry"
	"github.com/linuxboot/contest/pkg/signaling"
	"github.com/linuxboot/contest/pkg/signals"
	"github.com/linuxboot/contest/pkg/storage"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/userfunctions/donothing"
	"github.com/linuxboot/contest/pkg/userfunctions/ocp"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/linuxboot/contest/plugins/storage/memory"
	"github.com/linuxboot/contest/plugins/storage/rdbms"
	"github.com/linuxboot/contest/plugins/targetlocker/dblocker"
	"github.com/linuxboot/contest/plugins/targetlocker/inmemory"

	// the listener plugin
	"github.com/linuxboot/contest/plugins/listeners/httplistener"
)

type flags struct {
	DBURI              string
	ListenAddr         string
	ServerID           string
	ProcessTimeout     time.Duration
	TargetLocker       string
	InstanceTag        string
	PauseTimeout       time.Duration
	ResumeJobs         bool
	TargetLockDuration time.Duration
	// http logger parameters
	AdminServerAddr         string
	HttpLoggerBufferSize    int
	HttpLoggerMaxBatchSize  int
	HttpLoggerMaxBatchCount int
	HttpLoggerBatchSendFreq time.Duration
	HttpLoggerTimeout       time.Duration
	LogLevel                logger.Level
}

func parseFlags(cmd string, args ...string) (*flags, error) {
	f := &flags{
		LogLevel: logger.LevelDebug,
	}
	flagSet := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flagSet.StringVar(&f.DBURI, "dbURI", config.DefaultDBURI, "Database URI")
	flagSet.StringVar(&f.ListenAddr, "listenAddr", ":8080", "Listen address and port")
	flagSet.StringVar(&f.AdminServerAddr, "adminServerAddr", "", "Addr of the admin server to connect to")
	flagSet.IntVar(&f.HttpLoggerBufferSize, "loggerBufferSize", loggerhook.DefaultBufferSize, "buffer size for the http logger hook")
	flagSet.IntVar(&f.HttpLoggerMaxBatchSize, "loggerMaxBatchSize", loggerhook.DefaultMaxBatchSize, "max size (in bytes) of a logs batch to be sent if it reaches/exceeds it")
	flagSet.IntVar(&f.HttpLoggerMaxBatchCount, "loggerMaxBatchCount", loggerhook.DefaultMaxBatchCount, "max count of logs in a batch")
	flagSet.DurationVar(&f.HttpLoggerBatchSendFreq, "loggerBatchSendFreq", loggerhook.DefaultBatchSendFreq, "duration that defines the batch sending freq")
	flagSet.DurationVar(&f.HttpLoggerTimeout, "loggerTimeout", loggerhook.DefaultLogTimeout, "logs send timeout")
	flagSet.StringVar(&f.ServerID, "serverID", "", "Set a static server ID, e.g. the host name or another unique identifier. If unset, will use the listener's default")
	flagSet.DurationVar(&f.ProcessTimeout, "processTimeout", api.DefaultEventTimeout, "API request processing timeout")
	flagSet.StringVar(&f.TargetLocker, "targetLocker", "auto", "Target locker implementation to use, \"auto\" follows DBURI setting")
	flagSet.StringVar(&f.InstanceTag, "instanceTag", "", "A tag for this instance. Server will only operate on jobs with this tag and will add this tag to the jobs it creates.")
	flagSet.Var(&f.LogLevel, "logLevel", "A log level, possible values: debug, info, warning, error, panic, fatal")
	flagSet.DurationVar(&f.PauseTimeout, "pauseTimeout", 0, "SIGINT/SIGTERM shutdown timeout (seconds), after which pause will be escalated to cancellaton; -1 - no escalation, 0 - do not pause, cancel immediately")
	flagSet.BoolVar(&f.ResumeJobs, "resumeJobs", false, "Attempt to resume paused jobs")
	flagSet.DurationVar(&f.TargetLockDuration, "targetLockDuration", config.DefaultTargetLockDuration,
		"The amount of time target lock is extended by while the job is running. "+
			"This is the maximum amount of time a job can stay paused safely.")

	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}

	return f, nil
}

var userFunctions = []map[string]interface{}{
	ocp.Load(),
	donothing.Load(),
}

var funcInitOnce sync.Once

// PluginConfig contains the configuration for all the plugins desired in a
// server instance.
type PluginConfig struct {
	TargetManagerLoaders []target.TargetManagerLoader
	TestFetcherLoaders   []test.TestFetcherLoader
	TestStepLoaders      []test.TestStepLoader
	ReporterLoaders      []job.ReporterLoader
}

func registerPlugins(pluginRegistry *pluginregistry.PluginRegistry, pluginConfig *PluginConfig) error {
	// register targetmanagers
	for _, loader := range pluginConfig.TargetManagerLoaders {
		name, factory := loader()
		if err := pluginRegistry.RegisterTargetManager(name, factory); err != nil {
			return err
		}
	}

	// register testfetchers
	for _, loader := range pluginConfig.TestFetcherLoaders {
		name, factory := loader()
		if err := pluginRegistry.RegisterTestFetcher(name, factory); err != nil {
			return err
		}
	}

	// register teststeps
	for _, loader := range pluginConfig.TestStepLoaders {
		name, factory, events := loader()
		if err := pluginRegistry.RegisterTestStep(name, factory, events); err != nil {
			return err
		}
	}

	// register reporters
	for _, loader := range pluginConfig.ReporterLoaders {
		name, factory := loader()
		if err := pluginRegistry.RegisterReporter(name, factory); err != nil {
			return err
		}
	}

	// TODO make listener also configurable from contest-generator.
	// also register user functions here. TODO: make them configurable from contest-generator.
	errCh := make(chan error, 1)
	funcInitOnce.Do(func() {
		for _, userFunction := range userFunctions {
			for name, fn := range userFunction {
				if err := test.RegisterFunction(name, fn); err != nil {
					errCh <- fmt.Errorf("failed to load user function '%s': %w", name, err)
					return
				}
			}
		}
	})

	return nil
}

// Main is the main function that executes the ConTest server.
func Main(pluginConfig *PluginConfig, cmd string, args []string, sigs <-chan os.Signal) error {
	flags, err := parseFlags(cmd, args...)
	if err != nil {
		return fmt.Errorf("unable to parse the flags: %w", err)
	}

	clk := clock.New()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = logging.WithBelt(ctx, flags.LogLevel)

	if flags.AdminServerAddr != "" {
		httpHook, err := loggerhook.NewHttpHook(loggerhook.Config{
			Addr:          flags.AdminServerAddr,
			BufferSize:    flags.HttpLoggerBufferSize,
			MaxBatchSize:  flags.HttpLoggerMaxBatchSize,
			MaxBatchCount: flags.HttpLoggerMaxBatchCount,
			BatchSendFreq: flags.HttpLoggerBatchSendFreq,
			LogTimeout:    flags.HttpLoggerTimeout,
		})
		errmon.ObserveErrorCtx(ctx, err)
		if httpHook != nil {
			ctx = logger.CtxWithLogger(ctx, logger.FromCtx(ctx).WithHooks(httpHook))
		}
	}

	ctx, pause := signaling.WithSignal(ctx, signals.Paused)
	log := logger.FromCtx(ctx)
	defer cancel()

	// Let's store storage engine in context
	storageEngineVault := storage.NewSimpleEngineVault()

	pluginRegistry := pluginregistry.NewPluginRegistry(ctx)
	if err := registerPlugins(pluginRegistry, pluginConfig); err != nil {
		return fmt.Errorf("failed to register plugins: %w", err)
	}

	var storageInstances []storage.Storage
	defer func() {
		for i, s := range storageInstances {
			if err := s.Close(); err != nil {
				log.Errorf("Failed to close storage %d: %v", i, err)
			}
		}
	}()

	// primary storage initialization
	if flags.DBURI != "" {
		primaryDBURI := flags.DBURI
		log.Infof("Using database URI for primary storage: %s", primaryDBURI)
		s, err := rdbms.New(primaryDBURI)
		if err != nil {
			log.Fatalf("Could not initialize database: %v", err)
		}
		storageInstances = append(storageInstances, s)
		if err := storageEngineVault.StoreEngine(s, storage.SyncEngine); err != nil {
			log.Fatalf("Could not set storage: %v", err)
		}

		dbVerPrim, err := s.Version()
		if err != nil {
			log.Warnf("Could not determine storage version: %v", err)
		} else {
			log.Infof("Storage version: %d", dbVerPrim)
		}

		// replica storage initialization
		// pointing to main database for now but can be used to point to replica
		replicaDBURI := flags.DBURI
		log.Infof("Using database URI for replica storage: %s", replicaDBURI)
		r, err := rdbms.New(replicaDBURI)
		if err != nil {
			log.Fatalf("Could not initialize replica database: %v", err)
		}
		storageInstances = append(storageInstances, r)
		if err := storageEngineVault.StoreEngine(s, storage.AsyncEngine); err != nil {
			log.Fatalf("Could not set replica storage: %v", err)
		}

		dbVerRepl, err := r.Version()
		if err != nil {
			log.Warnf("Could not determine storage version: %v", err)
		} else {
			log.Infof("Storage version: %d", dbVerRepl)
		}

		if dbVerPrim != dbVerRepl {
			log.Fatalf("Primary and Replica DB Versions are different: %v and %v", dbVerPrim, dbVerRepl)
		}
	} else {
		log.Warnf("Using in-memory storage")
		if ms, err := memory.New(); err == nil {
			storageInstances = append(storageInstances, ms)
			if err := storageEngineVault.StoreEngine(ms, storage.SyncEngine); err != nil {
				log.Fatalf("Could not set storage: %v", err)
			}
			if err := storageEngineVault.StoreEngine(ms, storage.AsyncEngine); err != nil {
				log.Fatalf("Could not set replica storage: %v", err)
			}
		} else {
			log.Fatalf("Could not create storage: %v", err)
		}
	}

	// set Locker engine
	if flags.TargetLocker == "auto" {
		if flags.DBURI != "" {
			flags.TargetLocker = dblocker.Name
		} else {
			flags.TargetLocker = inmemory.Name
		}
		log.Infof("Locker engine set to auto, using %s", flags.TargetLocker)
	}
	switch flags.TargetLocker {
	case inmemory.Name:
		target.SetLocker(inmemory.New(clk))
	case dblocker.Name:
		if l, err := dblocker.New(flags.DBURI, dblocker.WithClock(clk)); err == nil {
			target.SetLocker(l)
		} else {
			log.Fatalf("Failed to create locker %q: %v", flags.TargetLocker, err)
		}
	default:
		log.Fatalf("Invalid target locker name %q", flags.TargetLocker)
	}

	// spawn JobManager
	listener := httplistener.New(flags.ListenAddr)

	opts := []jobmanager.Option{
		jobmanager.APIOption(api.OptionEventTimeout(flags.ProcessTimeout)),
	}
	if flags.ServerID != "" {
		opts = append(opts, jobmanager.APIOption(api.OptionServerID(flags.ServerID)))
	}
	if flags.InstanceTag != "" {
		opts = append(opts, jobmanager.OptionInstanceTag(flags.InstanceTag))
	}
	if flags.TargetLockDuration != 0 {
		opts = append(opts, jobmanager.OptionTargetLockDuration(flags.TargetLockDuration))
	}

	jm, err := jobmanager.New(listener, pluginRegistry, storageEngineVault, opts...)
	if err != nil {
		log.Fatalf("%v", err)
	}

	pauseTimeout := flags.PauseTimeout

	go func() {
		intLevel := 0
		// cancel immediately if pauseTimeout is zero
		if flags.PauseTimeout == 0 {
			intLevel = 1
		}
		for {
			sig, ok := <-sigs
			if !ok {
				return
			}
			switch sig {
			case syscall.SIGUSR1:
				// Gentle shutdown: stop accepting requests, drain without asserting pause signal.
				jm.StopAPI()
			case syscall.SIGINT:
				fallthrough
			case syscall.SIGTERM:
				// First signal - pause and drain, second - cancel.
				jm.StopAPI()
				if intLevel == 0 {
					log.Infof("Signal %q, pausing jobs", sig)
					pause()
					if flags.PauseTimeout > 0 {
						go func() {
							select {
							case <-ctx.Done():
							case <-time.After(pauseTimeout):
								log.Errorf("Timed out waiting for jobs to pause, canceling")
								cancel()
							}
						}()
					}
					intLevel++
				} else {
					log.Infof("Signal %q, canceling", sig)
					cancel()
				}
			}
		}
	}()

	err = jm.Run(ctx, flags.ResumeJobs)

	target.SetLocker(nil)

	log.Infof("Exiting, %v", err)

	return err
}
