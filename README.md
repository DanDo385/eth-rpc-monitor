# Ethereum RPC Monitor

A lightweight tool for monitoring Ethereum RPC endpoint performance and reliability.

## Why This Matters

**RPC latency directly impacts trading P&L.** In competitive markets, the speed at which you receive blockchain data determines whether you capture opportunities or miss them entirely.

### RPC Performance Tiers

| Deployment Model | Typical Latency | Monthly Cost | Use Case |
|-----------------|-----------------|--------------|-----------|
| Self-hosted node | 1-5ms | $500-2000 | High-frequency trading, MEV |
| Enterprise SLA (Alchemy, Infura) | 20-50ms | $50-500 | Production apps, wallets |
| Free public endpoints | 100-500ms | Free | Development, testing |

## Real-World Impact

### Example: Arbitrage Bot Timing

Consider an arbitrage opportunity that exists for 200ms:

```
Block N arrives → Opportunity detected → Execute trade
```

**With self-hosted node (3ms latency):**
- Receive block: 3ms
- Analyze: 10ms
- Submit tx: 3ms
- **Total: 16ms** ✓ Trade executes, profit captured

**With free public RPC (300ms latency):**
- Receive block: 300ms
- Analyze: 10ms
- Submit tx: 300ms
- **Total: 610ms** ✗ Opportunity gone, capital wasted

**Cost of slowness:** Lost arbitrage = $50-500 per opportunity. With 10-50 opportunities per day, this compounds to $15k-750k monthly.

## Features

### 1. Block Inspector
View detailed block information with automatic provider selection:

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

### 2. Health Check
Test all providers and compare tail latency performance:

```bash
health              # Uses default samples from config (30)
health --samples 10 # Override with custom sample count
health --json       # JSON report to reports/health-{timestamp}.json
```

**Features:**
- Warm-up request eliminates connection setup overhead from measurements
- Individual latency samples printed to stderr for tracing
- Percentile calculation uses nearest-rank method (ensures P95/P99 = Max for small samples)
- Block height mismatch detection (groups providers by height)

Output:
```
Testing 4 providers with 30 samples each...

Provider       Type   Success      P50      P95      P99      Max        Block
──────────────────────────────────────────────────────────────────────────────────
alchemy        public     100%     23ms     45ms     52ms     78ms   21234567
infura         public     100%     19ms     38ms     47ms     65ms   21234567
llamanodes     public      97%    142ms    203ms    287ms    412ms   21234566
publicnode     public     100%    134ms    178ms    245ms    389ms   21234567
```

**Insights:**
- **P50 (median)**: Typical response time
- **P95**: 95% of requests faster than this (captures outliers)
- **P99**: 99th percentile (worst-case scenarios)
- **Max**: Absolute worst observed latency
- Block height differences indicate sync lag
- Tail latency metrics (P95/P99/Max) are critical for trading systems where outliers cause missed opportunities

### 3. Fork Detection
Compare block hashes and heights across providers to detect chain splits, stale caches, or sync lag:

```bash
compare              # Compare latest block
compare latest       # Same as above
compare 19000000     # Compare specific block
compare latest --json # JSON report to reports/compare-{timestamp}.json
```

**Features:**
- Warm-up request for accurate latency measurements
- Concurrent fetching using errgroup for speed
- Groups providers by hash/height to show consensus
- Detects both height mismatches (sync lag) and hash mismatches (forks/stale data)

Output:
```
Fetching block latest from 4 providers...

Provider       Latency   Block Height   Block Hash
──────────────────────────────────────────────────────────────────────────────────────
alchemy          43ms        21234567   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
infura           39ms        21234567   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
llamanodes      167ms        21234566   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
publicnode      142ms        21234567   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456

⚠ BLOCK HEIGHT MISMATCH DETECTED:
  Height 21234567  →  [alchemy infura publicnode]
  Height 21234566  →  [llamanodes]

This may indicate lagging providers or propagation delays.

⚠ BLOCK HASH MISMATCH DETECTED:
  0xa1b2c3d4e5f678...  →  [alchemy infura llamanodes publicnode]
  0x9876543210fedc...  →  [cloudflare]

This may indicate stale caches, chain reorganization, or incorrect data.
```

**Why this matters:**
- **Height mismatches**: Provider is lagging behind the chain (propagation delay or sync issues)
- **Hash mismatches**: Providers disagree on block data (stale cache, chain reorganization, or fork)
- If your trading bot uses stale data, it might execute trades based on outdated state, leading to failed transactions or losses

### 4. Continuous Monitoring
Watch all providers in real-time with automatic refresh:

```bash
monitor                # Uses default interval from config (30s)
monitor --interval 10s # Override with custom interval
monitor --json         # Write JSON report on exit (Ctrl+C)
```

Output updates continuously showing:
- Current block height per provider
- Latency for each request
- Lag vs highest observed block (shows which providers are behind)

**Features:**
- Real-time dashboard with screen clearing (ANSI escape codes)
- Graceful shutdown on Ctrl+C with signal handling
- Optional JSON report written on exit with final snapshot
- Concurrent provider queries for fast refresh cycles

## Installation

### Prerequisites
- Go 1.24 or later
- Ethereum RPC endpoint URLs (paid or free)

### Build from source

```bash
git clone https://github.com/dmagro/eth-rpc-monitor
cd eth-rpc-monitor
go build -o block ./cmd/block
go build -o compare ./cmd/compare
go build -o health ./cmd/health
go build -o monitor ./cmd/monitor
```

