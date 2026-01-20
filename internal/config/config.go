// Package config provides YAML configuration file loading and validation.
// It handles environment variable expansion, default value application,
// and ensures all required configuration fields are present.
package config

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the root configuration structure loaded from YAML.
// It contains provider definitions and default settings that apply to all providers.
type Config struct {
	Providers []Provider `yaml:"providers"` // List of RPC endpoint providers
	Defaults  Defaults   `yaml:"defaults"`  // Default settings for all providers
}

// Provider represents a single Ethereum RPC endpoint configuration.
// Each provider can have its own timeout, or it will inherit from Defaults.
type Provider struct {
	Name    string        `yaml:"name"`              // Provider identifier (e.g., "alchemy", "infura")
	URL     string        `yaml:"url"`               // RPC endpoint URL (supports ${VAR} env expansion)
	Type    string        `yaml:"type"`              // Provider type: "public", "self_hosted", "enterprise" (informational)
	Timeout time.Duration `yaml:"timeout,omitempty"` // Per-provider timeout (optional, uses Defaults.Timeout if not set)
}

// Defaults contains default configuration values that apply to all providers
// unless overridden at the provider level.
type Defaults struct {
	Timeout       time.Duration `yaml:"timeout"`        // HTTP request timeout (e.g., "10s")
	MaxRetries    int           `yaml:"max_retries"`    // Maximum retry attempts (0 = no retries)
	HealthSamples int           `yaml:"health_samples"` // Number of samples for health command (e.g., 30)
	WatchInterval time.Duration `yaml:"watch_interval"` // Refresh interval for monitor command (e.g., "30s")
}

// Validate validates the configuration and applies defaults where appropriate.
// It may emit warnings (to stderr) for suspicious values but does not fail on warnings.
func (c *Config) Validate() error {
	// Validate required default fields - strict validation, no fallbacks
	if c.Defaults.Timeout == 0 {
		return fmt.Errorf("defaults.timeout is required")
	}
	if c.Defaults.MaxRetries < 0 {
		return fmt.Errorf("defaults.max_retries must be >= 0")
	}
	if c.Defaults.HealthSamples <= 0 {
		return fmt.Errorf("defaults.health_samples is required and must be > 0")
	}
	if c.Defaults.WatchInterval == 0 {
		return fmt.Errorf("defaults.watch_interval is required")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	warnTimeout := func(scope string, d time.Duration) {
		const low = 500 * time.Millisecond
		const high = 2 * time.Minute
		if d > 0 && d < low {
			fmt.Fprintf(os.Stderr, "Warning: %s timeout is very low (%s); requests may fail under normal network jitter\n", scope, d)
		}
		if d > high {
			fmt.Fprintf(os.Stderr, "Warning: %s timeout is very high (%s); failures may take a long time to surface\n", scope, d)
		}
	}
	warnTimeout("defaults", c.Defaults.Timeout)

	// Apply default timeout to providers that don't specify one and validate URLs.
	for i := range c.Providers {
		if c.Providers[i].Timeout == 0 {
			c.Providers[i].Timeout = c.Defaults.Timeout
		}
		if c.Providers[i].URL == "" {
			return fmt.Errorf("provider %s: url is required", c.Providers[i].Name)
		}

		u, err := url.Parse(c.Providers[i].URL)
		if err != nil {
			return fmt.Errorf("provider %s: invalid url: %w", c.Providers[i].Name, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("provider %s: invalid url (missing scheme or host)", c.Providers[i].Name)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("provider %s: invalid url scheme %q (expected http or https)", c.Providers[i].Name, u.Scheme)
		}

		warnTimeout(fmt.Sprintf("provider %s", c.Providers[i].Name), c.Providers[i].Timeout)
	}

	return nil
}

// Load reads and parses a YAML configuration file, expanding environment variables
// and validating all required fields. This function enforces strict validation:
// all required fields must be present in the config file (no hardcoded fallbacks).
//
// Parameters:
//   - path: File path to the YAML configuration file
//
// Returns:
//   - *Config: Parsed and validated configuration
//   - error: File read, parse, or validation error
//
// Environment variable expansion:
//
//	URLs can use ${VAR} syntax which will be expanded using os.ExpandEnv().
//	Example: url: ${ALCHEMY_URL} will use the ALCHEMY_URL environment variable.
//
// Validation rules:
//   - defaults.timeout must be set and > 0
//   - defaults.max_retries must be >= 0
//   - defaults.health_samples must be > 0
//   - defaults.watch_interval must be set and > 0
//   - At least one provider must be configured
//   - Each provider must have a non-empty URL
func Load(path string) (*Config, error) {
	// Read configuration file from disk
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Expand environment variables in the YAML content
	// This allows URLs like: url: ${ALCHEMY_URL}
	expanded := os.ExpandEnv(string(data))

	// Parse YAML into Config struct
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}
