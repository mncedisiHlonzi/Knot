// Package config loads the Knot backend's runtime configuration from the
// process environment.
//
// KNOT-002 scope: configuration loading ONLY. This package deliberately does
// not open, dial, or validate a connection to PostgreSQL or Redis, and it
// imports nothing outside the Go standard library — no database driver and no
// Redis client exist in the backend yet.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Environment names the backend understands.
const (
	// EnvLocal is the default when KNOT_ENV is unset or empty.
	EnvLocal = "local"
	// EnvCI is the continuous-integration environment.
	EnvCI = "ci"
	// EnvTest is an explicit test environment.
	EnvTest = "test"
)

// Environment variable names read by Load.
const (
	envKeyEnv         = "KNOT_ENV"
	envKeyPostgresDSN = "KNOT_POSTGRES_DSN"
	envKeyRedisAddr   = "KNOT_REDIS_ADDR"
)

// Safe local defaults. These are placeholders that match .env.example and the
// local docker-compose services; they are never used for ci or test.
const (
	defaultEnv         = EnvLocal
	defaultPostgresDSN = "postgres://knot:knot_local_only_change_me@localhost:5433/knot?sslmode=disable"
	defaultRedisAddr   = "localhost:6379"
)

// Config holds the backend's runtime configuration.
//
// Note: Config intentionally has no String method, so a DSN or credentials can
// never leak into a log line by accident. Log cfg.Env only.
type Config struct {
	// Env is the resolved environment name (see the Env* constants).
	Env string
	// PostgresDSN is the PostgreSQL connection string. It is stored, not used:
	// no connection is opened by this package.
	PostgresDSN string
	// RedisAddr is the host:port of the Redis server. It is stored, not used:
	// no connection is opened by this package.
	RedisAddr string
}

// Load reads configuration from the process environment.
//
// Rules:
//   - KNOT_ENV unset or empty resolves to "local".
//   - When the resolved environment is "local", missing KNOT_POSTGRES_DSN and
//     KNOT_REDIS_ADDR fall back to safe local defaults.
//   - When the resolved environment is "ci" or "test", both variables are
//     required and Load fails fast, naming every missing variable.
//   - For any other environment name, both variables are also required: an
//     unrecognised environment never gets silent local defaults.
//
// Load performs no I/O beyond reading environment variables.
func Load() (Config, error) {
	cfg := Config{
		Env:         resolveEnv(os.Getenv(envKeyEnv)),
		PostgresDSN: strings.TrimSpace(os.Getenv(envKeyPostgresDSN)),
		RedisAddr:   strings.TrimSpace(os.Getenv(envKeyRedisAddr)),
	}

	if cfg.Env == EnvLocal {
		if cfg.PostgresDSN == "" {
			cfg.PostgresDSN = defaultPostgresDSN
		}
		if cfg.RedisAddr == "" {
			cfg.RedisAddr = defaultRedisAddr
		}
		return cfg, nil
	}

	var missing []string
	if cfg.PostgresDSN == "" {
		missing = append(missing, envKeyPostgresDSN)
	}
	if cfg.RedisAddr == "" {
		missing = append(missing, envKeyRedisAddr)
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"config: env %q requires %s to be set",
			cfg.Env, strings.Join(missing, ", "),
		)
	}

	return cfg, nil
}

// resolveEnv normalises KNOT_ENV, falling back to the local default.
func resolveEnv(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	if env == "" {
		return defaultEnv
	}
	return env
}
