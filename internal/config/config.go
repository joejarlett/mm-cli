// Package config holds env-driven configuration for mm-cli.
// Mirrors the TS src/config.ts struct 1:1.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Config is the loaded environment. One instance per process, cached.
type Config struct {
	// HubURL is the base URL of the meta-me hub. Used for /api/mm,
	// /api/stt, /api/tts. Override via MM_HUB_URL.
	HubURL string
	// AuthURL is the base URL of the auth service. Used by the device-flow
	// login. Override via MM_AUTH_URL.
	AuthURL string
	// LocalAgentURL is the base URL of the local agent (mm chat + mm project).
	// Override via MM_LOCAL_AGENT_URL.
	LocalAgentURL string
	// DatabaseURL is the optional Postgres connection string for admin
	// commands. Read from MM_DATABASE_URL or DATABASE_URL.
	DatabaseURL string
}

var loaded *Config

// Load returns the singleton config, loading on first call. Honours
// ~/.mm/.env (explicit env wins).
func Load() *Config {
	if loaded != nil {
		return loaded
	}
	maybeLoadUserEnv()
	loaded = &Config{
		HubURL:        envOr("MM_HUB_URL", "https://meta-me.uk"),
		AuthURL:       envOr("MM_AUTH_URL", "https://auth.meta-me.uk"),
		LocalAgentURL: envOr("MM_LOCAL_AGENT_URL", "http://localhost:3142"),
		DatabaseURL:   envOrFallback("MM_DATABASE_URL", "DATABASE_URL", ""),
	}
	return loaded
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrFallback(primary, secondary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(secondary); v != "" {
		return v
	}
	return fallback
}

// maybeLoadUserEnv is the Go translation of src/config.ts's maybeLoadUserEnv.
// Reads ~/.mm/.env line-by-line, sets each KEY=VALUE if KEY is not already
// in the process env. No override — explicit env wins.
func maybeLoadUserEnv() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".mm", ".env")
	f, err := os.Open(path)
	if err != nil {
		return // file absent or unreadable — fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(line[:eq], "export "))
		value := strings.TrimSpace(line[eq+1:])
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			continue
		}
		if _, present := os.LookupEnv(key); present {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
