# Ethereum RPC Monitor

A lightweight Go tool for monitoring Ethereum RPC endpoint performance and reliability. Built for teams who treat blockchain infrastructure as production-critical.

## What This Project Does

This tool answers four questions about your Ethereum RPC providers:

1. **`block`** — "What does the latest block look like, and who served it fastest?"
2. **`test`** — "How reliable and fast is each provider over N samples?"
3. **`snapshot`** — "Do all providers agree on the current state of the blockchain?"
4. **`monitor`** — "What is happening right now, continuously?"

Each question is a separate binary. All four share the same configuration file and internal libraries.

## How the System Works

### The Big Picture

Ethereum applications talk to the blockchain through **RPC (Remote Procedure Call) endpoints** — HTTP APIs provided by services like Alchemy, Infura, or self-hosted nodes. These endpoints accept JSON-RPC requests ("What is the latest block number?") and return JSON responses ("0x1444F3B").

This tool measures the **performance** and **consistency** of those endpoints. Performance means latency (how fast they respond). Consistency means agreement (do they all return the same data for the same query?).

### Core Computer Science Concepts

Reading this codebase will expose you to the following concepts, all applied in a real production context:

- **Concurrency and Goroutines** — Every command queries multiple providers simultaneously using Go's goroutines and `errgroup` for structured concurrency. You'll see mutexes protecting shared state, contexts propagating cancellation, and closures capturing loop variables.

- **Pointers, Addresses, and Indirection** — Go uses pointers extensively. Every `*` and `&` in the code is documented with memory diagrams showing what exists on the stack vs. the heap, what gets copied vs. shared, and why the choice matters.

- **Network I/O and HTTP** — The RPC client builds JSON-RPC requests, sends them over HTTP POST, and measures round-trip latency. You'll see how Go's `net/http` manages connection pooling, how contexts abort in-flight requests, and how `defer` ensures cleanup.

- **Data Transformation Pipelines** — Block data flows through three representations: hex strings from the wire, native Go types for logic, and formatted strings for display. Each transformation is explicit and documented.

- **Statistical Analysis** — The `test` command computes percentiles (P50, P95, P99) using the nearest-rank method. The comments explain why percentiles are superior to averages for latency analysis.

- **Event Loops and Signal Handling** — The `monitor` command implements a `select`-based event loop with ticker-driven periodic refresh and OS signal handling for graceful shutdown.

### Data Flow Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Configuration Loading                     │
│  .env file → LoadEnv() → os.Setenv()                       │
│  providers.yaml → os.ExpandEnv() → yaml.Unmarshal → Config │
└──────────────────────┬──────────────────────────────────────┘
                       │
              ┌────────▼─────────────────────────────────────┐
              │              RPC Client Creation              │
              │  For each provider: NewClient(name, url, to)  │
              └────────┬─────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┬──────────────┐
        │              │              │              │
        ▼              ▼              ▼              ▼
   ┌─────────┐  ┌───────────┐  ┌──────────┐  ┌──────────┐
   │  block  │  │   test    │  │ snapshot │  │ monitor  │
   │ command │  │  command  │  │ command  │  │ command  │
   └────┬────┘  └─────┬─────┘  └────┬─────┘  └────┬─────┘
        │             │              │              │
        ▼             ▼              ▼              ▼
   Select fastest  N samples    Same block     Ticker loop
   → fetch block   per provider  all providers  → fetch all
   → display       → percentiles → compare      → display
                   → display      hashes/heights → repeat
        │             │              │              │
        └──────────┬──┴──────────────┴──────────────┘
                   │
          ┌────────▼──────────┐
          │  RPC Client.Call  │
          │  JSON-RPC over    │
          │  HTTP POST        │
          │  Latency measured │
          └────────┬──────────┘
                   │
          ┌────────▼──────────┐
          │  Ethereum Node    │
          │  (remote)         │
          └───────────────────┘
