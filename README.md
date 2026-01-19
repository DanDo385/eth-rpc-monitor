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
monitor                    # Latest block from fastest provider
monitor 19000000          # Specific block by number
monitor 0x121eac0         # Specific block by hex
monitor latest --provider alchemy
monitor latest --json     # Raw JSON output
```

### 2. Health Check
Test all providers and compare performance:

```bash
monitor health
monitor health --samples 10
```

Output:
```
Testing 5 providers with 5 samples each...

Provider       Type   Success        Avg        P95        Block
──────────────────────────────────────────────────────────────────
alchemy        paid      100%       45ms       52ms   21234567
infura         paid      100%       38ms       47ms   21234567
ankr           free       80%      156ms      203ms   21234566
cloudflare     free       60%      287ms      412ms   21234565
publicnode     free      100%      134ms      178ms   21234567
```

**Insights:**
- Paid providers (Alchemy, Infura) show consistent low latency
- Free endpoints have higher variance and occasional stale data
- Block height differences indicate sync lag

### 3. Fork Detection
Compare block hashes across providers to detect chain splits or stale caches:

```bash
monitor compare latest
monitor compare 19000000
```

Output:
```
Fetching block latest from 5 providers...

Provider       Latency   Block Hash
────────────────────────────────────────────────────────────────────────────────
alchemy          43ms   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
infura           39ms   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
ankr            167ms   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
cloudflare      289ms   0x9876543210fedcba0987654321fedcba0987654321fedcba0987654321fedcb
publicnode      142ms   0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456

⚠ HASH MISMATCH DETECTED:
  0xa1b2c3d4e5f678...  →  [alchemy infura ankr publicnode]
  0x9876543210fedc...  →  [cloudflare]

This may indicate stale cache or chain reorganization.
```

**Why this matters:** If your trading bot uses the stale Cloudflare data, it might execute trades based on outdated state, leading to failed transactions or losses.

## Installation

### Prerequisites
- Go 1.24 or later
- Ethereum RPC endpoint URLs (paid or free)

### Build from source

```bash
git clone https://github.com/dmagro/eth-rpc-monitor
cd eth-rpc-monitor
go build -o monitor ./cmd/monitor
```

## Configuration

Create or edit `config/providers.yaml`:

```yaml
defaults:
  timeout: 10s
  max_retries: 3

providers:
  - name: alchemy
    url: ${ALCHEMY_URL}
    type: paid

  - name: infura
    url: ${INFURA_URL}
    type: paid

  - name: ankr
    url: https://rpc.ankr.com/eth
    type: free

  - name: cloudflare
    url: https://cloudflare-eth.com
    type: free

  - name: publicnode
    url: https://ethereum-rpc.publicnode.com
    type: free
```

### Environment Variables

For paid providers, set environment variables:

```bash
export ALCHEMY_URL="https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY"
export INFURA_URL="https://mainnet.infura.io/v3/YOUR_API_KEY"
```

The config automatically expands `${ALCHEMY_URL}` and `${INFURA_URL}` using these environment variables.

## Usage Examples

### Quick health check before deployment
```bash
monitor health --samples 10
```
Run before deploying trading systems to verify all RPC endpoints are responsive.

### Monitor block production during high activity
```bash
watch -n 1 'monitor latest'
```
Watch blocks arrive in real-time. Useful during network congestion or major events.

### Verify historical data consistency
```bash
monitor compare 19000000
```
Ensure all providers agree on historical block data (important for reorgs).

### Automated monitoring script
```bash
#!/bin/bash
# Run every 5 minutes via cron
monitor health --samples 3 > /var/log/rpc-health.log
monitor compare latest >> /var/log/rpc-health.log
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
- **Avg latency**: Mean response time
- **P95 latency**: 95th percentile (captures outliers)
- **Block height**: Current sync state

**Red flags:**
- Success rate < 95%
- P95 > 2x Avg (high variance)
- Block height lagging behind others

## Architecture

This tool follows a simple, maintainable design:

```
cmd/monitor/
├── main.go      # Main CLI and block inspector
├── health.go    # Health check command
└── compare.go   # Block comparison command

internal/
├── config/
│   └── config.go      # YAML configuration loader
└── rpc/
    ├── types.go       # Block and response types
    └── client.go      # HTTP JSON-RPC client

config/
└── providers.yaml     # Provider configuration
```

**Design principles:**
- No external dependencies for RPC (just `net/http`)
- Simple exponential backoff retry (no circuit breakers)
- Configuration via YAML with env variable expansion
- Pure functions for parsing and formatting

## Troubleshooting

### "provider 'X' not found in config"
Check that `config/providers.yaml` includes a provider with that exact name.

### "failed to read config: no such file"
Specify config path: `monitor --config /path/to/providers.yaml`

### "defaults.timeout is required"
Add `defaults` section to `providers.yaml` with `timeout` and `max_retries`.

### All providers showing high latency
- Check your internet connection
- Try `curl https://cloudflare-eth.com` to test basic connectivity
- Verify no firewall blocking outbound HTTPS

### Hash mismatch on recent blocks
This is normal during chain reorganizations (reorgs). If it persists for >5 blocks, one provider may be on a stale fork.

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

## Support

For issues or questions:
- GitHub Issues: https://github.com/dmagro/eth-rpc-monitor/issues
- Read the code: It's intentionally simple and well-commented

## Acknowledgments

Built for teams who treat blockchain infrastructure as production-critical and need observability into their RPC dependencies.
