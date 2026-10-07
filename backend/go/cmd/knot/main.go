// Command knot is the entry point for the Knot backend.
//
// It is the composition root: it loads configuration, builds the structured
// logger, connects to PostgreSQL, assembles the store -> service -> handler graph
// explicitly (there is no dependency-injection framework), and serves the HTTP
// API. It is the only place that knows how the layers are wired together.
//
// Usage:
//
//	knot              start the HTTP API
//	knot migrate up   apply pending database migrations, then exit
//
// Migrations are deliberately NOT run automatically on server start: schema
// changes stay an explicit, reviewable action.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/appinfo"
	"github.com/knot/backend/internal/config"
	"github.com/knot/backend/internal/httpapi"
	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/migrations"
)

// Timeouts used at startup and shutdown.
const (
	// startupTimeout bounds the initial database connection.
	startupTimeout = 10 * time.Second
	// migrateTimeout bounds a whole migration run.
	migrateTimeout = 2 * time.Minute
	// shutdownGrace is how long in-flight requests get to finish.
	shutdownGrace = 10 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// The logger is built inside run, so report failures on stderr.
		fmt.Fprintf(os.Stderr, "%s: %v\n", appinfo.Name, err)
		os.Exit(1)
	}
}

// run parses the subcommand and dispatches to it.
func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	if cfg.UsingInsecureJWTSecret() {
		logger.Warn(
			"using an insecure JWT secret; tokens are forgeable by anyone who can read it. Set KNOT_JWT_SECRET to at least 32 random bytes.",
			slog.String("env", cfg.Env),
		)
	}

	switch {
	case len(args) == 0:
		return serve(cfg, logger)
	case len(args) == 2 && args[0] == "migrate" && args[1] == "up":
		return migrateUp(cfg, logger)
	default:
		return fmt.Errorf("unknown command %q; usage: knot [migrate up]", args)
	}
}

// newLogger returns the structured logger for the configured environment: text
// for local development, JSON everywhere else.
func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case config.LogLevelDebug:
		level = slog.LevelDebug
	case config.LogLevelWarn:
		level = slog.LevelWarn
	case config.LogLevelError:
		level = slog.LevelError
	}

	options := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Env == config.EnvLocal {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}

	return slog.New(handler)
}

// serve builds the application graph, starts the HTTP server, and blocks until a
// termination signal arrives, then drains in-flight requests.
func serve(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	store, err := identity.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	tokens, err := identity.NewTokenIssuer(cfg.JWTSecret)
	if err != nil {
		return err
	}

	service, err := identity.NewService(store, tokens)
	if err != nil {
		return err
	}

	authHandler, err := httpapi.NewAuthHandler(service, logger)
	if err != nil {
		return err
	}

	router, err := httpapi.NewRouter(authHandler, appinfo.Version, logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router.Handler(),
		// These bounds protect the server from slow or stalled clients.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(fmt.Sprintf("%s %s starting (env=%s, port=%d)", appinfo.Name, appinfo.Version, cfg.Env, cfg.HTTPPort))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("termination signal received; draining connections", slog.Duration("grace", shutdownGrace))
	}

	// The signal context is already cancelled, so shutdown needs a fresh one.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("shutdown complete")

	return nil
}

// migrateUp applies every pending migration and exits.
func migrateUp(cfg config.Config, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	pool, err := connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := migrations.Up(ctx, pool)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		logger.Info("database schema is already up to date")
		return nil
	}

	logger.Info("migrations applied", slog.Any("versions", applied))

	return nil
}

// connect opens the pool and proves it can reach the database, so a bad DSN or a
// down database fails the process immediately rather than on the first request.
//
// The DSN is never logged: it can carry credentials.
func connect(ctx context.Context, cfg config.Config, logger *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("configure postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		logger.Error("could not reach postgres", slog.String("env", cfg.Env))
		return nil, fmt.Errorf("reach postgres: %w", err)
	}

	return pool, nil
}
