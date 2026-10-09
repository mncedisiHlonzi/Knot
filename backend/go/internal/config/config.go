// Package config loads the Knot backend's runtime configuration from the
// process environment.
//
// It reads configuration only: it never opens a database connection, never dials
// Redis, and never verifies a credential against anything. Values such as the
// Postgres DSN and the JWT signing secret are stored so the composition root can
// hand them to the components that need them.
package config

import (
	"fmt"
	"os"
	"strconv"
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
	envKeyHTTPPort    = "KNOT_HTTP_PORT"
	envKeyJWTSecret   = "KNOT_JWT_SECRET"
	envKeyLogLevel    = "KNOT_LOG_LEVEL"
	envKeyS3Endpoint  = "KNOT_S3_ENDPOINT"
	envKeyS3Region    = "KNOT_S3_REGION"
	envKeyS3AccessKey = "KNOT_S3_ACCESS_KEY"
	envKeyS3SecretKey = "KNOT_S3_SECRET_KEY"
	envKeyS3Bucket    = "KNOT_S3_BUCKET"
)

// Safe local defaults. These are placeholders that match .env.example and the
// local docker-compose services; they are never used for ci or test.
const (
	defaultEnv         = EnvLocal
	defaultPostgresDSN = "postgres://knot:knot_local_only_change_me@localhost:5433/knot?sslmode=disable"
	defaultRedisAddr   = "localhost:6379"
	defaultHTTPPort    = 8080
	defaultLogLevel    = LogLevelInfo

	// Local object storage: the MinIO container's published S3 port and its
	// placeholder credentials (KNOT-ADR-028). These are local-only, exactly like
	// the Postgres and Redis defaults above.
	defaultS3Endpoint  = "http://127.0.0.1:9000"
	defaultS3Region    = "us-east-1"
	defaultS3AccessKey = "knot"
	defaultS3SecretKey = "knot_local_only_change_me"
	defaultS3Bucket    = "knot-media"
)

// LocalJWTSecretPlaceholder is the documented local-only signing secret. It is
// accepted in local development so a fresh checkout can start, and it is
// recognised so callers can warn about it.
const LocalJWTSecretPlaceholder = "change_me_in_local_development_min_32_chars"

// Log levels accepted in KNOT_LOG_LEVEL.
const (
	// LogLevelDebug is the most verbose level.
	LogLevelDebug = "debug"
	// LogLevelInfo is the default level.
	LogLevelInfo = "info"
	// LogLevelWarn reports warnings and errors only.
	LogLevelWarn = "warn"
	// LogLevelError reports errors only.
	LogLevelError = "error"
)

// Config holds the backend's runtime configuration.
//
// Note: Config intentionally has no String method, so a DSN, secret, or
// credential can never leak into a log line by accident.
type Config struct {
	// Env is the resolved environment name (see the Env* constants).
	Env string
	// PostgresDSN is the PostgreSQL connection string.
	PostgresDSN string
	// RedisAddr is the host:port of the Redis server. Nothing uses it yet: no
	// Redis client exists in the backend.
	RedisAddr string
	// HTTPPort is the TCP port the API server listens on.
	HTTPPort int
	// JWTSecret is the HS256 signing key for access and refresh tokens.
	JWTSecret string
	// LogLevel is the minimum level the structured logger emits.
	LogLevel string

	// S3Endpoint is the base URL of the S3-compatible object store that holds
	// media. Locally this is the MinIO container published on the host
	// (KNOT-ADR-028); in production it is the equivalent managed service.
	S3Endpoint string
	// S3Region is the signing region. MinIO ignores it, but the AWS SDK requires
	// one and a real bucket has a real region.
	S3Region string
	// S3AccessKey is the object store's access key id.
	S3AccessKey string
	// S3SecretKey is the object store's secret access key. It is never logged and
	// never serialised (Config has no String method).
	S3SecretKey string
	// S3Bucket is the bucket media objects are written to.
	S3Bucket string
}

