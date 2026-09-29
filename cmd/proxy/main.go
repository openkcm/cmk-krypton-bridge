package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/server"
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
	defer lg.Sync() //nolint:errcheck

	var kryptonClient kryptonclient.Client
	skipL1Lookup := false
	if cfg.KryptonClient.MockMode {
		lg.Info("using mock Krypton client (POC mode)")
		kryptonClient = kryptonclient.NewMockClient()
	} else {
		lg.Info("connecting to Krypton", zap.String("address", cfg.KryptonClient.Address))
		kryptonClient, err = kryptonclient.NewGRPCClient(cfg.KryptonClient.GRPCClient)
		if err != nil {
			lg.Fatal("failed to create Krypton gRPC client", zap.Error(err))
		}
		skipL1Lookup = true
	}

	orch := orchestrator.NewWithOptions(kryptonClient, lg, skipL1Lookup)
	handler := server.NewHandler(orch, kryptonClient, lg)

	grpcSrv, err := server.NewGRPCServer(handler, cfg.GRPCServer.Address, lg)
	if err != nil {
		lg.Fatal("failed to create gRPC server", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	healthSrv := server.NewHealthHTTPServer(kryptonClient, lg)
	go server.StartHealthServer(ctx, ":8080", healthSrv.Handler(), lg)

	lg.Info("CmkKryptonBridge starting", zap.String("version", cfg.Application.BuildInfo.Version))

	if err := grpcSrv.Serve(ctx); err != nil {
		lg.Error("gRPC server stopped", zap.Error(err))
		os.Exit(1)
	}

	lg.Info("CmkKryptonBridge stopped")
}
