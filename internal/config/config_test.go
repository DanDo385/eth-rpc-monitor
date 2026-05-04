package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_minimalAndTimeoutDefault(t *testing.T) {
	t.Setenv("ETH_RPC_MONITOR_URLX", "https://example.com/v1")

	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	content := `defaults:
  timeout: 5s
  health_samples: 3
  watch_interval: 1s
providers:
  - name: a
    url: ${ETH_RPC_MONITOR_URLX}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "a" {
		t.Fatalf("providers: %+v", cfg.Providers)
	}
	if cfg.Providers[0].URL != "https://example.com/v1" {
		t.Fatalf("expand url: %q", cfg.Providers[0].URL)
	}
	if cfg.Providers[0].Timeout != 5*time.Second {
		t.Fatalf("timeout: %v", cfg.Providers[0].Timeout)
	}
}

func TestLoadEnv_setsVariables(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ETH_RPC_MONITOR_LOADENV_K=v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old, had := os.LookupEnv("ETH_RPC_MONITOR_LOADENV_K")
	t.Cleanup(func() {
		if had {
			os.Setenv("ETH_RPC_MONITOR_LOADENV_K", old)
		} else {
			os.Unsetenv("ETH_RPC_MONITOR_LOADENV_K")
		}
	})
	os.Unsetenv("ETH_RPC_MONITOR_LOADENV_K")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	LoadEnv()
	if os.Getenv("ETH_RPC_MONITOR_LOADENV_K") != "v1" {
		t.Fatalf("got %q", os.Getenv("ETH_RPC_MONITOR_LOADENV_K"))
	}
}
