// =============================================================================
// FILE: internal/config/config.go
// ROLE: Configuration Layer — Loading and Validating Provider Settings
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file is the first thing that runs in every command. Before any RPC
// calls are made, before any goroutines are spawned, the configuration must
// be loaded. It reads a YAML file (config/providers.yaml), expands any
// environment variables in the URLs (so API keys stay out of source control),
// and produces a Config struct that every other component consumes.
//
// ARCHITECTURE POSITION
// =====================
//
//   ┌──────────────────────────────────────────┐
//   │         .env file (optional)             │
//   │   ALCHEMY_API_KEY=abc123                 │
//   └───────────┬──────────────────────────────┘
//               │  LoadEnv() reads and sets
//               ▼  environment variables
//   ┌──────────────────────────────────────────┐
//   │     config/providers.yaml                │
//   │   url: .../${ALCHEMY_API_KEY}            │
//   └───────────┬──────────────────────────────┘
//               │  Load() reads, expands, parses
//               ▼
//   ┌──────────────────────────────────────────┐
//   │     Config struct (in memory)            │
//   │   Providers: [{name, url, timeout}, ...] │
//   │   Defaults:  {timeout, samples, interval}│
//   └───────────┬──────────────────────────────┘
//               │  Passed to every command
//               ▼
//   ┌──────────────────────────────────────────┐
//   │   cmd/block, cmd/test, cmd/snapshot,     │
//   │   cmd/monitor                            │
//   └──────────────────────────────────────────┘
//
// DESIGN DECISIONS
// ================
// 1. YAML OVER JSON: YAML supports comments (JSON doesn't), which is valuable
//    for a config file where users need to annotate provider details.
//
// 2. ENVIRONMENT VARIABLE EXPANSION: API keys should never be hardcoded in
//    config files that might be committed to source control. The `${VAR}`
//    syntax lets users reference environment variables in their URLs.
//
// 3. DEFAULT TIMEOUT INHERITANCE: Providers without an explicit timeout
//    inherit from defaults.timeout. This follows the DRY principle — define
//    the common case once, override only when needed.
//
// 4. SINGLE SOURCE OF TRUTH: One YAML file configures all four commands.
//    Adding a provider once makes it available everywhere.
//
// CS CONCEPTS: CONFIGURATION AS DATA
// ====================================
// Configuration files are a form of DECLARATIVE programming — you describe
// WHAT you want (which providers, what timeouts) rather than HOW to do it
// (the code handles the how). This separation means:
//   - Non-programmers can modify behavior without touching Go code
//   - The same binary works with different provider sets
//   - Secrets (API keys) are managed separately from code
//
// The YAML file is deserialized into Go structs using struct tags
// (`yaml:"field_name"`), which tell the YAML parser how to map YAML keys
// to struct fields — the same concept as JSON tags but for YAML format.
// =============================================================================

package config

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// SECTION 1: Configuration Types
// =============================================================================

// Config is the top-level configuration struct, representing the entire
// contents of providers.yaml.
//
// In YAML, this maps to:
//
//   defaults:
//     timeout: 10s
//     ...
//   providers:
//     - name: alchemy
//       ...
//
// STRUCT TAG: `yaml:"providers"`
// ==============================
// The struct tag tells the YAML parser: "the Providers field in Go corresponds
// to the 'providers' key in the YAML file." Without this tag, the YAML parser
// would look for a key matching the Go field name exactly ("Providers" with
// a capital P), which wouldn't match the lowercase YAML convention.
//
// SLICE TYPE: []Provider
// ======================
// Providers is a SLICE of Provider structs (not an array). In Go:
//   - An array has a fixed size: [4]Provider (always 4 elements)
//   - A slice has a dynamic size: []Provider (0 or more elements)
//
// Under the hood, a slice is a small struct (24 bytes on 64-bit systems):
//
//   ┌──────────────┐
//   │ Slice header  │
//   │  ptr: ────────┼──▶ [Provider0, Provider1, Provider2, Provider3]
//   │  len: 4       │     (underlying array on the heap)
//   │  cap: 4       │
//   └──────────────┘
//
// The YAML parser creates this slice dynamically as it reads each `- name: ...`
// entry in the YAML list.
type Config struct {
	Providers []Provider `yaml:"providers"` // List of RPC providers to monitor
	Defaults  Defaults   `yaml:"defaults"`  // Default settings (timeout, samples, interval)
}

