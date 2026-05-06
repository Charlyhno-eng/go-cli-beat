package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const configFileName = "config.json"

// Config holds the application configuration.
type Config struct {
	MusicDir string `json:"music_dir"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		MusicDir: filepath.Join(home, "Musique", "Techno"),
	}
}

// findConfigPath returns the path to config.json, searching:
//  1. Current working directory  (convenient for `go run .`)
//  2. Directory of the running binary  (normal installed use)
//
// If neither exists, it returns the cwd path so the caller can create it there.
func findConfigPath() string {
	// 1. Current working directory
	if cwd, err := os.Getwd(); err == nil {
		p := filepath.Join(cwd, configFileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// 2. Beside the binary (go build / go install)
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), configFileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Fallback: will be created in cwd on first run
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, configFileName)
}

// Load reads config.json (cwd first, then beside the binary).
// If no file is found, it writes one with default values and returns them.
func Load() (*Config, error) {
	path := findConfigPath()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := DefaultConfig()
		if saveErr := save(cfg, path); saveErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write default config: %v\n", saveErr)
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config.json: %w", err)
	}
	return &cfg, nil
}

// save writes cfg to path as indented JSON.
func save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
