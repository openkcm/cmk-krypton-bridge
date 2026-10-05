package main

import (
	"context"
	"database/sql"
	"flag"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"

	dbpkg "github.com/openkcm/cmk-krypton-bridge/db"
	eventprocessor "github.com/openkcm/cmk-krypton-bridge/internal/event-processor"
	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/registryclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/server"
	"github.com/openkcm/cmk-krypton-bridge/internal/worker"
	"github.com/openkcm/cmk-krypton-bridge/pkg/config"
	"github.com/openkcm/cmk-krypton-bridge/pkg/logger"
)

func main() {
	cfgFile := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgFile)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	isDev := cfg.Logger.Level == "debug"
	lg, err := logger.New(cfg.Logger.Level, isDev)
	if err != nil {
		log.Fatalf("creating logger: %v", err)
	}

	var kryptonClient kryptonclient.Client
	skipL1Lookup := false
	if cfg.KryptonClient.MockMode {
		lg.Info("using mock Krypton client (POC mode)")
		kryptonClient = kryptonclient.NewMockClient()
	} else {
		lg.Info("connecting to Krypton", slog.String("address", cfg.KryptonClient.Address))
		kryptonClient, err = kryptonclient.NewGRPCClient(cfg.KryptonClient.GRPCClient)
		if err != nil {
			log.Fatalf("failed to create Krypton gRPC client: %v", err)
		}
		skipL1Lookup = true
	}

	orch := orchestrator.NewWithOptions(kryptonClient, lg, skipL1Lookup)

	// Job store: in-memory when mock mode, PostgreSQL otherwise.
	var store jobstore.JobStore
	if cfg.KryptonClient.MockMode {
		lg.Info("using in-memory job store (mock mode)")
		store = jobstore.NewInMemoryJobStore()
	} else {
		if cfg.Database.DSN == "" {
			log.Fatal("database.dsn is required")
		}
		db, err := sql.Open("pgx", cfg.Database.DSN)
		if err != nil {
			log.Fatalf("opening database: %v", err)
		}
		defer db.Close()

		migFS, err := fs.Sub(dbpkg.MigrationsFS, "migrations")
		if err != nil {
			log.Fatalf("preparing migrations fs: %v", err)
		}
		if err := jobstore.Migrate(context.Background(), db, migFS); err != nil {
			log.Fatalf("running migrations: %v", err)
		}
		lg.Info("database migrations applied")
		store = jobstore.NewPostgresJobStore(db)
	}
	handler := server.NewHandler(orch, kryptonClient, store, lg)

	// Build Registry client.
	var regClient registryclient.Client
	if cfg.Registry.Enabled {
		lg.Info("connecting to CMK Registry", slog.String("address", cfg.Registry.Address))
		regOpts := registryclient.GRPCClientOptions{
			Insecure:    cfg.Registry.Insecure,
			BearerToken: cfg.Registry.BearerToken,
			CACert:      cfg.Registry.CACert,
			ClientCert:  cfg.Registry.ClientCert,
			ClientKey:   cfg.Registry.ClientKey,
		}
		regClient, err = registryclient.NewGRPCClientWithOptions(cfg.Registry.Address, regOpts, lg)
		if err != nil {
			log.Fatalf("failed to create Registry gRPC client: %v", err)
		}
	} else {
		lg.Info("CMK Registry disabled — using noop client")
		regClient = registryclient.NoopClient{}
	}

	grpcSrv, err := server.NewGRPCServer(handler, cfg.GRPCServer.Address, lg)
	if err != nil {
		log.Fatalf("failed to create gRPC server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start background worker pool.
	pool := worker.NewPool(
		cfg.Worker.Concurrency,
		store,
		orch,
		regClient,
		lg,
		cfg.Worker.PollInterval,
		cfg.Worker.StuckTimeout,
		cfg.Registry.Region,
		cfg.Registry.OwnerID,
		cfg.Registry.OwnerType,
		cfg.Registry.Role,
	)
	go pool.Start(ctx)

	// Start orbital AMQP operator if enabled.
	// Mirrors CMK's TenantOperator.RunOperator — KryptonReconciler owns the full lifecycle.
	if cfg.Orbital.Enabled {
		reconciler := eventprocessor.NewKryptonReconciler(slog.Default())
		go func() {
			if err := reconciler.Start(ctx, cfg.Orbital); err != nil {
				lg.Error("orbital: KryptonReconciler stopped", slog.String("error", err.Error()))
			}
		}()
	}

	healthSrv := server.NewHealthHTTPServer(kryptonClient, lg)
	go server.StartHealthServer(ctx, ":8080", healthSrv.Handler(), lg)

	lg.Info("CmkKryptonBridge starting", slog.String("version", cfg.Application.BuildInfo.Version))

	if err := grpcSrv.Serve(ctx); err != nil {
		lg.Error("gRPC server stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}

	lg.Info("CmkKryptonBridge stopped")
}