// Provider represents a single Ethereum RPC endpoint configuration.
//
// Example YAML:
//
//   - name: alchemy
//     url: https://eth-mainnet.g.alchemy.com/v2/${ALCHEMY_API_KEY}
//     type: public
//     timeout: 15s    # optional — overrides default
//
// TIMEOUT AND `omitempty`
// =======================
// The `omitempty` tag on Timeout means: if the YAML file doesn't include a
// timeout for this provider, the field stays at its zero value (0).
// The Load() function then detects this zero value and fills in the default.
//
// time.Duration is an int64 under the hood (nanoseconds). Its zero value is
// 0 nanoseconds, which we use as a sentinel meaning "not set." The YAML
// parser can parse strings like "10s", "500ms", "1m" directly into
// time.Duration values — a very convenient feature of the yaml.v3 library.
//
// TYPE FIELD
// ==========
// The Type field ("public", "self_hosted", "enterprise") is informational
// only — it appears in test output but does NOT change any behavior.
// It exists for human operators to understand their provider landscape.
type Provider struct {
	Name    string        `yaml:"name"`             // Identifier (e.g., "alchemy", "infura")
	URL     string        `yaml:"url"`              // Full RPC endpoint URL (env vars expanded)
	Type    string        `yaml:"type"`             // Informational: "public", "self_hosted", "enterprise"
	Timeout time.Duration `yaml:"timeout,omitempty"` // Per-provider timeout override; 0 = use default
}

// Defaults holds the default settings shared across all commands.
//
// These values are used when a command doesn't receive an explicit override
// via command-line flags:
//   - Timeout:       Used for RPC request timeouts (default: 10s)
//   - HealthSamples: Number of samples in the `test` command (default: 30)
//   - WatchInterval: Refresh interval in the `monitor` command (default: 30s)
type Defaults struct {
	Timeout       time.Duration `yaml:"timeout"`        // Default request timeout
	HealthSamples int           `yaml:"health_samples"` // Default samples for health test
	WatchInterval time.Duration `yaml:"watch_interval"` // Default monitor refresh interval
}

// =============================================================================
// SECTION 2: Load — Reading, Expanding, and Parsing Configuration
// =============================================================================

