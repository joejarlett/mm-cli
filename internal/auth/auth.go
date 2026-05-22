// Package auth handles AuthState on disk and the device-flow login.
// Mirrors the TS src/auth.ts + src/commands/login.ts.
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is the on-disk auth shape. Stored at ~/.config/mm/auth.json (0o600).
// Byte-for-byte compatible with the TS AuthState — same fields, same JSON keys,
// so existing TS-issued auth.json files load cleanly into the Go binary.
type State struct {
	Token     string `json:"token"`
	Prefix    string `json:"prefix"`
	UserID    string `json:"userId"`
	UserName  string `json:"userName"`
	UserEmail string `json:"userEmail"`
	CreatedAt string `json:"createdAt"`
}

// loggedOutMarker is what `mm logout` writes — non-AuthState, treated as null.
type loggedOutMarker struct {
	LoggedOut bool `json:"loggedOut"`
}

// configDir returns ~/.config/mm, creating it (mode 0700) on demand.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "mm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Path returns the absolute path to auth.json, ensuring the dir exists.
func Path() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

// Load reads ~/.config/mm/auth.json. Returns (nil, nil) on missing-or-corrupt
// — silent-null matches the TS behaviour. Also returns nil if the file is the
// `{"loggedOut": true}` marker.
func Load() (*State, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, nil // silent-null on read error
	}

	// Try logged-out marker first.
	var marker loggedOutMarker
	if err := json.Unmarshal(data, &marker); err == nil && marker.LoggedOut {
		return nil, nil
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, nil
	}
	if s.Token == "" {
		return nil, nil
	}
	return &s, nil
}

// Save writes the AuthState to ~/.config/mm/auth.json with mode 0o600.
func Save(s *State) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Write then chmod — covers the case where the file already existed with
	// a wider mode.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Clear writes the logged-out marker to auth.json. Matches TS behaviour of
// preserving the file + dir + mode instead of unlinking.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(`{"loggedOut": true}`), 0o600)
}

// MustLoad returns the AuthState or errors with the standard message used by
// every authenticated command in TS.
func MustLoad() (*State, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("Not authenticated. Run `mm login` first.")
	}
	return s, nil
}
