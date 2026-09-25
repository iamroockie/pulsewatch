package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv          string        `env:"APP_ENV,notEmpty"`
	LogLevel        slog.Level    `env:"LOG_LEVEL,notEmpty"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT,notEmpty"`

	HTTP     HTTPConfig     `envPrefix:"HTTP_"`
	Postgres PostgresConfig `envPrefix:"PG_"`
}

type HTTPConfig struct {
	Host string `env:"HOST,notEmpty"`
	Port uint16 `env:"PORT,notEmpty"`
}

type PostgresConfig struct {
	Host     string `env:"HOST,notEmpty"`
	Port     uint16 `env:"PORT,notEmpty"`
	DB       string `env:"NAME,notEmpty"`
	User     string `env:"USER,notEmpty"`
	Password string `env:"PASS,notEmpty"`
	SSL      string `env:"SSL,notEmpty"`
}

func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}

	return cfg, nil
}

func (c PostgresConfig) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Path:   "/" + c.DB,
	}
	query := url.Values{}
	query.Set("sslmode", c.SSL)
	u.RawQuery = query.Encode()

	return u.String()
}

func (c HTTPConfig) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port)))
}