// Load reads a YAML configuration file and returns a fully-populated Config.
//
// This function performs three operations in sequence:
//   1. READ:   Load the raw file bytes from disk
//   2. EXPAND: Replace ${VAR} patterns with environment variable values
//   3. PARSE:  Deserialize the YAML text into Go structs
//   4. DEFAULT: Fill in missing per-provider timeouts from the defaults
//
// RETURN TYPE: (*Config, error)
// =============================
// Returns *Config (a POINTER to Config), not Config (a value). This is
// the idiomatic Go pattern for "constructors" that can fail:
//
//   - On success: returns a pointer to the heap-allocated Config, nil error
//   - On failure: returns nil pointer, non-nil error
//
// Why a pointer? Two reasons:
//   1. Config contains a slice (Providers), which the caller will iterate.
//      Returning by value would copy the slice header (cheap) but is
//      unconventional for "loaded data" that lives for the program's lifetime.
//   2. Returning nil on error is cleaner than returning an empty Config{}.
//      Callers can check: if cfg == nil { handle error }.
//
// DETAILED WALKTHROUGH OF EACH STEP
// ==================================
//
// Step 1 — os.ReadFile(path):
//   Reads the entire file into a []byte. If the file doesn't exist or
//   can't be read, returns an error immediately. The file path comes
//   from the --config flag (default: "config/providers.yaml").
//
// Step 2 — os.ExpandEnv(string(data)):
//   Scans the file content for ${VAR} or $VAR patterns and replaces them
//   with the corresponding environment variable values. For example:
//
//     Before: url: https://eth-mainnet.g.alchemy.com/v2/${ALCHEMY_API_KEY}
//     After:  url: https://eth-mainnet.g.alchemy.com/v2/abc123def456
//
//   If the environment variable is not set, the pattern is replaced with
//   an empty string. This is a security-conscious design — API keys live
//   in the environment (or .env file), not in the YAML file.
//
// Step 3 — yaml.Unmarshal(..., &cfg):
//   IMPORTANT: The &cfg passes the ADDRESS of cfg to the YAML unmarshaler.
//
//   &cfg — the `&` (address-of) operator:
//     - `cfg` is a local variable of type Config (a value on the stack)
//     - `&cfg` is the memory address of that variable
//     - Unmarshal needs the address so it can WRITE INTO cfg's fields
//     - Without &, Unmarshal would receive a COPY and our cfg stays empty
//
//   In memory:
//
//     Stack                           After Unmarshal fills it in:
//     ┌───────────────┐               ┌──────────────────────────┐
//     │ cfg (Config)  │               │ cfg (Config)             │
//     │  Providers: []│               │  Providers: [{alchemy},  │
//     │  Defaults: {} │               │              {infura},...│]
//     └───────────────┘               │  Defaults: {10s, 30, 30s}│
//          ▲                          └──────────────────────────┘
//          │                               ▲
//     &cfg (passed to Unmarshal)       &cfg (same address)
//
// Step 4 — Default timeout inheritance:
//   After parsing, iterate through providers. Any provider with Timeout == 0
//   (meaning "not set in YAML") gets the default timeout.
//
//   NOTE: `for i := range cfg.Providers` uses an INDEX loop, not a
//   value-copy loop. This is critical:
//
//     for i := range cfg.Providers {
//         cfg.Providers[i].Timeout = ...  // ← Modifies the ACTUAL slice element
//     }
//
//   vs. the WRONG way:
//
//     for _, p := range cfg.Providers {
//         p.Timeout = ...  // ← Modifies a COPY — original unchanged!
//     }
//
//   In the value-copy form, `p` is a new variable that receives a copy of
//   each Provider. Modifying `p` doesn't affect the original in the slice.
//   Using the index form (cfg.Providers[i]) accesses the actual element
//   in the slice's underlying array.
//
// Step 5 — return &cfg, nil:
//   The `&` takes the address of the local cfg variable. Go's escape analysis
//   detects that this address is being returned, so cfg is allocated on the
//   heap (not the stack) to ensure it outlives the function call.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), &cfg); err != nil {
		return nil, err
	}

	// Apply default timeout to any provider that doesn't specify one.
	// Uses index-based iteration to modify the original slice elements.
	for i := range cfg.Providers {
		if cfg.Providers[i].Timeout == 0 {
			cfg.Providers[i].Timeout = cfg.Defaults.Timeout
		}
	}
	return &cfg, nil
}

// =============================================================================
// SECTION 3: LoadEnv — Loading Environment Variables from .env Files
// =============================================================================

// LoadEnv reads a .env file from the current working directory and sets
// each KEY=VALUE pair as an environment variable.
//
// .env files are a convention for storing secrets during development.
// Instead of exporting variables in your shell profile, you put them in
// a .env file (which is .gitignored) and the application loads them.
//
// Example .env file:
//
//   ALCHEMY_API_KEY=abc123def456
//   INFURA_API_KEY=xyz789
//
// After LoadEnv runs, os.Getenv("ALCHEMY_API_KEY") returns "abc123def456",
// and the Load() function's os.ExpandEnv() call will substitute it into URLs.
//
// ERROR HANDLING: Deliberately permissive
// ========================================
// Notice that os.ReadFile errors are silently ignored (the _ discard):
//
//   data, _ := os.ReadFile(".env")
//
// This is intentional — the .env file is OPTIONAL. In production, environment
// variables come from the deployment environment (Docker, Kubernetes, etc.),
// not from a .env file. If the file doesn't exist, ReadFile returns an error
// and empty data, the Split produces no valid lines, and nothing happens.
//
// PARSING STRATEGY
// ================
// strings.SplitN(line, "=", 2) splits each line on the FIRST "=" only.
// The `2` means "produce at most 2 parts." This correctly handles values
// that contain "=" characters:
//
//   Input:  "API_KEY=abc=123=xyz"
//   Parts:  ["API_KEY", "abc=123=xyz"]  ← value includes the extra "="
//
// If SplitN used a limit greater than 2, the value would be incorrectly
// split into multiple parts.
func LoadEnv() {
	data, _ := os.ReadFile(".env")
	for _, line := range strings.Split(string(data), "\n") {
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
}
