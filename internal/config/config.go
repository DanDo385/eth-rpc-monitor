package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ProviderType indicates the deployment model of the RPC endpoint
type ProviderType string

const (
	ProviderTypePublic     ProviderType = "public"
	ProviderTypeEnterprise ProviderType = "enterprise"
	ProviderTypeSelfHosted ProviderType = "self_hosted"
)

// Provider represents a single RPC endpoint configuration
type Provider struct {
	Name    string        `yaml:"name"`
	URL     string        `yaml:"url"`
	Type    ProviderType  `yaml:"type"`
	Timeout time.Duration `yaml:"timeout"`
}

// Config holds the complete application configuration
type Config struct {
	Providers []Provider `yaml:"providers"`
	Defaults  Defaults   `yaml:"defaults"`
}

// Defaults holds default values for providers
type Defaults struct {
	Timeout        time.Duration `yaml:"timeout"`
	MaxRetries     int           `yaml:"max_retries"`
	BackoffInitial time.Duration `yaml:"backoff_initial"`
	BackoffMax     time.Duration `yaml:"backoff_max"`
}

// Load reads and parses a config file from the given path
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults to providers that don't have explicit values
	for i := range cfg.Providers {
		if cfg.Providers[i].Timeout == 0 {
			cfg.Providers[i].Timeout = cfg.Defaults.Timeout
		}
		if cfg.Providers[i].Type == "" {
			cfg.Providers[i].Type = ProviderTypePublic
		}
	}

	// Set sensible defaults if not specified
	if cfg.Defaults.Timeout == 0 {
		cfg.Defaults.Timeout = 10 * time.Second
	}
	if cfg.Defaults.MaxRetries == 0 {
		cfg.Defaults.MaxRetries = 3
	}
	if cfg.Defaults.BackoffInitial == 0 {
		cfg.Defaults.BackoffInitial = 100 * time.Millisecond
	}
	if cfg.Defaults.BackoffMax == 0 {
		cfg.Defaults.BackoffMax = 5 * time.Second
	}

	return &cfg, nil
}

// Validate checks the config for required fields and valid values
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}

	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("provider %d: name is required", i)
		}
		if p.URL == "" {
			return fmt.Errorf("provider %s: url is required", p.Name)
		}
	}

	return nil
}
