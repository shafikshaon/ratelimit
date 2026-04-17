package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DBHost            string
	DBPort            string
	DBUser            string
	DBPassword        string
	DBName            string
	DBSSLMode         string
	RedisAddr         string
	RedisPassword     string
	ScyllaHosts       []string
	ScyllaKeyspace    string
	ServerPort        string
	FingerprintSecret  string
	FingerprintTTLHours int    // how many hours a fingerprint session stays valid
	AllowedOrigins     []string
}

func Load() *Config {
	return &Config{
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         getEnv("DB_PORT", "5432"),
		DBUser:         getEnv("DB_USER", "postgres"),
		DBPassword:     getEnv("DB_PASSWORD", "postgres"),
		DBName:         getEnv("DB_NAME", "ratelimit"),
		DBSSLMode:      getEnv("DB_SSLMODE", "disable"),
		RedisAddr:      getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:  getEnv("REDIS_PASSWORD", ""),
		ScyllaHosts:    strings.Split(getEnv("SCYLLA_HOSTS", "localhost"), ","),
		ScyllaKeyspace:    getEnv("SCYLLA_KEYSPACE", "ratelimit"),
		ServerPort:        getEnv("SERVER_PORT", "8080"),
		FingerprintSecret:   getEnv("FINGERPRINT_SECRET", "change-me-in-production-32-chars!!"),
		FingerprintTTLHours: getEnvInt("FINGERPRINT_TTL_HOURS", 24),
		AllowedOrigins:      strings.Split(getEnv("ALLOWED_ORIGINS", "http://localhost:8080"), ","),
	}
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultVal
}
