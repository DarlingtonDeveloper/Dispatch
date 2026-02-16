package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MikeSquared-Agency/Dispatch/internal/alexandria"
	"github.com/MikeSquared-Agency/Dispatch/internal/api"
	"github.com/MikeSquared-Agency/Dispatch/internal/broker"
	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	db, err := store.NewPostgresStore(ctx, cfg.Database.URL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("connected to database")

	// Hermes (optional)
	var hermesClient hermes.Client
	if cfg.Hermes.URL != "" {
		hc, err := hermes.NewNATSClient(ctx, cfg.Hermes.URL, logger)
		if err != nil {
			logger.Warn("failed to connect to hermes, running without events", "error", err)
		} else {
			hermesClient = hc
			defer hc.Close()
			logger.Info("connected to hermes")
		}
	}

	// Warren
	warrenClient := warren.NewHTTPClient(cfg.Warren.URL, cfg.Warren.Token)

	// PromptForge
	forgeClient := forge.NewHTTPClient(cfg.PromptForge.URL)

	// Alexandria
	alexandriaClient := alexandria.NewHTTPClient(cfg.Alexandria.URL)

	// Broker
	b := broker.New(db, hermesClient, warrenClient, forgeClient, alexandriaClient, cfg, logger)
	b.Start(ctx)
	defer b.Stop()
	logger.Info("broker started", "tick_interval", cfg.TickInterval())

	// Subscribe to NATS events for bookkeeping
	b.SetupSubscriptions()

	// Backlog scorer
	backlogScorer := scoring.NewBacklogScorer(scoring.BacklogWeightSet{
		BusinessImpact:      cfg.Scoring.BacklogWeights.BusinessImpact,
		DependencyReadiness: cfg.Scoring.BacklogWeights.DependencyReadiness,
		Urgency:             cfg.Scoring.BacklogWeights.Urgency,
		CostEfficiency:      cfg.Scoring.BacklogWeights.CostEfficiency,
	})

	// API server
	router := api.NewRouter(db, hermesClient, warrenClient, forgeClient, b, backlogScorer, cfg, cfg.Server.AdminToken, logger)
	apiServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	// Metrics server
	metricsRouter := api.NewMetricsRouter()
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.MetricsPort),
		Handler: metricsRouter,
	}

	go func() {
		logger.Info("API server starting", "port", cfg.Server.Port)
		if err := apiServer.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("API server error", "error", err)
		}
	}()

	go func() {
		logger.Info("metrics server starting", "port", cfg.Server.MetricsPort)
		if err := metricsServer.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("metrics server error", "error", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = apiServer.Shutdown(shutdownCtx)
	_ = metricsServer.Shutdown(shutdownCtx)
	// b.Stop() handled by defer on line 74

	logger.Info("shutdown complete")
}