```

### Reading Order

For the best learning experience, read the source files in this order:

1. **`internal/rpc/types.go`** — Start here. Defines every data structure in the system. Introduces the two-layer type model (hex wire format vs. typed values) and explains pointer types with memory diagrams.

2. **`internal/rpc/format.go`** — Hex parsing and human-readable formatting. Covers number bases, arbitrary-precision arithmetic with `big.Int`, and Go's time formatting reference date.

3. **`internal/rpc/client.go`** — The HTTP JSON-RPC client. Shows how requests are serialized, latency is measured, and responses are deserialized. Extensive `&` and `*` documentation with memory diagrams.

4. **`internal/config/config.go`** — YAML configuration loading with environment variable expansion. Demonstrates the `&` address-of operator in `yaml.Unmarshal`, index-based loop iteration for mutation, and Go's escape analysis.

5. **`internal/format/colors.go`** — Terminal color handling. Explains ANSI escape codes, the padding problem with colored strings, and Go closures.

6. **`internal/format/block.go`** — Single-block display. Shows the `io.Writer` pattern for testable output and pointer parameter semantics.

7. **`internal/format/test.go`** — Percentile calculation and test results table. Covers the nearest-rank method, defensive slice copying, and height mismatch detection.

8. **`internal/format/snapshot.go`** — Fork detection display. Explains the `error` interface, map-based grouping, and consensus analysis.

9. **`internal/format/monitor.go`** — Live dashboard rendering. Covers ANSI screen clearing and relative lag calculation.

10. **`cmd/block/main.go`** — Block inspector. Demonstrates concurrent provider selection, context timeouts, flag parsing with pointer returns, error wrapping, and JSON export.

11. **`cmd/test/main.go`** — Health check. Shows concurrent sampling with `errgroup`, warm-up methodology, and inter-sample delays.

12. **`cmd/snapshot/main.go`** — Fork detection. The simplest command — a complete concurrent pipeline in a single `main()` function.

13. **`cmd/monitor/main.go`** — Continuous monitor. The most architecturally rich file: event loops, signal handling, channels, tickers, context cancellation, and closures with mutable captured state.

## Installation

### Prerequisites
- Go 1.24 or later
- Ethereum RPC endpoint URLs (paid or free)

### Build from source

```bash
git clone https://github.com/dando385/eth-rpc-monitor
cd eth-rpc-monitor
make build
```

Or build manually:

```bash
go build -o bin/block ./cmd/block
go build -o bin/test ./cmd/test
go build -o bin/snapshot ./cmd/snapshot
go build -o bin/monitor ./cmd/monitor
```

All binaries will be placed in the `bin/` directory.

## Configuration

Create or edit `config/providers.yaml` (copy from `config/providers.yaml.example`):

```yaml
defaults:
  timeout: 10s
  health_samples: 30      # Default samples for test command
  watch_interval: 30s     # Default refresh interval for monitor command

providers:
  # Alchemy – managed public RPC
  - name: alchemy
    url: https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY
    type: public

  # Infura – ConsenSys managed public RPC
  - name: infura
    url: https://mainnet.infura.io/v3/YOUR_API_KEY
    type: public

  # LlamaNodes – community public RPC
  - name: llamanodes
    url: https://eth.llamarpc.com
    type: public

  # PublicNode – community public RPC
  - name: publicnode
    url: https://ethereum-rpc.publicnode.com
    type: public

  # Example self-hosted (commented)
  # - name: local-geth
  #   url: http://localhost:8545
  #   type: self_hosted
  #   timeout: 5s

  # Example enterprise (commented)
  # - name: alchemy-enterprise
  #   url: https://eth-mainnet.g.alchemy.com/v2/YOUR_ENTERPRISE_KEY
  #   type: enterprise
  #   timeout: 15s
