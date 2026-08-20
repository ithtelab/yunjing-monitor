package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vps-agent/internal/server"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	cfg := server.Config{
		Addr:                 env("ADDR", ":3000"),
		AuthSecret:           os.Getenv("AUTH_SECRET"),
		AdminUser:            env("ADMIN_USER", "admin"),
		AdminPass:            os.Getenv("ADMIN_PASS"),
		DataPath:             env("DATA_PATH", "data/server.json"),
		StoreDriver:          os.Getenv("STORE_DRIVER"),
		DBPath:               os.Getenv("DB_PATH"),
		PublicURL:            os.Getenv("PUBLIC_URL"),
		PublicMonitorDetails: envBool("PUBLIC_MONITOR_DETAILS", false),
		CORSOrigins:          envList("CORS_ORIGINS"),
		OfflineWait:          envDuration("OFFLINE_WAIT", 60*time.Second),
		MaxNodes:             envInt("MAX_NODES", 2000),
		UpdateRepo:           env("UPDATE_REPOSITORY", "ithtelab/yunjing-monitor"),
		GitHubToken:          os.Getenv("GITHUB_TOKEN"),
		UpdateEnabled:        envBool("UPDATE_ENABLED", false),
		BackupEncryptionKey:  os.Getenv("BACKUP_ENCRYPTION_KEY"),
		BackupDir:            os.Getenv("BACKUP_DIR"),
		BackupInterval:       envDuration("BACKUP_INTERVAL", 0),
		BackupWebDAVURL:      os.Getenv("BACKUP_WEBDAV_URL"),
		BackupWebDAVUser:     os.Getenv("BACKUP_WEBDAV_USER"),
		BackupWebDAVPassword: os.Getenv("BACKUP_WEBDAV_PASSWORD"),
	}
	server.SetBuildInfo(version, commit, buildTime)

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("center server listening on %s", cfg.Addr)
	serverError := make(chan error, 1)
	go func() { serverError <- srv.ListenAndServe() }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serverError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envList(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
