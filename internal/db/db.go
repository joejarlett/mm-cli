// Package db opens a pgx pool against MM_DATABASE_URL / DATABASE_URL.
// Mirrors src/db.ts. Admin commands only; non-admin commands never touch this.
package db

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mm-cli/internal/config"
)

var pool *pgxpool.Pool

// Pool returns the lazily-initialised pgx pool, or an explanatory error
// if MM_DATABASE_URL / DATABASE_URL isn't set.
func Pool(ctx context.Context) (*pgxpool.Pool, error) {
	if pool != nil {
		return pool, nil
	}
	cfg := config.Load()
	url := cfg.DatabaseURL
	if url == "" {
		return nil, fmt.Errorf("MM_DATABASE_URL or DATABASE_URL not set (env or ~/.mm/.env)")
	}
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	pcfg.MaxConns = 2
	pcfg.MinConns = 0
	pcfg.MaxConnIdleTime = 10 * time.Second
	pcfg.ConnConfig.ConnectTimeout = 5 * time.Second
	// SSL: rejectUnauthorized:false for non-localhost (matches TS).
	if !strings.Contains(url, "localhost") && !strings.Contains(url, "127.0.0.1") {
		pcfg.ConnConfig.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	p, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	pool = p
	return pool, nil
}

// Close shuts the pool down. Called from main.go on exit.
func Close() {
	if pool != nil {
		pool.Close()
		pool = nil
	}
}
