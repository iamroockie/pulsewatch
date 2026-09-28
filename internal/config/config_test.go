package config_test

import (
	"log/slog"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/config"
)

func baseEnv() map[string]string {
	return map[string]string{
		"APP_ENV":          "local",
		"LOG_LEVEL":        "debug",
		"SHUTDOWN_TIMEOUT": "20s",
		"PG_HOST":          "db",
		"PG_PORT":          "5432",
		"PG_NAME":          "pulsewatch",
		"PG_USER":          "pulse",
		"PG_PASS":          "secret",
		"PG_SSL":           "disable",
	}
}

func apiEnv() map[string]string {
	env := baseEnv()
	env["HTTP_HOST"] = "localhost"
	env["HTTP_PORT"] = "8080"

	return env
}

func workerEnv() map[string]string {
	env := baseEnv()
	env["WORKER_COUNT"] = "8"
	env["REDIS_HOST"] = "cache"
	env["REDIS_PORT"] = "6379"
	env["REDIS_PASS"] = "hidden"

	return env
}

func wantBase() config.Base {
	return config.Base{
		AppEnv:          "local",
		LogLevel:        slog.LevelDebug,
		ShutdownTimeout: 20 * time.Second,
		Postgres: config.PostgresConfig{
			Host:     "db",
			Port:     5432,
			DB:       "pulsewatch",
			User:     "pulse",
			Password: "secret",
			SSL:      "disable",
		},
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for name, value := range env {
		t.Setenv(name, value)
	}
}

func with(env map[string]string, name, value string) map[string]string {
	env = maps.Clone(env)
	env[name] = value

	return env
}

func loadErr[T config.API | config.Worker]() error {
	_, err := config.Load[T]()

	return err
}

func TestLoadAPI(t *testing.T) {
	setEnv(t, apiEnv())
	want := config.API{
		Base: wantBase(),
		HTTP: config.HTTPConfig{Host: "localhost", Port: 8080},
	}

	got, err := config.Load[config.API]()

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLoadWorker(t *testing.T) {
	setEnv(t, workerEnv())
	want := config.Worker{
		Base:        wantBase(),
		WorkerCount: 8,
		Redis:       config.RedisConfig{Host: "cache", Port: 6379, Password: "hidden"},
	}

	got, err := config.Load[config.Worker]()

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLoadRejectsEmptyValue(t *testing.T) {
	tests := map[string]struct {
		env  map[string]string
		load func() error
	}{
		"api":    {env: apiEnv(), load: loadErr[config.API]},
		"worker": {env: workerEnv(), load: loadErr[config.Worker]},
	}

	for name, test := range tests {
		for key := range test.env {
			t.Run(name+"/"+key, func(t *testing.T) {
				setEnv(t, with(test.env, key, ""))

				err := test.load()

				require.Error(t, err)
				assert.Contains(t, err.Error(), key)
			})
		}
	}
}

func TestLoadRejectsMalformedValue(t *testing.T) {
	tests := map[string]struct {
		env     map[string]string
		load    func() error
		wantErr []string
	}{
		"port is not a number": {
			env:     with(apiEnv(), "HTTP_PORT", "abc"),
			load:    loadErr[config.API],
			wantErr: []string{"Port", "uint16", `"abc"`},
		},
		"port exceeds uint16": {
			env:     with(apiEnv(), "HTTP_PORT", "70000"),
			load:    loadErr[config.API],
			wantErr: []string{"Port", "out of range", `"70000"`},
		},
		"port is negative": {
			env:     with(workerEnv(), "PG_PORT", "-1"),
			load:    loadErr[config.Worker],
			wantErr: []string{"Port", `"-1"`},
		},
		"unknown log level": {
			env:     with(apiEnv(), "LOG_LEVEL", "nonsense"),
			load:    loadErr[config.API],
			wantErr: []string{"LogLevel", `"nonsense"`},
		},
		"duration without unit": {
			env:     with(workerEnv(), "SHUTDOWN_TIMEOUT", "20"),
			load:    loadErr[config.Worker],
			wantErr: []string{"ShutdownTimeout", "missing unit", `"20"`},
		},
		"duration is not a number": {
			env:     with(apiEnv(), "SHUTDOWN_TIMEOUT", "soon"),
			load:    loadErr[config.API],
			wantErr: []string{"ShutdownTimeout", `"soon"`},
		},
		"worker count is not a number": {
			env:     with(workerEnv(), "WORKER_COUNT", "many"),
			load:    loadErr[config.Worker],
			wantErr: []string{"WorkerCount", `"many"`},
		},
		"redis port exceeds uint16": {
			env:     with(workerEnv(), "REDIS_PORT", "70000"),
			load:    loadErr[config.Worker],
			wantErr: []string{"Port", "out of range", `"70000"`},
		},
		"worker count is negative": {
			env:     with(workerEnv(), "WORKER_COUNT", "-3"),
			load:    loadErr[config.Worker],
			wantErr: []string{"WorkerCount", `"-3"`},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			setEnv(t, test.env)

			err := test.load()

			require.Error(t, err)
			for _, want := range test.wantErr {
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

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, test.cfg.Addr())
		})
	}
}

func TestRedisAddr(t *testing.T) {
	cfg := config.RedisConfig{Host: "::1", Port: 6379, Password: "hidden"}

	assert.Equal(t, "[::1]:6379", cfg.Addr())
}
