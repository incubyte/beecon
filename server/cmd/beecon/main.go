// Command beecon is the single Beecon binary ("beecon serve"): it boots
// against Postgres or SQLite, runs migrations, and serves the HTTP API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"beecon/internal/app"
	"beecon/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: beecon <serve|import-membrane>")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		slog.SetDefault(logger)
		if err := serve(logger); err != nil {
			logger.Error("beecon exited with error", "err", err)
			os.Exit(1)
		}
	case "import-membrane":
		if err := runImportMembrane(os.Args[2:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: beecon <serve|import-membrane>")
		os.Exit(1)
	}
}

func serve(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}

	wired, err := app.Wire(context.Background(), app.Deps{
		Config: cfg,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("wiring failed: %w", err)
	}
	defer func() { _ = wired.Close() }()

	srv := &http.Server{
		Addr:              addrFromBaseURL(cfg.BaseURL),
		Handler:           wired.Router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("beecon listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	// Workers.Start only after the HTTP listener is up (section 3 of the
	// architecture doc, PD29); Wire itself never starts them, so every
	// existing test and journey composes the app without background
	// nondeterminism.
	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	wired.Workers.Start(workersCtx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		return fmt.Errorf("listen failed: %w", err)
	case <-stop:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}
	// Workers.Stop after the HTTP listener's own shutdown, so in-flight
	// deliveries finish (or release their claims) within the same
	// shutdown window (section 3 of the architecture doc).
	wired.Workers.Stop(shutdownCtx)
	return nil
}

// defaultPort is used when BEECON_BASE_URL carries no explicit port.
const defaultPort = "8080"

// addrFromBaseURL derives the listen address's port from BEECON_BASE_URL,
// defaulting to defaultPort when the base URL carries no explicit port or
// fails to parse.
func addrFromBaseURL(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Port() == "" {
		return ":" + defaultPort
	}
	return ":" + u.Port()
}
