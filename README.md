# Ethereum RPC Infrastructure Monitor

A command-line tool for monitoring Ethereum RPC endpoint reliability, latency distribution, and cross-provider data consistency. Designed for institutional operations teams who need visibility into their infrastructure dependencies.

## Why This Exists

Institutions running operations on Ethereum—custody, staking, trading, settlement—depend on reliable RPC infrastructure. Whether self-hosted nodes or managed providers, the operational concerns are identical:

- **What happens when a provider goes down?**
- **How do we detect stale or inconsistent data?**
- **Which providers should we prioritize for failover?**

This tool demonstrates the monitoring patterns required for production-grade Ethereum infrastructure.

## Features

### Snapshot Mode
Generate a detailed diagnostic report across all configured providers:
- Latency percentiles (p50, p95, p99, max)
- Success rates and error classification
- Cross-provider block height consistency
- Block hash verification (detects reorgs and stale caches)

### Watch Mode  
Real-time monitoring with live updates:
- Provider status at a glance
- Current latency and block height
- Recent incident log
- Immediate visibility when infrastructure degrades

## Installation

```bash
git clone https://github.com/dmagro/eth-rpc-monitor
cd eth-rpc-monitor
go build -o monitor ./cmd/monitor
```

## Usage

### Snapshot Report
```bash
# Run with default settings (30 samples per provider)
./monitor snapshot

# Custom sample count and interval
./monitor snapshot --samples 50 --interval 100

# JSON output for automation
./monitor snapshot --format json > report.json
```

### Live Monitoring
```bash
# Default 5-second refresh
./monitor watch

# Faster refresh for active incidents
./monitor watch --refresh 2s
```

### Configuration
```bash
# Use custom provider config
./monitor snapshot --config /path/to/providers.yaml
```

## Provider Configuration

Edit `config/providers.yaml` to configure your endpoints:

```yaml
providers:
  - name: alchemy
    url: https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
    type: enterprise
    
  - name: local-geth
    url: http://localhost:8545
    type: self_hosted
    timeout: 5s
```

Provider types:
- `public`: Free tier endpoints (rate limited, no SLA)
- `enterprise`: Paid managed services (Alchemy, Infura enterprise tiers)
- `self_hosted`: Self-operated nodes

## Architecture

```
├── cmd/monitor/          # CLI entry point
├── internal/
│   ├── rpc/              # JSON-RPC client with retry/circuit breaker
│   ├── metrics/          # Statistics and consistency checking
│   ├── output/           # Terminal and JSON rendering
│   └── config/           # YAML configuration
└── config/               # Default provider configuration
```

### Resilience Patterns

The RPC client implements production-grade resilience:

- **Exponential backoff with jitter**: Prevents thundering herd on recovery
- **Circuit breaker**: Stops hammering dead endpoints (3 failures → 30s cooldown)
- **Graceful degradation**: Partial failures don't block healthy providers

## RPC Methods and Institutional Impact

### eth_blockNumber
Returns the current block height. Primary use: liveness check and sync status.

**Why institutions care:**  
Stale block height means stale pricing data, delayed risk calculations, and potential settlement failures. If your provider reports height 19,234,560 while the network is at 19,234,580, your systems are operating on 4-minute-old state.

**Failure modes:**
- Timeout → Provider unreachable, trigger failover
- Stale height → Provider is up but behind, data integrity risk

### eth_getBlockByNumber
Returns block data including hash. Primary use: data retrieval and consistency verification.

**Why institutions care:**  
Two providers reporting the same height but different hashes indicates a reorg in progress or a caching issue. For custody operations and reconciliation, this is a critical inconsistency that could cause double-counting or missed transactions.

**What this tool checks:**
- Height variance across providers (acceptable: ≤2 blocks / ~24 seconds)
- Hash agreement at common height (any mismatch is flagged)

## Deployment Reality

This tool monitors public RPC endpoints for demonstration purposes. In production:

| Environment | Infrastructure |
|------------|----------------|
| **Enterprise** | Alchemy/Infura enterprise tiers, Blockdaemon, Figment |
| **Self-hosted** | Geth/Erigon execution clients + Prysm/Lighthouse consensus |
| **Hybrid** | Primary self-hosted with provider fallback |

**The monitoring logic is identical regardless of endpoint type.** Only the SLAs and rate limits differ.

## Sample Output

### Snapshot Report
```
╭─────────────────────────────────────────────────────────────────╮
│           Ethereum RPC Infrastructure Report                    │
│                    2025-01-13 14:32:01 UTC                      │
│                      Sample Size: 30                            │
╰─────────────────────────────────────────────────────────────────╯

Provider Performance
┌──────────────┬────────┬────────┬────────┬────────┬─────────┐
│ Provider     │ Status │ p50    │ p95    │ p99    │ Success │
├──────────────┼────────┼────────┼────────┼────────┼─────────┤
│ Alchemy      │ ✓ UP   │ 138ms  │ 201ms  │ 287ms  │ 100.0%  │
│ Infura       │ ✓ UP   │ 145ms  │ 219ms  │ 298ms  │ 98.0%   │
│ LlamaNodes   │ ⚠ SLOW │ 312ms  │ 876ms  │ 1.2s   │ 94.0%   │
│ PublicNode   │ ✗ DOWN │ —      │ —      │ —      │ 34.0%   │
└──────────────┴────────┴────────┴────────┴────────┴─────────┘

Block Height Consistency
  Network Height: 19,234,567 (via Alchemy)
  
  ┌──────────────┬─────────────┬───────────┬─────────────────────────┐
  │ Provider     │ Block       │ Delta     │ Assessment              │
  ├──────────────┼─────────────┼───────────┼─────────────────────────┤
  │ Alchemy      │ 19,234,567  │ 0         │ ✓ In sync               │
  │ Infura       │ 19,234,567  │ 0         │ ✓ In sync               │
  │ LlamaNodes   │ 19,234,566  │ -1        │ ⚠ 1 block behind (~12s) │
  │ PublicNode   │ 19,234,561  │ -6        │ ✗ Stale (72s behind)    │
  └──────────────┴─────────────┴───────────┴─────────────────────────┘

Operational Assessment
  ┌─────────────────────────────────────────────────────────────────┐
  │ ✗ PublicNode unsuitable for production use                      │
  │ ⚠ LlamaNodes showing elevated latency, acceptable for fallback  │
  │ ✓ Alchemy, Infura performing within expected parameters         │
  │                                                                 │
  │ Recommended priority: Alchemy → Infura → LlamaNodes             │
  └─────────────────────────────────────────────────────────────────┘
```

## Development

### Prerequisites
- Go 1.21+

### Build
```bash
go build -o monitor ./cmd/monitor
```

### Test
```bash
go test ./...
```

### Dependencies
- [cobra](https://github.com/spf13/cobra) - CLI framework
- [color](https://github.com/fatih/color) - Terminal colors
- [table](https://github.com/rodaine/table) - ASCII tables
- [yaml.v3](https://gopkg.in/yaml.v3) - Configuration parsing

## License

MIT

## Author

Daniel Magro - [LinkedIn](https://linkedin.com/in/daniel-magro-2323941a2)

Background: Institutional fixed income trading and portfolio management, transitioning to Ethereum infrastructure. This project demonstrates the intersection of traditional finance operational rigor and blockchain infrastructure requirements.