```

### Environment Variables

URLs support `${VAR}` syntax for environment variable expansion:

```bash
export ALCHEMY_API_KEY="your_key_here"
export INFURA_API_KEY="your_key_here"
```

The config automatically expands environment variables using `os.ExpandEnv()`.

**Note:** The `type` field (public, self_hosted, enterprise) is informational only and does not affect provider behavior.

## Commands

### `block` - Block Inspector

View detailed block information with automatic provider selection.

**Usage:**
```bash
block [block_number] [flags]
```

**Arguments:**
- `block_number` (optional): Block identifier - can be:
  - `latest` (default) - Most recent block
  - `pending` - Pending block
  - `earliest` - Genesis block
  - Decimal number (e.g., `19000000`)
  - Hex number (e.g., `0x121eac0`)

**Flags:**
- `--config <path>`: Config file path (default: `config/providers.yaml`)
- `--provider <name>`: Use specific provider instead of auto-selecting fastest
- `--json`: Output JSON report to `reports/block-{timestamp}.json`

**Examples:**
```bash
block                    # Latest block from fastest provider
block 19000000          # Specific block by number
block 0x121eac0         # Specific block by hex
block latest --provider alchemy
block latest --json     # JSON report to reports/block-{timestamp}.json
```

**JSON output features:**
- Decimal values (not hex) for easier parsing
- ISO 8601 timestamps (e.g., "2026-01-20T17:02:23Z")
- Base fee converted to gwei (not wei)
- All numeric fields as native types

**Output example:**
```
Block #21,234,567
═══════════════════════════════════════════════════
  Hash:         0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
  Parent:       0x9876543210...
  Timestamp:    2024-01-15 14:32:18 UTC (12s ago)
  Gas:          29,847,293 / 30,000,000 (99.5%)
  Base Fee:     25.43 gwei
  Transactions: 342

  Provider:     alchemy (45ms)
```

---

### `test` - Provider Health Check

Test all providers and compare tail latency performance.

**Usage:**
```bash
test [flags]
```

**Flags:**
- `--config <path>`: Config file path (default: `config/providers.yaml`)
- `--samples <count>`: Number of test samples per provider (default: uses config value, typically 30)
- `--json`: Output JSON report to `reports/test-{timestamp}.json`

**Examples:**
```bash
test              # Uses default samples from config (30)
test --samples 10 # Override with custom sample count
test --json       # JSON report to reports/test-{timestamp}.json
```

**Features:**
- Warm-up request eliminates connection setup overhead from measurements
- Individual latency samples printed to stderr for tracing
- Percentile calculation uses nearest-rank method (ensures P95/P99 = Max for small samples)
- Block height mismatch detection (groups providers by height)

**Output example:**
```
Testing 4 providers with 30 samples each...

Provider       Type   Success      P50      P95      P99      Max        Block
──────────────────────────────────────────────────────────────────────────────────
alchemy        public     100%     23ms     45ms     52ms     78ms   21234567
infura         public     100%     19ms     38ms     47ms     65ms   21234567
llamanodes     public      97%    142ms    203ms    287ms    412ms   21234566
publicnode     public     100%    134ms    178ms    245ms    389ms   21234567
```

**Metrics explained:**
- **P50 (median)**: Typical response time
- **P95**: 95% of requests faster than this (captures outliers)
- **P99**: 99th percentile (worst-case scenarios)
- **Max**: Absolute worst observed latency
- Block height differences indicate sync lag
- Tail latency metrics (P95/P99/Max) are critical for trading systems where outliers cause missed opportunities

**Red flags:**
- Success rate < 95%
- P99 >> P50 (high variance, unpredictable performance)
- P99 > 500ms for paid providers (consider switching)
- Block height lagging behind others

---

### `snapshot` - Fork Detection

Compare block hashes and heights across providers to detect chain splits, stale caches, or sync lag.

**Usage:**
```bash
snapshot [block_number] [flags]
```

**Arguments:**
- `block_number` (optional): Block identifier (same format as `block` command, default: `latest`)

**Flags:**
- `--config <path>`: Config file path (default: `config/providers.yaml`)

**Examples:**
```bash
snapshot              # Compare latest block
snapshot latest       # Same as above
snapshot 19000000     # Compare specific block
```

**Features:**
- Warm-up request for accurate latency measurements
- Concurrent fetching using errgroup for speed
- Groups providers by hash/height to show consensus
- Detects both height mismatches (sync lag) and hash mismatches (forks/stale data)

**Output example:**
```
Fetching block latest from 4 providers...

