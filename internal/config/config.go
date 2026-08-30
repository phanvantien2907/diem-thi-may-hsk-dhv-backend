package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration values.
type Config struct {
	// Server
	AppPort string
	AppEnv  string

	// Database
	DatabaseURL       string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration

	// JWT
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration

	// Rate Limit
	RateLimitRPS   int
	RateLimitBurst int

	// Cloudinary
	CloudinaryURL string

	// CORS
	CORSAllowedOrigins []string

	// Webhook
	WebhookSecret string
}

// Load reads configuration from the .env file and environment variables.
// Environment variables take precedence over .env file values.
func Load() (*Config, error) {
	// Attempt to load .env file; not fatal if missing in production.
	if err := godotenv.Load(); err != nil {
		log.Println("[config] .env file not found, relying on environment variables")
	}

	cfg := &Config{
		AppPort:       getEnv("APP_PORT", "8080"),
		AppEnv:        getEnv("APP_ENV", "production"),
		DatabaseURL:   mustGetEnv("DATABASE_URL"),
		JWTSecret:     mustGetEnv("JWT_SECRET"),
		CloudinaryURL: mustGetEnv("CLOUDINARY_URL"),
		WebhookSecret: getEnv("WEBHOOK_SECRET", ""),
		CORSAllowedOrigins: strings.Split(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173"), ","),
	}

	var err error

	cfg.DBMaxOpenConns, err = strconv.Atoi(getEnv("DB_MAX_OPEN_CONNS", "25"))
	if err != nil {
		return nil, fmt.Errorf("invalid DB_MAX_OPEN_CONNS: %w", err)
	}

	cfg.DBMaxIdleConns, err = strconv.Atoi(getEnv("DB_MAX_IDLE_CONNS", "5"))
	if err != nil {
		return nil, fmt.Errorf("invalid DB_MAX_IDLE_CONNS: %w", err)
	}

	cfg.DBConnMaxLifetime, err = time.ParseDuration(getEnv("DB_CONN_MAX_LIFETIME", "5m"))
	if err != nil {
		return nil, fmt.Errorf("invalid DB_CONN_MAX_LIFETIME: %w", err)
	}

	cfg.JWTAccessTTL, err = time.ParseDuration(getEnv("JWT_ACCESS_TTL", "24h"))
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_ACCESS_TTL: %w", err)
	}

	cfg.JWTRefreshTTL, err = time.ParseDuration(getEnv("JWT_REFRESH_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_REFRESH_TTL: %w", err)
	}

	cfg.RateLimitRPS, err = strconv.Atoi(getEnv("RATE_LIMIT_RPS", "10"))
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_RPS: %w", err)
	}

	cfg.RateLimitBurst, err = strconv.Atoi(getEnv("RATE_LIMIT_BURST", "20"))
	if err != nil {
		return nil, fmt.Errorf("invalid RATE_LIMIT_BURST: %w", err)
	}

	return cfg, nil
}

// getEnv returns the value of the env variable or the fallback default.
func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// mustGetEnv panics if the required env variable is not set.
func mustGetEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		log.Fatalf("[config] required environment variable %q is not set", key)
	}
	return v
}
