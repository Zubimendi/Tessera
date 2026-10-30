package config

import "os"

type Config struct {
	Host       string
	Port       string
	DatabaseURL string
	RedisAddr  string
	JWTSecret  string
}

func Load() Config {
	return Config{
		Host:       envOrDefault("APP_HOST", "0.0.0.0"),
		Port:       envOrDefault("APP_PORT", "8080"),
		DatabaseURL: envOrDefault("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tessera?sslmode=disable"),
		RedisAddr:  envOrDefault("REDIS_ADDR", "localhost:6379"),
		JWTSecret:  envOrDefault("JWT_SECRET", "dev-secret"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