## Configuration

Create or edit `config/providers.yaml` (copy from `config/providers.yaml.example`):

```yaml
defaults:
  timeout: 10s
  max_retries: 3
  health_samples: 30      # Default samples for health command
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
export ALCHEMY_URL="https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY"
export INFURA_URL="https://mainnet.infura.io/v3/YOUR_API_KEY"
```

The config automatically expands environment variables using `os.ExpandEnv()`.

**Note:** The `type` field (public, self_hosted, enterprise) is informational only and does not affect provider behavior.

## Usage Examples

### Quick health check before deployment
```bash
health --samples 10
```
Run before deploying trading systems to verify all RPC endpoints are responsive.

### Monitor block production during high activity
```bash
monitor
```
Monitor all providers continuously with automatic refresh. Useful during network congestion or major events.

### Verify historical data consistency
```bash
compare 19000000
```
Ensure all providers agree on historical block data (important for reorgs).

### Automated monitoring script
```bash
#!/bin/bash
# Run every 5 minutes via cron
health --samples 3 > /var/log/rpc-health.log
compare latest >> /var/log/rpc-health.log
```

## Understanding the Output

### Block Inspector Output
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

**Key metrics:**
- **Timestamp age**: How old is this block? <15s is normal for latest
- **Gas usage**: High (>95%) indicates network congestion
- **Base Fee**: Current gas price in gwei
- **Latency**: Time to fetch block from provider

### Health Check Metrics

- **Success rate**: % of requests that succeeded
- **P50 latency**: Median response time (typical performance)
- **P95 latency**: 95th percentile (captures outliers)
- **P99 latency**: 99th percentile (worst-case scenarios)
- **Max latency**: Absolute worst observed latency
- **Block height**: Current sync state

**Red flags:**
- Success rate < 95%
- P99 >> P50 (high variance, unpredictable performance)
- P99 > 500ms for paid providers (consider switching)
- Block height lagging behind others

## Architecture

This tool follows a simple, maintainable design with extensive documentation:

```
cmd/
├── block/
│   └── main.go      # Block inspector (fetch and display blocks)
├── compare/
│   └── main.go      # Block comparison (fork detection)
├── health/
│   └── main.go      # Health check (tail latency metrics)
└── monitor/
    └── main.go      # Continuous monitoring (real-time dashboard)

internal/
├── config/
│   └── config.go      # YAML configuration loader with env expansion
├── env/
│   └── env.go        # .env file loader for sensitive config
├── reports/
│   └── reports.go    # JSON report generation (timestamped files)
└── rpc/
    ├── client.go     # HTTP JSON-RPC client with retry, latency measurement
    ├── format.go     # Hex parsing, number formatting, unit conversion
    └── types.go      # Block and response types

config/
└── providers.yaml     # Provider configuration (single source of truth)
```

**Design principles:**
- No external dependencies for RPC (just `net/http`)
- Simple exponential backoff retry (no circuit breakers)
- Configuration via YAML with env variable expansion
- Pure functions for parsing and formatting
- Extensive inline documentation for maintainability
- Concurrent execution using `golang.org/x/sync/errgroup`
- Warm-up requests in health/compare to eliminate connection overhead

## Troubleshooting

### "provider 'X' not found in config"
Check that `config/providers.yaml` includes a provider with that exact name.

### "failed to read config: no such file"
Specify config path: `block --config /path/to/providers.yaml`

### "defaults.timeout is required"
Add `defaults` section to `providers.yaml` with required fields:
- `timeout`: Request timeout (e.g., `10s`)
- `max_retries`: Retry count (e.g., `3`)
- `health_samples`: Default samples for health command (e.g., `30`)
- `watch_interval`: Default refresh interval for monitor (e.g., `30s`)

### All providers showing high latency
- Check your internet connection
- Try `curl https://cloudflare-eth.com` to test basic connectivity
- Verify no firewall blocking outbound HTTPS

### Hash mismatch on recent blocks
This is normal during chain reorganizations (reorgs). If it persists for >5 blocks, one provider may be on a stale fork.

### Height mismatch in compare output
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
Monthly savings from 50ms → 5ms = (opportunities captured) × (avg profit)
Cost of self-hosted node = $500-2000/mo

ROI = Monthly savings - Node cost
```

If ROI < 0, stick with paid RPC. If ROI > 0, consider self-hosting.

## Contributing

This is a focused tool. Contributions should maintain simplicity:

**Accepted:**
- Bug fixes
- Performance improvements
- Additional RPC methods (if broadly useful)
- Better error messages

**Not accepted:**
- Complex abstractions
- Framework dependencies
- Features that duplicate existing functionality

## License

MIT License - see LICENSE file for details

## Code Documentation

This project includes extensive inline documentation:
- **Package-level docs**: Every package explains its purpose and usage
- **Function docs**: All exported functions include parameter/return documentation
- **Algorithm explanations**: Complex logic (percentiles, provider selection) is documented
- **Type documentation**: All structs and types have explanatory comments

The codebase is designed to be self-documenting and maintainable.

## Support

For issues or questions:
- GitHub Issues: https://github.com/dmagro/eth-rpc-monitor/issues
- Read the code: It's intentionally simple and extensively commented

## Acknowledgments

Built for teams who treat blockchain infrastructure as production-critical and need observability into their RPC dependencies.
