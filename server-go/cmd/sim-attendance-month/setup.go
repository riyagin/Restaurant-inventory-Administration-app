package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"

	"inventory-app/server-go/internal/config"
)

// The simulator owns these throwaway databases. They are dropped and recreated
// on every run so the user's dev database is never touched.
const (
	SimDBWeekly = "attendance_sim_weekly" // pipeline A: nightly job + weekly cumulative import
	SimDBFinal  = "attendance_sim"        // pipeline B: one import, re-evaluate, corrections
)

func dbURL(cfg *config.Config, name string) string {
	return strings.Replace(cfg.DBUrl, "/"+cfg.DBName, "/"+name, 1)
}

// recreateSimDB drops and recreates attendance_sim, then applies all migrations.
func recreateSimDB(ctx context.Context, cfg *config.Config, name string) (string, error) {
	admin, err := pgx.Connect(ctx, dbURL(cfg, "postgres"))
	if err != nil {
		return "", fmt.Errorf("connect postgres db: %w", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		return "", err
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		return "", err
	}
	url := dbURL(cfg, name)
	m, err := migrate.New("file://migrations", url)
	if err != nil {
		return "", fmt.Errorf("migrate init: %w", err)
	}
	// 042 (accounting opening balance) cannot run on an empty database: it
	// inserts a journal entry with zero lines, which the journal constraint
	// rejects. It touches no HR table, so the simulator skips it.
	if err := m.Migrate(41); err != nil && err != migrate.ErrNoChange {
		return "", fmt.Errorf("migrate to 41: %w", err)
	}
	if err := m.Force(42); err != nil {
		return "", fmt.Errorf("skip 42: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return "", fmt.Errorf("migrate up: %w", err)
	}
	m.Close()
	return url, nil
}
