package config

import (
	"os"
	"strings"
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
	Type    string        `yaml:"type"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

type Defaults struct {
	Timeout       time.Duration `yaml:"timeout"`
	HealthSamples int           `yaml:"health_samples"`
	WatchInterval time.Duration `yaml:"watch_interval"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &cfg); err != nil {
		return nil, err
	}

	for i := range cfg.Providers {
		if cfg.Providers[i].Timeout == 0 {
			cfg.Providers[i].Timeout = cfg.Defaults.Timeout
		}
	}
	return &cfg, nil
}

func LoadEnv() {
	data, _ := os.ReadFile(".env")
	for _, line := range strings.Split(string(data), "\n") {
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
}