Provider       Latency   Block Height   Block Hash
──────────────────────────────────────────────────────────────────────────────────────
alchemy          43ms        21234567   0xa1b2c3d4e5f6...
infura           39ms        21234567   0xa1b2c3d4e5f6...
llamanodes      167ms        21234566   0xa1b2c3d4e5f6...
publicnode      142ms        21234567   0xa1b2c3d4e5f6...

⚠ BLOCK HEIGHT MISMATCH DETECTED:
  Height 21234567  →  [alchemy infura publicnode]
  Height 21234566  →  [llamanodes]
```

**Why this matters:**
- **Height mismatches**: Provider is lagging behind the chain (propagation delay or sync issues)
- **Hash mismatches**: Providers disagree on block data (stale cache, chain reorganization, or fork)
- If your trading bot uses stale data, it might execute trades based on outdated state, leading to failed transactions or losses

---

### `monitor` - Continuous Monitoring

Watch all providers in real-time with automatic refresh.

**Usage:**
```bash
monitor [flags]
```

**Flags:**
- `--config <path>`: Config file path (default: `config/providers.yaml`)
- `--interval <duration>`: Refresh interval (default: uses config value, typically `30s`)

**Examples:**
```bash
monitor                # Uses default interval from config (30s)
monitor --interval 10s # Override with custom interval
```

**Features:**
- Real-time dashboard with screen clearing (ANSI escape codes)
- Graceful shutdown on Ctrl+C with signal handling
- Concurrent provider queries for fast refresh cycles

**Output updates continuously showing:**
- Current block height per provider
- Latency for each request
- Lag vs highest observed block (shows which providers are behind)

Press Ctrl+C to exit gracefully.

---

## Usage Examples

### Quick health check before deployment
```bash
test --samples 10
```
Run before deploying trading systems to verify all RPC endpoints are responsive.

### Monitor block production during high activity
```bash
monitor
```
Monitor all providers continuously with automatic refresh. Useful during network congestion or major events.

### Verify historical data consistency
```bash
snapshot 19000000
```
Ensure all providers agree on historical block data (important for reorgs).

### Automated monitoring script
```bash
#!/bin/bash
# Run every 5 minutes via cron
test --samples 3 > /var/log/rpc-health.log
snapshot latest >> /var/log/rpc-health.log
```

## Why This Matters

**RPC latency directly impacts trading P&L.** In competitive markets, the speed at which you receive blockchain data determines whether you capture opportunities or miss them entirely.

### RPC Performance Tiers

| Deployment Model | Typical Latency | Monthly Cost | Use Case |
|-----------------|-----------------|--------------|-----------|
| Self-hosted node | 1-5ms | $500-2000 | High-frequency trading, MEV |
| Enterprise SLA (Alchemy, Infura) | 20-50ms | $50-500 | Production apps, wallets |
| Free public endpoints | 100-500ms | Free | Development, testing |

### Real-World Impact

Consider an arbitrage opportunity that exists for 200ms:

```
Block N arrives → Opportunity detected → Execute trade
```

**With self-hosted node (3ms latency):**
- Receive block: 3ms
- Analyze: 10ms
- Submit tx: 3ms
- **Total: 16ms** — Trade executes, profit captured

**With free public RPC (300ms latency):**
- Receive block: 300ms
- Analyze: 10ms
- Submit tx: 300ms
- **Total: 610ms** — Opportunity gone, capital wasted

**Cost of slowness:** Lost arbitrage = $50-500 per opportunity. With 10-50 opportunities per day, this compounds to $15k-750k monthly.

## Architecture

```
cmd/                             # One binary per command
├── block/main.go                # Block inspector (fetch and display blocks)
├── test/main.go                 # Health check (tail latency metrics)
├── snapshot/main.go             # Fork detection (block comparison)
└── monitor/main.go              # Continuous monitoring (real-time dashboard)

