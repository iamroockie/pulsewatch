package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/config"
)

func validEnv() map[string]string {
	return map[string]string{
		"APP_ENV":          "local",
		"LOG_LEVEL":        "debug",
		"SHUTDOWN_TIMEOUT": "20s",
		"HTTP_HOST":        "localhost",
		"HTTP_PORT":        "8080",
		"PG_HOST":          "db",
		"PG_PORT":          "5432",
		"PG_NAME":          "pulsewatch",
		"PG_USER":          "pulse",
		"PG_PASS":          "secret",
		"PG_SSL":           "disable",
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for name, value := range env {
		t.Setenv(name, value)
	}
}

func envWith(name, value string) map[string]string {
	env := validEnv()
	env[name] = value

	return env
}

func TestLoad(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "local", cfg.AppEnv)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, 20*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, "localhost", cfg.HTTP.Host)
	assert.Equal(t, uint16(8080), cfg.HTTP.Port)
	assert.Equal(t, "db", cfg.Postgres.Host)
	assert.Equal(t, uint16(5432), cfg.Postgres.Port)
	assert.Equal(t, "pulsewatch", cfg.Postgres.DB)
	assert.Equal(t, "pulse", cfg.Postgres.User)
	assert.Equal(t, "secret", cfg.Postgres.Password)
	assert.Equal(t, "disable", cfg.Postgres.SSL)
}

func TestLoadRejectsEmptyValue(t *testing.T) {
	for name := range validEnv() {
		t.Run(name, func(t *testing.T) {
			setEnv(t, envWith(name, ""))

			_, err := config.Load()

			require.Error(t, err)
			assert.Contains(t, err.Error(), name)
		})
	}
}

func TestLoadRejectsMalformedValue(t *testing.T) {
	tests := map[string]struct {
		name    string
		value   string
		wantErr []string
	}{
		"port is not a number": {
			name: "HTTP_PORT", value: "abc",
			wantErr: []string{"Port", "uint16", `"abc"`},
		},
		"port exceeds uint16": {
			name: "HTTP_PORT", value: "70000",
			wantErr: []string{"Port", "out of range", `"70000"`},
		},
		"port is negative": {
			name: "PG_PORT", value: "-1",
			wantErr: []string{"Port", `"-1"`},
		},
		"unknown log level": {
			name: "LOG_LEVEL", value: "nonsense",
			wantErr: []string{"LogLevel", `"nonsense"`},
		},
		"duration without unit": {
			name: "SHUTDOWN_TIMEOUT", value: "20",
			wantErr: []string{"ShutdownTimeout", "missing unit", `"20"`},
		},
		"duration is not a number": {
			name: "SHUTDOWN_TIMEOUT", value: "soon",
			wantErr: []string{"ShutdownTimeout", `"soon"`},
		},
	}

	for title, tt := range tests {
		t.Run(title, func(t *testing.T) {
			setEnv(t, envWith(tt.name, tt.value))

			_, err := config.Load()

			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := config.PostgresConfig{
		Host:     "db",
		Port:     5432,
		DB:       "pulsewatch",
		User:     "pulse",
		Password: "secret",
		SSL:      "disable",
	}

	assert.Equal(t, "postgres://pulse:secret@db:5432/pulsewatch?sslmode=disable", cfg.DSN())
}

func TestPostgresDSNEscapesCredentials(t *testing.T) {
	cfg := config.PostgresConfig{
		Host:     "db",
		Port:     5432,
		DB:       "pulsewatch",
		User:     "pulse user",
		Password: "p@ss:w0rd/",
		SSL:      "verify-full",
	}

	want := "postgres://pulse%20user:p%40ss%3Aw0rd%2F@db:5432/pulsewatch?sslmode=verify-full"
	assert.Equal(t, want, cfg.DSN())
}

func TestHTTPAddr(t *testing.T) {
	tests := map[string]struct {
		cfg  config.HTTPConfig
		want string
	}{
		"hostname": {cfg: config.HTTPConfig{Host: "localhost", Port: 8080}, want: "localhost:8080"},
		"ipv4":     {cfg: config.HTTPConfig{Host: "0.0.0.0", Port: 80}, want: "0.0.0.0:80"},
		"ipv6":     {cfg: config.HTTPConfig{Host: "::1", Port: 8080}, want: "[::1]:8080"},
	}

	for title, tt := range tests {
		t.Run(title, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.Addr())
		})
	}
}