// Load reads configuration from the process environment.
//
// Rules:
//   - KNOT_ENV unset or empty resolves to "local".
//   - In "local", missing values fall back to safe local defaults and a missing
//     JWT secret becomes LocalJWTSecretPlaceholder. Object storage falls back to
//     the local MinIO container's endpoint and placeholder credentials. Callers
//     should check UsingInsecureJWTSecret and warn.
//   - In "ci", "test", or any other environment name, KNOT_POSTGRES_DSN,
//     KNOT_REDIS_ADDR, and KNOT_JWT_SECRET are required, and the JWT secret must
//     be at least MinJWTSecretBytes long. KNOT_S3_ENDPOINT, KNOT_S3_ACCESS_KEY,
//     and KNOT_S3_SECRET_KEY are required too: outside local there is no default
//     object store to fall back to. Load fails fast, naming every problem.
//   - KNOT_HTTP_PORT and KNOT_LOG_LEVEL must be valid when set.
//
// Load performs no I/O beyond reading environment variables.
func Load() (Config, error) {
	httpPort, err := parseHTTPPort(os.Getenv(envKeyHTTPPort))
	if err != nil {
		return Config{}, err
	}

	logLevel, err := parseLogLevel(os.Getenv(envKeyLogLevel))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:         resolveEnv(os.Getenv(envKeyEnv)),
		PostgresDSN: strings.TrimSpace(os.Getenv(envKeyPostgresDSN)),
		RedisAddr:   strings.TrimSpace(os.Getenv(envKeyRedisAddr)),
		HTTPPort:    httpPort,
		JWTSecret:   strings.TrimSpace(os.Getenv(envKeyJWTSecret)),
		LogLevel:    logLevel,
		S3Endpoint:  strings.TrimSpace(os.Getenv(envKeyS3Endpoint)),
		S3Region:    strings.TrimSpace(os.Getenv(envKeyS3Region)),
		S3AccessKey: strings.TrimSpace(os.Getenv(envKeyS3AccessKey)),
		S3SecretKey: strings.TrimSpace(os.Getenv(envKeyS3SecretKey)),
		S3Bucket:    strings.TrimSpace(os.Getenv(envKeyS3Bucket)),
	}

	if cfg.Env == EnvLocal {
		if cfg.PostgresDSN == "" {
			cfg.PostgresDSN = defaultPostgresDSN
		}
		if cfg.RedisAddr == "" {
			cfg.RedisAddr = defaultRedisAddr
		}
		if cfg.JWTSecret == "" {
			cfg.JWTSecret = LocalJWTSecretPlaceholder
		}
		if cfg.S3Endpoint == "" {
			cfg.S3Endpoint = defaultS3Endpoint
		}
		if cfg.S3Region == "" {
			cfg.S3Region = defaultS3Region
		}
		if cfg.S3AccessKey == "" {
			cfg.S3AccessKey = defaultS3AccessKey
		}
		if cfg.S3SecretKey == "" {
			cfg.S3SecretKey = defaultS3SecretKey
		}
		if cfg.S3Bucket == "" {
			cfg.S3Bucket = defaultS3Bucket
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
	if cfg.JWTSecret == "" {
		missing = append(missing, envKeyJWTSecret)
	}
	// Object storage has no default outside local: a ci or test run must be told
	// exactly which store it is writing to and with which credentials. Region and
	// bucket keep their defaults (us-east-1, knot-media) in every environment.
	if cfg.S3Endpoint == "" {
		missing = append(missing, envKeyS3Endpoint)
	}
	if cfg.S3AccessKey == "" {
		missing = append(missing, envKeyS3AccessKey)
	}
	if cfg.S3SecretKey == "" {
		missing = append(missing, envKeyS3SecretKey)
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"config: env %q requires %s to be set",
			cfg.Env, strings.Join(missing, ", "),
		)
	}

	if len(cfg.JWTSecret) < MinJWTSecretBytes {
		return Config{}, fmt.Errorf(
			"config: %s must be at least %d bytes for env %q (got %d)",
			envKeyJWTSecret, MinJWTSecretBytes, cfg.Env, len(cfg.JWTSecret),
		)
	}

	return cfg, nil
}

// MinJWTSecretBytes is the shortest signing secret accepted outside local
// development. It mirrors identity.MinJWTSecretBytes; config cannot import
// identity, so the value is declared here too.
const MinJWTSecretBytes = 32

// UsingInsecureJWTSecret reports whether the resolved JWT secret is the local
// placeholder or otherwise too short to be trusted. The composition root uses it
// to log a prominent warning; Load itself never warns or logs.
func (c Config) UsingInsecureJWTSecret() bool {
	return c.JWTSecret == LocalJWTSecretPlaceholder || len(c.JWTSecret) < MinJWTSecretBytes
}

// parseHTTPPort resolves KNOT_HTTP_PORT, defaulting when unset.
func parseHTTPPort(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultHTTPPort, nil
	}

	port, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number: %w", envKeyHTTPPort, err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("config: %s must be between 1 and 65535 (got %d)", envKeyHTTPPort, port)
	}

	return port, nil
}

// parseLogLevel resolves KNOT_LOG_LEVEL, defaulting when unset.
func parseLogLevel(raw string) (string, error) {
	level := strings.ToLower(strings.TrimSpace(raw))
	if level == "" {
		return defaultLogLevel, nil
	}

	switch level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return level, nil
	default:
		return "", fmt.Errorf(
			"config: %s must be one of %s, %s, %s, %s (got %q)",
			envKeyLogLevel, LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError, raw,
		)
	}
}

// resolveEnv normalises KNOT_ENV, falling back to the local default.
func resolveEnv(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	if env == "" {
		return defaultEnv
	}
	return env
}
