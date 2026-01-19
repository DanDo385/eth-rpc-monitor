package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Providers []Provider `yaml:"providers"`
	Defaults  Defaults   `yaml:"defaults"`
}

type Provider struct {
	Name    string        `yaml:"name"`
	URL     string        `yaml:"url"`
	Type    string        `yaml:"type"` // "paid" or "free"
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

type Defaults struct {
	Timeout    time.Duration `yaml:"timeout"`
	MaxRetries int           `yaml:"max_retries"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate required fields - NO hardcoded fallbacks
	if cfg.Defaults.Timeout == 0 {
		return nil, fmt.Errorf("defaults.timeout is required")
	}
	if cfg.Defaults.MaxRetries == 0 {
		return nil, fmt.Errorf("defaults.max_retries is required")
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}

	// Apply defaults to providers
	for i := range cfg.Providers {
		if cfg.Providers[i].Timeout == 0 {
			cfg.Providers[i].Timeout = cfg.Defaults.Timeout
		}
		if cfg.Providers[i].URL == "" {
			return nil, fmt.Errorf("provider %s: url is required", cfg.Providers[i].Name)
		}
	}

	return &cfg, nil
}
