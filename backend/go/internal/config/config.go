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

// Object-storage providers the backend knows how to be pointed at.
//
// The choice changes nothing about the code that talks to the store: both speak
// the S3 API through the same client, so this value is a label — it names which
// endpoint the credentials belong to, and it is what the startup log reports
// (KNOT-037, KNOT-ADR-091).
const (
	// StorageProviderMinIO is the local docker-compose MinIO service
	// (KNOT-ADR-028). It is the default in every environment.
	StorageProviderMinIO = "minio"
	// StorageProviderR2 is Cloudflare R2, the production object store: S3-compatible,
	// with no egress charge (KNOT-ADR-091). Its endpoint is
	// https://<account-id>.r2.cloudflarestorage.com and its region is "auto".
	StorageProviderR2 = "r2"
)

// Environment variable names read by Load.
const (
	envKeyEnv             = "KNOT_ENV"
	envKeyPostgresDSN     = "KNOT_POSTGRES_DSN"
	envKeyRedisAddr       = "KNOT_REDIS_ADDR"
	envKeyRedisPassword   = "KNOT_REDIS_PASSWORD"
	envKeyRedisDB         = "KNOT_REDIS_DB"
	envKeyHTTPPort        = "KNOT_HTTP_PORT"
	envKeyJWTSecret       = "KNOT_JWT_SECRET"
	envKeyAdminSecret     = "KNOT_ADMIN_SECRET"
	envKeyLogLevel        = "KNOT_LOG_LEVEL"
	envKeyS3Endpoint      = "KNOT_S3_ENDPOINT"
	envKeyS3Region        = "KNOT_S3_REGION"
	envKeyS3AccessKey     = "KNOT_S3_ACCESS_KEY"
	envKeyS3SecretKey     = "KNOT_S3_SECRET_KEY"
	envKeyS3Bucket        = "KNOT_S3_BUCKET"
	envKeyStorageProvider = "KNOT_STORAGE_PROVIDER"
	envKeyResendAPIKey    = "KNOT_RESEND_API_KEY"
	envKeyEmailFrom       = "KNOT_EMAIL_FROM"
	envKeyWebURL          = "KNOT_WEB_URL"
	envKeyFeedCache       = "KNOT_FEED_CACHE_ENABLED"
	envKeyFeedThreshold   = "KNOT_FEED_FANOUT_THRESHOLD"
	envKeyFeedCap         = "KNOT_FEED_CAP"
)

// Safe local defaults. These are placeholders that match .env.example and the
// local docker-compose services; they are never used for ci or test.
const (
	defaultEnv         = EnvLocal
	defaultPostgresDSN = "postgres://knot:knot_local_only_change_me@localhost:5433/knot?sslmode=disable"
	defaultRedisAddr   = "localhost:6379"
	// defaultRedisDB is Redis's own default database. A local stack that is not
	// shared with anything else has no reason to use another.
	defaultRedisDB  = 0
	defaultHTTPPort = 8080
	defaultLogLevel = LogLevelInfo
	// Local object storage: the MinIO container's published S3 port and its
	// placeholder credentials (KNOT-ADR-028). These are local-only, exactly like
	// the Postgres and Redis defaults above.
	defaultS3Endpoint = "http://127.0.0.1:9000"
	// defaultS3Region is R2's signing region. MinIO ignores the value entirely (it
	// signs with whatever region it is told), so one default serves both providers
	// and the deployed product needs no override (KNOT-037).
	defaultS3Region    = "auto"
	defaultS3AccessKey = "knot"
	defaultS3SecretKey = "knot_local_only_change_me"
	defaultS3Bucket    = "knot-media"
	// defaultStorageProvider is MinIO: a checkout with no object-store
	// configuration must keep working against the local container.
	defaultStorageProvider = StorageProviderMinIO
	// DefaultEmailFrom is the sender Resend's sandbox accepts without a verified
	// domain, which is what a checkout without a Resend account can send from
	// (KNOT-ADR-090). It is the same default the email package documents.
	defaultEmailFrom = "Knot <onboarding@resend.dev>"
	// defaultWebURL is the placeholder origin behind the clickable links in the
	// verification and reset emails. It is a reserved mDNS name, so it resolves
	// nowhere on purpose: there is no web app yet, and a deployment must set
	// KNOT_WEB_URL to its real origin before anyone relies on the link.
	defaultWebURL = "https://knot.local"

	// DefaultFeedCacheEnabled is ON: the precomputed feed is the default read path,
	// and the switch exists so a problem with Redis can be diagnosed against the SQL
	// feed without a deploy (KNOT-036).
	defaultFeedCacheEnabled = true
	// DefaultFeedFanoutThreshold mirrors feedcache.FanoutThreshold: past this audience
	// size an author's stories are stored once instead of fanned out.
	DefaultFeedFanoutThreshold = 10000
	// DefaultFeedCap mirrors feedcache.FeedCap: how many entries one reader's
	// precomputed feed holds.
	DefaultFeedCap = 800
)

