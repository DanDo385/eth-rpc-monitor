// Package config provides YAML configuration file loading and validation.
// It handles environment variable expansion, default value application,
// and ensures all required configuration fields are present.
package config

import (
	"fmt"
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

	// Validate required default fields - strict validation, no fallbacks
	if cfg.Defaults.Timeout == 0 {
		return nil, fmt.Errorf("defaults.timeout is required")
	}
	if cfg.Defaults.MaxRetries < 0 {
		return nil, fmt.Errorf("defaults.max_retries must be >= 0")
	}
	if cfg.Defaults.HealthSamples <= 0 {
		return nil, fmt.Errorf("defaults.health_samples is required and must be > 0")
	}
	if cfg.Defaults.WatchInterval == 0 {
		return nil, fmt.Errorf("defaults.watch_interval is required")
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}

	// Apply default timeout to providers that don't specify one
	for i := range cfg.Providers {
		if cfg.Providers[i].Timeout == 0 {
			cfg.Providers[i].Timeout = cfg.Defaults.Timeout
		}
		// Validate provider has required fields
		if cfg.Providers[i].URL == "" {
			return nil, fmt.Errorf("provider %s: url is required", cfg.Providers[i].Name)
		}
	}

	return &cfg, nil
}
