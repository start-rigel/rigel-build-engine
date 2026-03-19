package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config contains the runtime contract for the build engine service.
type Config struct {
	ServiceName          string
	HTTPPort             string
	LogLevel             string
	PostgresDSN          string
	RedisAddr            string
	BuildEngineMode      string
	InternalServiceToken string
	AdviceMaxConcurrency int
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
}

// Load reads service configuration from environment variables.
func Load() (Config, error) {
	readTimeout, err := durationFromEnv("RIGEL_HTTP_READ_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	writeTimeout, err := durationFromEnv("RIGEL_HTTP_WRITE_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	idleTimeout, err := durationFromEnv("RIGEL_HTTP_IDLE_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		ServiceName:          stringFromEnv("RIGEL_SERVICE_NAME", "rigel-build-engine"),
		HTTPPort:             stringFromEnv("RIGEL_HTTP_PORT", stringFromEnv("RIGEL_BUILD_ENGINE_PORT", "8080")),
		LogLevel:             stringFromEnv("RIGEL_LOG_LEVEL", "info"),
		PostgresDSN:          stringFromEnv("RIGEL_POSTGRES_DSN", ""),
		RedisAddr:            stringFromEnv("RIGEL_REDIS_ADDR", ""),
		BuildEngineMode:      stringFromEnv("RIGEL_BUILD_ENGINE_MODE", "local"),
		InternalServiceToken: stringFromEnv("RIGEL_INTERNAL_SERVICE_TOKEN", ""),
		AdviceMaxConcurrency: intFromEnv("RIGEL_ADVICE_MAX_CONCURRENCY", 4),
		ReadTimeout:          readTimeout,
		WriteTimeout:         writeTimeout,
		IdleTimeout:          idleTimeout,
	}

	if cfg.HTTPPort == "" {
		return Config{}, fmt.Errorf("RIGEL_HTTP_PORT must not be empty")
	}
	if cfg.PostgresDSN == "" {
		return Config{}, fmt.Errorf("RIGEL_POSTGRES_DSN must not be empty")
	}
	if cfg.InternalServiceToken == "" {
		return Config{}, fmt.Errorf("RIGEL_INTERNAL_SERVICE_TOKEN must not be empty")
	}

	return cfg, nil
}

func stringFromEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func intFromEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
