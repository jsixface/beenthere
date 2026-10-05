// Package config loads runtime configuration from the environment, using the
// same variable names as the Rails app so existing deployments can be reused.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	Addr        string
	DatabaseURL string
	SecretKey   string
	MaxUploadMB int64
	Workers     int
	PoolMax     int32
}

// LoadDB loads only the database settings (for CLI subcommands).
func LoadDB() (Config, error) {
	c, err := load()
	if err != nil && c.DatabaseURL != "" {
		return c, nil
	}
	return c, err
}

func Load() (Config, error) {
	c, err := load()
	if err == nil && c.SecretKey == "" {
		return c, fmt.Errorf("SECRET_KEY_BASE must be set (used to sign session cookies)")
	}
	return c, err
}

func load() (Config, error) {
	c := Config{
		Addr:        ":" + env("PORT", "3000"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		SecretKey:   os.Getenv("SECRET_KEY_BASE"),
		MaxUploadMB: envInt("MAX_UPLOAD_MB", 1024),
		Workers:     int(envInt("WORKERS", 2)),
		PoolMax:     int32(envInt("DB_POOL", 10)),
	}
	if c.DatabaseURL == "" {
		u := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(env("DATABASE_USERNAME", "postgres"), os.Getenv("DATABASE_PASSWORD")),
			Host:   fmt.Sprintf("%s:%s", env("DATABASE_HOST", "localhost"), env("DATABASE_PORT", "5432")),
			Path:   env("DATABASE_NAME", "dawarich_development"),
		}
		c.DatabaseURL = u.String() + "?sslmode=" + env("DATABASE_SSLMODE", "prefer")
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(k), 10, 64); err == nil && v > 0 {
		return v
	}
	return def
}
