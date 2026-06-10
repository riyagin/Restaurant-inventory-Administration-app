package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBUrl      string
	JWTSecret  string
	Port       string
	UploadsDir string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	uploadsDir := getEnv("UPLOADS_DIR", "")
	if uploadsDir == "" {
		exe, err := os.Executable()
		if err == nil {
			uploadsDir = filepath.Join(filepath.Dir(exe), "..", "server", "uploads")
		} else {
			uploadsDir = filepath.Join("..", "server", "uploads")
		}
	}

	cfg := &Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBName:     getEnv("DB_NAME", "inventory_app"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		JWTSecret:  getEnv("JWT_SECRET", ""),
		Port:       getEnv("PORT", "5000"),
		UploadsDir: uploadsDir,
	}

	cfg.DBUrl = fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName,
	)

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
