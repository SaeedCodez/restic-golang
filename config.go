package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
)

// Config holds every setting the app needs to talk to a restic repository.
// It is persisted as a small JSON file in the working directory.
//
// Note: this is a single-user, local tool, so the repository password and the
// S3 secret key are stored in plaintext in the config file. That is acceptable
// for a local demo but would not be appropriate for a multi-user deployment.
type Config struct {
	// BackendType selects how the repository is addressed: "S3" or "Local".
	BackendType string `json:"backendType"`

	// S3 backend settings.
	Endpoint  string `json:"endpoint"`  // e.g. https://s3.amazonaws.com or https://play.min.io
	Bucket    string `json:"bucket"`    // bucket name
	Region    string `json:"region"`    // optional, e.g. us-east-1
	AccessKey string `json:"accessKey"` // AWS_ACCESS_KEY_ID
	SecretKey string `json:"secretKey"` // AWS_SECRET_ACCESS_KEY

	// Local backend settings.
	LocalPath string `json:"localPath"` // a directory that holds the restic repository

	// Common: the restic repository password used for encryption (required).
	Password string `json:"password"`
}

// Repository returns the restic repository string for the active backend,
// e.g. "s3:https://s3.amazonaws.com/my-bucket" or "/path/to/local/repo".
func (c *Config) Repository() (string, error) {
	switch c.BackendType {
	case "S3":
		ep := strings.TrimSpace(c.Endpoint)
		bucket := strings.TrimSpace(c.Bucket)
		if ep == "" || bucket == "" {
			return "", errors.New("S3 endpoint and bucket are both required")
		}
		ep = strings.TrimSuffix(ep, "/")
		return "s3:" + ep + "/" + bucket, nil
	case "Local":
		p := strings.TrimSpace(c.LocalPath)
		if p == "" {
			return "", errors.New("a local repository directory path is required")
		}
		return p, nil
	default:
		return "", errors.New(`backend type must be "S3" or "Local"`)
	}
}

// Validate checks that the configuration is complete enough to run restic.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Password) == "" {
		return errors.New("a repository password is required (set it in Settings)")
	}
	_, err := c.Repository()
	return err
}

// Env builds the environment for a restic command. Credentials are passed here,
// via the process environment, and never on the command line. We start from the
// current process environment (so PATH etc. are preserved) but strip any keys we
// are about to set, so our values are unambiguously the ones restic sees.
func (c *Config) Env() []string {
	managed := map[string]bool{
		"RESTIC_PASSWORD":       true,
		"RESTIC_REPOSITORY":     true,
		"AWS_ACCESS_KEY_ID":     true,
		"AWS_SECRET_ACCESS_KEY": true,
		"AWS_DEFAULT_REGION":    true,
	}

	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 && managed[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}

	env = append(env, "RESTIC_PASSWORD="+c.Password)
	if c.BackendType == "S3" {
		env = append(env, "AWS_ACCESS_KEY_ID="+c.AccessKey)
		env = append(env, "AWS_SECRET_ACCESS_KEY="+c.SecretKey)
		if strings.TrimSpace(c.Region) != "" {
			env = append(env, "AWS_DEFAULT_REGION="+c.Region)
		}
	}
	return env
}

// ConfigStore persists a Config to disk and guards concurrent access.
type ConfigStore struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// loadConfigStore reads the config file at path. If the file does not exist a
// sensible default config is created in memory (and written on the first save).
func loadConfigStore(path string) (*ConfigStore, error) {
	s := &ConfigStore{path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.cfg = defaultConfig()
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return nil, err
	}
	if s.cfg.BackendType == "" {
		s.cfg.BackendType = "Local"
	}
	return s, nil
}

// defaultConfig returns a config that lets the demo run locally with the least
// possible setup: a Local backend pointing at ./restic-repo. The user still has
// to choose a password in Settings before backing up.
func defaultConfig() Config {
	return Config{
		BackendType: "Local",
		LocalPath:   "./restic-repo",
	}
}

// Get returns a copy of the current config (safe to read without locking).
func (s *ConfigStore) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Set replaces the config and writes it to disk atomically.
func (s *ConfigStore) Set(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file then rename, so a crash never leaves a half-written config.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.cfg = c
	return nil
}
