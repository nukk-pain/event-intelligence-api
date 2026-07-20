package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/smpain/event-intelligence-api/internal/eventscoutserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("server.fatal", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	config, err := eventscoutserver.LoadRuntimeConfig()
	if err != nil {
		return err
	}
	runner, err := eventscoutserver.NewSolarDiscoveryRunner(config.SolarBackend)
	if err != nil {
		return err
	}
	handler, err := eventscoutserver.NewHandler(config.Handler, eventscoutserver.HandlerDependencies{
		Runner: runner, Clock: eventscoutserver.SystemClock{}, Logger: logger,
	})
	if err != nil {
		return err
	}
	server, err := eventscoutserver.NewServer(config.Server, handler)
	if err != nil {
		return err
	}
	logger.Info("server.start", slog.String("address", config.Server.Address))
	return server.Run(ctx)
}