internal/                        # Shared private packages
├── rpc/
│   ├── types.go                 # JSON-RPC protocol + block data structures
│   ├── client.go                # HTTP client with latency measurement
│   └── format.go                # Hex parsing, number/timestamp formatting
├── config/
│   └── config.go                # YAML loader with env var expansion
└── format/
    ├── colors.go                # ANSI color helpers + padding utilities
    ├── block.go                 # Single-block display renderer
    ├── test.go                  # Test results table + percentile calculation
    ├── snapshot.go              # Snapshot comparison + mismatch detection
    └── monitor.go               # Live dashboard renderer

config/
└── providers.yaml               # Provider configuration (single source of truth)
```

**Design principles:**
- No external dependencies for RPC (just `net/http`)
- Simplified design (no retry logic, no client pooling)
- Configuration via YAML with env variable expansion
- Pure functions for parsing and formatting
- Color-coded terminal output for quick visual assessment
- Concurrent execution using `golang.org/x/sync/errgroup`
- Warm-up requests in test/snapshot to eliminate connection overhead
- Extensive inline documentation designed as a guided walkthrough

## Troubleshooting

### "provider 'X' not found in config"
Check that `config/providers.yaml` includes a provider with that exact name.

### "failed to read config: no such file"
Specify config path: `block --config /path/to/providers.yaml`

### "defaults.timeout is required"
Add `defaults` section to `providers.yaml` with required fields:
- `timeout`: Request timeout (e.g., `10s`)
- `health_samples`: Default samples for test command (e.g., `30`)
- `watch_interval`: Default refresh interval for monitor (e.g., `30s`)

### All providers showing high latency
- Check your internet connection
- Try `curl https://cloudflare-eth.com` to test basic connectivity
- Verify no firewall blocking outbound HTTPS

### Hash mismatch on recent blocks
This is normal during chain reorganizations (reorgs). If it persists for >5 blocks, one provider may be on a stale fork.

### Height mismatch in snapshot output
If providers show different block heights for `latest`, some providers are lagging behind. This is common with free public endpoints during high network activity. Consider using paid providers with SLAs for production systems.

## Performance Considerations

### How fast is "fast enough"?

| Use Case | Acceptable Latency | Recommended Setup |
|----------|-------------------|-------------------|
| High-frequency trading | <10ms | Self-hosted node in same datacenter |
| MEV / arbitrage | <50ms | Self-hosted or premium tier |
| Production dApps | <200ms | Paid RPC with SLA |
| Wallets, explorers | <500ms | Free tier acceptable |
| Development | <1000ms | Any endpoint |

### Cost vs Performance

Don't over-optimize. Calculate your expected revenue impact:

```
Monthly savings from 50ms → 5ms = (opportunities captured) * (avg profit)
Cost of self-hosted node = $500-2000/mo

ROI = Monthly savings - Node cost
```

If ROI < 0, stick with paid RPC. If ROI > 0, consider self-hosting.

## Code Documentation

This project includes extensive inline documentation modeled as a guided walkthrough:

- **File-level headers**: Every file explains its system role, architecture position, and data flow
- **Section-level commentary**: Major blocks are introduced with intent, sequence, and rationale
- **Function-level docs**: Parameters, return values, and invariants are documented
- **Pointer deep-dives**: Every `*` and `&` is explained with ASCII memory diagrams
- **CS concept callouts**: Concurrency, closures, channels, and escape analysis are taught in context

The codebase is designed to be read as a learning resource — a guided walkthrough of a real-world system.

## License

MIT License - see LICENSE file for details

## Support

For issues or questions:
- GitHub Issues: https://github.com/dando385/eth-rpc-monitor/issues
- Read the code: It's intentionally simple and extensively commented

## Acknowledgments

Built for teams who treat blockchain infrastructure as production-critical and need observability into their RPC dependencies.
