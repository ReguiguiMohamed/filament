package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/galaxy-io/filament"
	"github.com/galaxy-io/filament/cmd/internal/boot"
	"github.com/galaxy-io/filament/cmd/internal/connectors"
	"github.com/galaxy-io/filament/cmd/internal/health"
	"github.com/galaxy-io/filament/registry"
	"github.com/galaxy-io/filament/worker"
)

const shutdownTimeout = 10 * time.Second

// serve runs the worker long-lived as the one process that carries every
// driver, answering connector calls for the control services. It needs no
// datastore or bus: each call brings its own config.
func serve(ctx context.Context) error {
	sources, err := connectors.SourcesFromEnv()
	if err != nil {
		return err
	}
	log, _, _, closeTelemetry, err := boot.Telemetry(ctx)
	if err != nil {
		return err
	}
	defer closeTelemetry()
	log = log.With(filament.Field{Key: "component", Value: "worker"})

	mux := http.NewServeMux()
	healthState := health.New(2 * time.Second)
	healthState.Mount(mux)
	mux.Handle(worker.Handler(worker.Local(sources, registry.DefaultSinks)))

	addr := workerAddress()
	srv := &http.Server{Addr: addr, Handler: otelhttp.NewHandler(mux, "worker"), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	healthState.MarkStarted()
	log.Info("worker started",
		filament.Field{Key: "event.name", Value: "worker.started"},
		filament.Field{Key: "address", Value: addr},
		filament.Field{Key: "sources", Value: len(sources.Specs())},
		filament.Field{Key: "sinks", Value: len(registry.DefaultSinks.Specs())})

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("worker listener: %w", err)
	case <-ctx.Done():
		log.Info("worker stopping", filament.Field{Key: "event.name", Value: "worker.stopping"})
		healthState.MarkStopping()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func workerAddress() string {
	if addr := os.Getenv("WORKER_ADDR"); addr != "" {
		return addr
	}
	return ":8080"
}
