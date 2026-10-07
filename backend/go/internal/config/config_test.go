package config

import (
	"strings"
	"testing"
)

// clearEnv removes every variable Load reads so each test starts from a known
// state, regardless of the machine running the tests.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envKeyEnv, envKeyPostgresDSN, envKeyRedisAddr} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaultsWhenEnvUnset(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error %v, want nil", err)
	}

	if cfg.Env != EnvLocal {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvLocal)
	}
	if cfg.PostgresDSN != defaultPostgresDSN {
		t.Errorf("PostgresDSN = %q, want default %q", cfg.PostgresDSN, defaultPostgresDSN)
	}
	if cfg.RedisAddr != defaultRedisAddr {
		t.Errorf("RedisAddr = %q, want default %q", cfg.RedisAddr, defaultRedisAddr)
	}
}

func TestLoadLocalFallsBackToDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv(envKeyEnv, EnvLocal)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error %v, want nil", err)
	}

	if cfg.PostgresDSN != defaultPostgresDSN {
		t.Errorf("PostgresDSN = %q, want default %q", cfg.PostgresDSN, defaultPostgresDSN)
	}
	if cfg.RedisAddr != defaultRedisAddr {
		t.Errorf("RedisAddr = %q, want default %q", cfg.RedisAddr, defaultRedisAddr)
	}
}

func TestLoadLocalHonoursEnvOverrides(t *testing.T) {
	clearEnv(t)
	const (
		wantDSN   = "postgres://someone:secret@db.internal:5432/knot?sslmode=require"
		wantRedis = "redis.internal:6380"
	)
	t.Setenv(envKeyEnv, EnvLocal)
	t.Setenv(envKeyPostgresDSN, wantDSN)
	t.Setenv(envKeyRedisAddr, wantRedis)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error %v, want nil", err)
	}

	if cfg.PostgresDSN != wantDSN {
		t.Errorf("PostgresDSN = %q, want %q", cfg.PostgresDSN, wantDSN)
	}
	if cfg.RedisAddr != wantRedis {
		t.Errorf("RedisAddr = %q, want %q", cfg.RedisAddr, wantRedis)
	}
}

func TestLoadTrimsWhitespaceFromValues(t *testing.T) {
	clearEnv(t)
	t.Setenv(envKeyEnv, "  LOCAL  ")
	t.Setenv(envKeyPostgresDSN, "  postgres://knot@localhost:5432/knot  ")
	t.Setenv(envKeyRedisAddr, "\tlocalhost:6379\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error %v, want nil", err)
	}

	if cfg.Env != EnvLocal {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvLocal)
	}
	if cfg.PostgresDSN != "postgres://knot@localhost:5432/knot" {
		t.Errorf("PostgresDSN = %q, want trimmed value", cfg.PostgresDSN)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("RedisAddr = %q, want trimmed value", cfg.RedisAddr)
	}
}

func TestLoadCIFailsFastWhenAllRequiredVarsMissing(t *testing.T) {
	clearEnv(t)
	t.Setenv(envKeyEnv, EnvCI)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want error for env=ci with no variables set")
	}
	for _, want := range []string{envKeyPostgresDSN, envKeyRedisAddr} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name missing variable %q", err.Error(), want)
		}
	}
}

func TestLoadCIFailsFastWhenOnlyDSNMissing(t *testing.T) {
	clearEnv(t)
	t.Setenv(envKeyEnv, EnvCI)
	t.Setenv(envKeyRedisAddr, "localhost:6379")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want error for env=ci with no DSN")
	}
	if !strings.Contains(err.Error(), envKeyPostgresDSN) {
		t.Errorf("error %q does not name %q", err.Error(), envKeyPostgresDSN)
	}
	if strings.Contains(err.Error(), envKeyRedisAddr) {
		t.Errorf("error %q names %q, which was set", err.Error(), envKeyRedisAddr)
	}
}

func TestLoadTestFailsFastWhenRedisMissing(t *testing.T) {
	clearEnv(t)
	t.Setenv(envKeyEnv, EnvTest)
	t.Setenv(envKeyPostgresDSN, "postgres://knot@localhost:5432/knot")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want error for env=test with no Redis address")
	}
	if !strings.Contains(err.Error(), envKeyRedisAddr) {
		t.Errorf("error %q does not name %q", err.Error(), envKeyRedisAddr)
	}
}

func TestLoadCIDoesNotApplyLocalDefaults(t *testing.T) {
	clearEnv(t)
	const (
		wantDSN   = "postgres://ci:ci@127.0.0.1:5432/knot_test"
		wantRedis = "127.0.0.1:6379"
	)
	t.Setenv(envKeyEnv, EnvCI)
	t.Setenv(envKeyPostgresDSN, wantDSN)
	t.Setenv(envKeyRedisAddr, wantRedis)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error %v, want nil", err)
	}

	if cfg.Env != EnvCI {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvCI)
	}
	if cfg.PostgresDSN != wantDSN {
		t.Errorf("PostgresDSN = %q, want %q (no local default in ci)", cfg.PostgresDSN, wantDSN)
	}
	if cfg.RedisAddr != wantRedis {
		t.Errorf("RedisAddr = %q, want %q (no local default in ci)", cfg.RedisAddr, wantRedis)
	}
}

func TestLoadUnknownEnvRequiresVars(t *testing.T) {
	clearEnv(t)
	t.Setenv(envKeyEnv, "staging")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil error, want error for unrecognised env with no variables set")
	}
}

func TestResolveEnv(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty falls back to local", raw: "", want: EnvLocal},
		{name: "whitespace only falls back to local", raw: "   ", want: EnvLocal},
		{name: "lowercase preserved", raw: "local", want: EnvLocal},
		{name: "uppercase normalised", raw: "CI", want: EnvCI},
		{name: "mixed case and padding normalised", raw: "  Test ", want: EnvTest},
		{name: "unknown value preserved lowercased", raw: "Staging", want: "staging"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveEnv(tt.raw); got != tt.want {
				t.Errorf("resolveEnv(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
