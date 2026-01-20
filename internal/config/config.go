// internal/config/config.go
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
	Timeout      time.Duration `yaml:"timeout"`
	MaxRetries   int           `yaml:"max_retries"`
	HealthSamples int          `yaml:"health_samples"`
	WatchInterval time.Duration `yaml:"watch_interval"`
}

func Load(path string) (*Config, error) { // load the config
	data, err := os.ReadFile(path) // read the config file
	if err != nil { // 
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config 
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil { // parse the config
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate required fields - NO hardcoded fallbacks
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