// LocalJWTSecretPlaceholder is the documented local-only signing secret. It is
// accepted in local development so a fresh checkout can start, and it is
// recognised so callers can warn about it.
const LocalJWTSecretPlaceholder = "change_me_in_local_development_min_32_chars"

// LocalAdminSecretPlaceholder is the documented local-only admin secret.
//
// It guards the MVP purge trigger (KNOT-034). It is a placeholder in exactly the
// sense the JWT placeholder is: accepted locally so a fresh checkout can call the
// endpoint, never acceptable outside local development.
const LocalAdminSecretPlaceholder = "knot_local_admin_secret_change_me"

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
	// RedisAddr is the host:port of the Redis server, which backs sessions, rate
	// limiting, caching and token management (KNOT-ADR-082).
	RedisAddr string
	// RedisPassword is the Redis password, or "" when the server requires none —
	// which is how the local docker-compose stack is configured. It is read
	// verbatim rather than trimmed, because a Redis password is opaque bytes and a
	// leading or trailing space may be part of it. Like every other secret here it
	// is never logged (Config has no String method).
	RedisPassword string
	// RedisDB is the logical Redis database to select. It is 0 unless a deployment
	// deliberately shares one server between environments.
	RedisDB int
	// HTTPPort is the TCP port the API server listens on.
	HTTPPort int
	// JWTSecret is the HS256 signing key for access and refresh tokens.
	JWTSecret string
	// AdminSecret guards the operator-only endpoints — today just the MVP account
	// purge trigger (KNOT-034). It is a shared secret in a header rather than a user
	// role because there is no admin UI and no admin account model yet, and inventing
	// one for a single endpoint would be the tail wagging the dog.
	AdminSecret string
	// LogLevel is the minimum level the structured logger emits.
	LogLevel string

	// S3Endpoint is the base URL of the S3-compatible object store that holds
	// media. Locally this is the MinIO container published on the host
	// (KNOT-ADR-028); in production it is the equivalent managed service.
	S3Endpoint string
	// S3Region is the signing region. MinIO ignores it, but the AWS SDK requires
	// one and a real bucket has a real region. R2 signs with "auto" (KNOT-037).
	S3Region string
	// S3AccessKey is the object store's access key id.
	S3AccessKey string
	// S3SecretKey is the object store's secret access key. It is never logged and
	// never serialised (Config has no String method).
	S3SecretKey string
	// S3Bucket is the bucket media objects are written to.
	S3Bucket string
	// StorageProvider names which object store the S3 settings point at:
	// StorageProviderMinIO or StorageProviderR2. It is a label rather than a code
	// path — both speak the S3 API — and it exists so the composition root can say
	// at startup which store this process will write to (KNOT-ADR-091).
	StorageProvider string

	// ResendAPIKey is the Resend API key that delivers verification and password
	// reset emails (KNOT-ADR-090). An empty value is a supported configuration: the
	// email service becomes a no-op that logs each message, which is how local
	// development and CI run. Like every other secret here it is never logged
	// (Config has no String method).
	ResendAPIKey string
	// EmailFrom is the From header on those emails. It must be an address the
	// Resend account is allowed to send from; the sandbox default works only with
	// the onboarding@resend.dev sender.
	EmailFrom string
	// WebURL is the public origin the emails link to, e.g. https://knot.example.
	// It backs the clickable button, which has to be https because many mail
	// clients do not treat a custom scheme as a link (KNOT-031a-fix). A trailing
	// slash is tolerated and normalised by the authentication service.
	WebURL string

	// FeedCacheEnabled switches the precomputed feed on or off (KNOT-036). With it
	// off, GET /stories takes the PostgreSQL path exactly as it did before, and no
	// story is fanned out — which is the escape hatch an operator needs if Redis is
	// the problem.
	FeedCacheEnabled bool
	// FeedFanoutThreshold is the audience size above which an author is treated as
	// high-fanout: their stories are stored once and pulled at read time instead of
	// being written into every reader's feed.
	FeedFanoutThreshold int
	// FeedCap is how many entries one reader's precomputed feed holds. The sorted set
	// is trimmed to it on every insert, which is what replaces a time to live.
	FeedCap int
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
//   - KNOT_REDIS_PASSWORD is optional in every environment and is read verbatim:
//     an empty value means the server requires no password, which is how the
//     local docker-compose stack runs.
//   - KNOT_REDIS_DB must be a non-negative integer and defaults to 0 when unset.
//   - KNOT_RESEND_API_KEY is optional in every environment: without it the email
//     service logs each message instead of sending it (KNOT-ADR-090).
//   - KNOT_EMAIL_FROM defaults to the Resend sandbox sender when unset.
//   - KNOT_WEB_URL is the origin the emails link to and defaults to
//     https://knot.local, a placeholder that resolves nowhere (KNOT-031a-fix).
//   - In "local", a missing KNOT_ADMIN_SECRET becomes LocalAdminSecretPlaceholder;
//     outside local it is required, like the JWT secret it sits beside.
//   - KNOT_STORAGE_PROVIDER names the object store the S3 settings belong to:
//     "minio" (the default, in every environment) or "r2". Any other value is
//     an error, because a typo there would point a deployment at the wrong store
//     (KNOT-037).
//   - KNOT_FEED_CACHE_ENABLED switches the precomputed feed on or off. It defaults
//     to true; "false", "0", "no" and "off" switch it off, and anything else is
//     an error rather than a silent truthy read (KNOT-036).
//   - KNOT_FEED_FANOUT_THRESHOLD and KNOT_FEED_CAP must be positive integers when
//     set, and default to 10000 and 800. A zero or negative value is an error: a
//     feed of size zero would silently serve nothing.
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

	redisDB, err := parseRedisDB(os.Getenv(envKeyRedisDB))
	if err != nil {
		return Config{}, err
	}

	storageProvider, err := parseStorageProvider(os.Getenv(envKeyStorageProvider))
	if err != nil {
		return Config{}, err
	}

	feedCacheEnabled, err := parseFeedCacheEnabled(os.Getenv(envKeyFeedCache))
	if err != nil {
		return Config{}, err
	}

	feedFanoutThreshold, err := parsePositiveCount(envKeyFeedThreshold, os.Getenv(envKeyFeedThreshold), DefaultFeedFanoutThreshold)
	if err != nil {
		return Config{}, err
	}

	feedCap, err := parsePositiveCount(envKeyFeedCap, os.Getenv(envKeyFeedCap), DefaultFeedCap)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:             resolveEnv(os.Getenv(envKeyEnv)),
		PostgresDSN:     strings.TrimSpace(os.Getenv(envKeyPostgresDSN)),
		RedisAddr:       strings.TrimSpace(os.Getenv(envKeyRedisAddr)),
		RedisPassword:   os.Getenv(envKeyRedisPassword),
		RedisDB:         redisDB,
		HTTPPort:        httpPort,
		JWTSecret:       strings.TrimSpace(os.Getenv(envKeyJWTSecret)),
		AdminSecret:     strings.TrimSpace(os.Getenv(envKeyAdminSecret)),
		LogLevel:        logLevel,
		S3Endpoint:      strings.TrimSpace(os.Getenv(envKeyS3Endpoint)),
		S3Region:        strings.TrimSpace(os.Getenv(envKeyS3Region)),
		S3AccessKey:     strings.TrimSpace(os.Getenv(envKeyS3AccessKey)),
		S3SecretKey:     strings.TrimSpace(os.Getenv(envKeyS3SecretKey)),
		S3Bucket:        strings.TrimSpace(os.Getenv(envKeyS3Bucket)),
		StorageProvider: storageProvider,
		ResendAPIKey:    strings.TrimSpace(os.Getenv(envKeyResendAPIKey)),
		EmailFrom:       strings.TrimSpace(os.Getenv(envKeyEmailFrom)),
		WebURL:          strings.TrimSpace(os.Getenv(envKeyWebURL)),

		FeedCacheEnabled:    feedCacheEnabled,
		FeedFanoutThreshold: feedFanoutThreshold,
		FeedCap:             feedCap,
	}

	// The From address has a default in every environment: there is nothing
	// unsafe about the Resend sandbox sender, and an empty header would make every
	// send fail for a reason that reads like a bug.
	if cfg.EmailFrom == "" {
		cfg.EmailFrom = defaultEmailFrom
	}

	// Likewise the web origin: an empty base would turn every button in an email
	// into a dead link, which reads as a bug rather than as missing configuration.
	if cfg.WebURL == "" {
		cfg.WebURL = defaultWebURL
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
		if cfg.AdminSecret == "" {
			cfg.AdminSecret = LocalAdminSecretPlaceholder
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
	// The admin secret is required outside local for the same reason the JWT secret
	// is: a default would be a published credential guarding an endpoint that
	// anonymises accounts.
	if cfg.AdminSecret == "" {
		missing = append(missing, envKeyAdminSecret)
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

// parseStorageProvider resolves KNOT_STORAGE_PROVIDER.
//
// An empty value is MinIO, so every existing checkout keeps working unchanged. An
// unknown value is refused rather than treated as MinIO: pointing a deployment at
// the wrong object store would silently write media nobody can find (KNOT-037).
func parseStorageProvider(raw string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(raw))
	switch provider {
	case "":
		return defaultStorageProvider, nil
	case StorageProviderMinIO, StorageProviderR2:
		return provider, nil
	default:
		return "", fmt.Errorf(
			"config: %s must be one of %s, %s (got %q)",
			envKeyStorageProvider, StorageProviderMinIO, StorageProviderR2, raw,
		)
	}
}

// parseRedisDB resolves KNOT_REDIS_DB, defaulting to 0 when unset.
//
// A typo in the number is caught here rather than at the first command that
// reaches Redis, where it would look like a connection problem.
func parseRedisDB(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultRedisDB, nil
	}

	db, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number: %w", envKeyRedisDB, err)
	}
	if db < 0 {
		return 0, fmt.Errorf("config: %s must not be negative (got %d)", envKeyRedisDB, db)
	}

	return db, nil
}

// resolveEnv normalises KNOT_ENV, falling back to the local default.
func resolveEnv(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	if env == "" {
		return defaultEnv
	}
	return env
}

// parseFeedCacheEnabled resolves KNOT_FEED_CACHE_ENABLED.
//
// Unset is ON, because the precomputed feed is the default read path. The explicit
// false spellings are accepted so a shell, a compose file and a dashboard can each
// write the value the way they prefer; anything else is refused rather than
// silently read as true, because "KNOT_FEED_CACHE_ENABLED=nope" that quietly left
// the cache on would be a trap (KNOT-036).
func parseFeedCacheEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return defaultFeedCacheEnabled, nil
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf(
			"config: %s must be true or false (got %q)",
			envKeyFeedCache, raw,
		)
	}
}

// parsePositiveCount resolves a positive-integer setting with a default.
//
// Zero is refused rather than treated as "unset": a feed cap of zero would serve
// every reader an empty feed, which is a silent outage, and a fan-out threshold of
// zero would push every author down the high-fanout path.
func parsePositiveCount(name, raw string, fallback int) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number: %w", name, err)
	}
	if value < 1 {
		return 0, fmt.Errorf("config: %s must be positive (got %d)", name, value)
	}

	return value, nil
}
