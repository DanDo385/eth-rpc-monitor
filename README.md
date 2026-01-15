# Ethereum RPC Infrastructure Monitor

A command-line tool for monitoring Ethereum RPC endpoint reliability, latency distribution, and cross-provider data consistency. Designed for institutional operations teams who need visibility into their infrastructure dependencies.

## Why This Exists

Institutions running operations on Ethereum—custody, staking, trading, settlement—depend on reliable RPC infrastructure. Whether self-hosted nodes or managed providers, the operational concerns are identical:

- **What happens when a provider goes down?**
- **How do we detect stale or inconsistent data?**
- **Which providers should we prioritize for failover?**

This tool demonstrates the monitoring patterns required for production-grade Ethereum infrastructure. It is being developed in iterative phases to gradually expand functionality while maintaining clarity. Each phase adds features addressing specific operational needs: real-time monitoring, on-demand data queries, intelligent provider selection, and smart contract state verification.

Future enhancements will include automated provider recommendations based on live performance metrics and expanded consistency verification across additional RPC methods.

## Features

### Snapshot Mode
Generate a detailed one-time diagnostic report across all configured providers:

- **Latency percentiles** (p50, p95, p99, max) with comprehensive statistical analysis
- **Success rates and error classification** (timeouts, rate limits, server errors, parse errors)
- **Cross-provider block height consistency** with variance detection and drift analysis
- **Block hash verification** at a common reference height to detect chain reorgs and stale caches
- **Operational assessment** with automated provider ranking and failover recommendations

The snapshot command collects configurable sample sizes (default: 30) with adjustable intervals between requests, providing statistically significant performance metrics for capacity planning and SLA validation.

### Watch Mode

Real-time monitoring with live updates and incident tracking:

- **Provider status at a glance** (UP/SLOW/DEGRADED/DOWN) with visual indicators
- **Current latency and block height** for each provider, updated at configurable intervals
- **Recent incident log** capturing status changes, latency spikes, and provider failures
- **Immediate visibility** when infrastructure degrades, enabling rapid incident response
- **Block hash consensus tracking** performed every 3 refresh cycles to balance thoroughness with performance

Watch mode implements automatic refresh (default: 5 seconds) and maintains a rolling incident history to help operators identify patterns in provider behavior.

### On-Demand Queries

In addition to continuous monitoring, the tool provides commands for one-off queries and data inspection:

#### Block Data Retrieval (`blocks` command)
Fetch and display the details of a specific block by number, hex, or tag (`latest`). Returns comprehensive block metadata including:
- Block number, hash, and parent hash
- Timestamp (both Unix and human-readable)
- Gas used and gas limit with utilization percentage
- Base fee per gas (post-EIP-1559 blocks)
- Transaction count

Useful for quick inspection of block metadata across providers and verifying data consistency. Supports `--raw` flag to display the full JSON-RPC response for detailed analysis.

#### Transaction Listing (`txs` command)
Display the list of transactions within a given block. Shows transaction details including:
- Transaction hash
- From and to addresses
- Value transferred (in ETH)
- Gas limit and gas price
- Transaction index and nonce
- Calldata summary (truncated for readability)

Allows drilling into block contents to verify all providers return the same transactions, detecting any discrepancies in transaction data. Supports `--limit` flag to control output size and `--raw` flag for full JSON transaction objects.

#### Smart Contract Call (`call` command)
Query on-chain data via `eth_call` to read smart contract state without sending transactions. Currently implements ERC-20 token balance queries with automatic ABI encoding/decoding:

**USDC Balance Query:**
```bash
monitor call usdc balance <address>
```

This executes a `balanceOf(address)` call to the USDC contract (0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48) on mainnet, demonstrating:
- Keccak-256 function selector computation (`balanceOf(address)` → `0x70a08231`)
- ABI parameter encoding (address → 32-byte padded hex)
- Return value decoding (uint256 → human-readable balance with decimals)
- Token amount formatting with thousand separators and proper decimal placement

The `--raw` flag displays the raw calldata and hex-encoded return value for educational purposes, showing exactly what gets transmitted over the wire.

#### Status Check (`status` command)
Get an instant status summary of all configured providers with automatic health-based ranking. Performs a quick health check (default: 5 samples per provider) and returns:

**Per-Provider Metrics:**
- Current status (UP, SLOW, DEGRADED, DOWN) based on success rate and latency thresholds
- Success rate percentage over the sample window
- Average and P95 latency measurements
- Latest block height with delta from network maximum
- Composite health score (0-1) computed from weighted metrics

**Scoring Algorithm:**
The composite score combines three weighted factors:
- **Success rate (50% weight):** Reliability is paramount for production systems
- **Latency (30% weight):** P95 latency normalized against 1000ms threshold
- **Freshness (20% weight):** Block height delta normalized against 10-block window

**Ranking and Recommendations:**
Providers are ranked by composite score, with exclusion logic for degraded endpoints:
- Success rate below 80% → excluded with reason
- More than 5 blocks behind → excluded with reason
- Automatic failover priority recommendation (e.g., "Alchemy → Infura → LlamaNodes")

**Operational Assessment:**
The status output includes human-readable assessment of infrastructure health, flagging providers unsuitable for production use and highlighting those acceptable for fallback scenarios.

This command is particularly valuable for:
- **Capacity planning:** Understanding which providers can handle production traffic
- **Incident response:** Quickly identifying degraded endpoints during outages
- **Failover configuration:** Determining optimal provider ordering based on live performance
- **Educational exploration:** Seeing how different provider tiers (public/enterprise/self-hosted) perform in practice

### Smart Provider Selection

All on-demand query commands (`blocks`, `txs`, `call`) implement automatic provider selection when no `--provider` flag is specified. The selection algorithm:

1. Performs a quick health check (3 samples per provider) with 10-second timeout
2. Calculates composite scores based on success rate, latency, and block height freshness
3. Excludes providers with <80% success rate or >5 blocks behind
4. Selects the highest-scoring provider and reports the auto-selection to the user

This ensures queries are routed to the most reliable, performant provider available at query time, automatically adapting to changing network conditions.

**Fallback Behavior:**
If health check fails or all providers are degraded, the system falls back to the first configured provider with a warning message, ensuring operations continue even during partial outages.

## Installation

```bash
git clone https://github.com/dmagro/eth-rpc-monitor
cd eth-rpc-monitor
go build -o monitor ./cmd/monitor
```

**Requirements:**
- Go 1.21+ (tested with Go 1.24)
- Network access to configured RPC endpoints

**Build Output:**
The compiled binary (~12MB) is a self-contained executable with no runtime dependencies.

## Usage

### Snapshot Report
```bash
# Run with default settings (30 samples per provider, 100ms interval)
./monitor snapshot

# Custom sample count and interval for high-resolution profiling
./monitor snapshot --samples 50 --interval 100

# JSON output for automation and integration with monitoring systems
./monitor snapshot --format json > report.json
```

**Sample Configuration Trade-offs:**
- **Fewer samples (10-20):** Fast execution, suitable for quick checks, higher variance
- **Default (30 samples):** Balanced statistical significance and execution time (~3 seconds)
- **More samples (50-100):** Higher confidence in percentile calculations, longer execution (~5-10 seconds)

**Interval Tuning:**
- Lower intervals (50ms): Faster overall execution but may trigger rate limits on public endpoints
- Higher intervals (200-500ms): More suitable for public tier endpoints with rate limits
- The interval provides a simple throttle mechanism to prevent overwhelming providers

**JSON Output Structure:**
The JSON format provides machine-readable data suitable for:
- Integration with monitoring platforms (Prometheus, Datadog, Grafana)
- Automated alerting based on success rate or latency thresholds
- Historical trending and SLA compliance reporting
- Custom analysis and visualization pipelines

### Live Monitoring
```bash
# Default 5-second refresh interval
./monitor watch

# Faster refresh (e.g. 2s) for active incident response
./monitor watch --refresh 2s

# Slower refresh (e.g. 30s) for low-priority monitoring or rate-limited endpoints
./monitor watch --refresh 30s
```

**Watch Mode Implementation Details:**
- Uses terminal clearing and redrawing for smooth live updates without scrolling
- Maintains a rolling incident log (last 10 events) with timestamps and severity
- Tracks status transitions (UP→SLOW, SLOW→DOWN, etc.) to identify flapping
- Performs block hash consensus checks every 3 cycles (15 seconds at default refresh) to detect reorgs without excessive RPC calls
- Supports graceful shutdown via Ctrl+C, preserving final state in terminal

**Refresh Interval Considerations:**
- **Fast (1-2s):** High visibility during incidents, increased RPC load and potential rate limiting
- **Medium (5-10s):** Balanced for operational monitoring, default recommended setting
- **Slow (30s+):** Suitable for long-term observation or public endpoint constraints

### Block Query
```bash
# Fetch details for block 17000000 (using auto-selected provider)
./monitor blocks 17000000

# Fetch latest block
./monitor blocks latest

# Use hexadecimal block number (alternative notation)
./monitor blocks 0x1036640

# Specify a provider explicitly (bypasses auto-selection)
./monitor blocks 17000000 --provider alchemy

# Get output in JSON format (useful for parsing or feeding to other tools)
./monitor blocks 17000000 --format json

# Show raw JSON-RPC response (educational: see exact RPC wire format)
./monitor blocks latest --raw
```

**Terminal Mode Output:**
Displays block metadata in a human-readable format with:
- Block identifier and confirmation status
- Timestamp in both UTC and relative format (e.g., "2 days ago")
- Gas metrics with utilization percentage bar
- Base fee in both Wei and Gwei for post-London blocks
- Transaction count
- Provider used and query latency

**JSON Mode Output:**
Returns structured data including all terminal fields plus access to nested block properties for programmatic consumption.

**Raw Mode:**
Shows the complete JSON-RPC response exactly as returned by the provider, including all fields. Useful for:
- Understanding the full RPC data structure
- Verifying provider-specific extensions or non-standard fields
- Educational purposes (learning Ethereum RPC protocol)
- Debugging data parsing issues

### Transactions in a Block
```bash
# List all transactions in block 17000000 (using auto-selected provider)
./monitor txs 17000000

# Limit output to first 10 transactions (useful for large blocks)
./monitor txs 17000000 --limit 10

# Show all transactions (0 = unlimited)
./monitor txs 17000000 --limit 0

# Use specific provider
./monitor txs 17000000 --provider infura

# JSON output with full transaction objects
./monitor txs 17000000 --format json

# Raw mode: display complete transaction array as returned by RPC
./monitor txs 17000000 --raw
```

**Terminal Mode Display:**
For each transaction, shows:
- Transaction hash (clickable in most terminals when formatted as `0x...`)
- From → To addresses (with special notation for contract creation)
- Value transferred in ETH with full decimal precision
- Gas limit allocated
- Gas price (legacy) or max fee (EIP-1559)
- Calldata size and preview (first 32 bytes)

**Limit Parameter Behavior:**
- Default limit (25): Shows first 25 transactions with summary of total count
- Custom limit: User-specified count for exploring large blocks incrementally
- Zero limit: Displays all transactions regardless of count (use with caution on large blocks)

**Use Cases:**
- Verifying transaction inclusion across providers (consistency check)
- Investigating block contents during incident analysis
- Comparing transaction data between providers to detect discrepancies
- Educational exploration of transaction structures and patterns

### Smart Contract Call (Token Balance)
```bash
# Query USDC balance for address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045
./monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045

# Use specific provider
./monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 --provider alchemy

# JSON output (structured data)
./monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 --format json

# Raw mode: show calldata encoding and hex-encoded return value
./monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 --raw
```

**Implementation Details:**

The `call` command demonstrates production-grade ABI encoding and decoding:

**Function Selector Computation:**
```
balanceOf(address) → Keccak256 hash → First 4 bytes → 0x70a08231
```

**Address Parameter Encoding:**
```
Input: 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045
Output: 0x000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045
        (left-padded to 32 bytes)
```

**Complete Calldata:**
```
0x70a08231000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045
  ^^^^^^^^ function selector
          ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ padded address parameter
```

**Return Value Decoding:**
```
RPC returns: 0x0000000000000000000000000000000000000000000000000000000005f5e100 (hex)
Decoded: 100000000 (uint256)
Formatted: 100.000000 USDC (accounting for 6 decimals)
```

**Terminal Output:**
Displays:
- Contract being queried (address and symbol)
- Method called (`balanceOf`)
- Target address being queried
- Formatted balance with decimals and thousand separators
- Provider used and query latency
- Optional: Raw calldata and response (with `--raw` flag)

**Educational Value:**
This command serves as a reference implementation for:
- Keccak-256 hashing for function selectors (using golang.org/x/crypto/sha3)
- ABI encoding rules (fixed-size types, left-padding for addresses)
- Big integer arithmetic for token amounts (using math/big)
- Decimal formatting with thousand separators
- Proper handling of different token decimal places (6 for USDC, 18 for WETH, etc.)

**Extensibility:**
The ABI encoding logic in `internal/rpc/abi.go` is designed for easy extension to other ERC-20 tokens or arbitrary contract calls. Additional token contracts can be added by defining contract address and decimal constants.

### Status Check
```bash
# Get instant status summary of all configured providers
./monitor status

# Custom sample count for more precise scoring
./monitor status --samples 10

# JSON output for integration with monitoring systems
./monitor status --format json
```

**Terminal Mode Output:**

```
╭─────────────────────────────────────────────────────────────────╮
│              Provider Health Status Report                      │
│                   2025-01-14 14:32:01 UTC                       │
│                      Sample Size: 5                             │
╰─────────────────────────────────────────────────────────────────╯

Provider Rankings
┌──────────────┬─────────┬─────────┬────────┬─────────┬───────┬────────┐
│ Provider     │ Status  │ Success │ Avg    │ P95     │ Block │ Score  │
├──────────────┼─────────┼─────────┼────────┼─────────┼───────┼────────┤
│ Alchemy      │ ✓ UP    │ 100.0%  │ 145ms  │ 189ms   │ 0     │ 0.912  │
│ Infura       │ ✓ UP    │ 100.0%  │ 167ms  │ 203ms   │ 0     │ 0.896  │
│ LlamaNodes   │ ⚠ SLOW  │ 100.0%  │ 423ms  │ 512ms   │ -1    │ 0.682  │
│ PublicNode   │ ✗ DOWN  │ 40.0%   │ —      │ —       │ -8    │ 0.124  │
└──────────────┴─────────┴─────────┴────────┴─────────┴───────┴────────┘

Operational Assessment
  ┌─────────────────────────────────────────────────────────────────┐
  │ ✗ PublicNode unsuitable for production use                      │
  │   Reason: success rate 40.0% below threshold                    │
  │                                                                 │
  │ ⚠ LlamaNodes acceptable for fallback only                       │
  │   Status: SLOW (P95 latency 512ms exceeds 500ms threshold)      │
  │                                                                 │
  │ ✓ Alchemy, Infura performing within expected parameters         │
  │                                                                 │
  │ Recommended priority: Alchemy → Infura → LlamaNodes             │
  └─────────────────────────────────────────────────────────────────┘
```

**JSON Output Structure:**
```json
{
  "timestamp": "2025-01-14T14:32:01Z",
  "providers": [
    {
      "name": "Alchemy",
      "status": "UP",
      "successRate": 100.0,
      "avgLatency": "145ms",
      "p95Latency": "189ms",
      "blockHeight": 19234567,
      "blockDelta": 0,
      "score": 0.912,
      "excluded": false,
      "excludeReason": ""
    }
  ],
  "recommendations": ["Alchemy", "Infura", "LlamaNodes"],
  "excluded": ["PublicNode"]
}
```

**Status Thresholds:**

The status command applies production-grade health classification:

| Status    | Success Rate | P95 Latency | Description |
|-----------|-------------|-------------|-------------|
| **UP**    | ≥90%        | ≤500ms      | Optimal performance, suitable for primary use |
| **SLOW**  | ≥90%        | >500ms      | Reliable but slow, acceptable for fallback |
| **DEGRADED** | 50-90%   | Any         | Partial failures, use with caution |
| **DOWN**  | <50%        | Any         | Majority failures, should not be used |

**Exclusion Logic:**
- Success rate <80%: Excluded from recommendations with specific reason
- Block delta >5: Excluded due to stale data (>60 seconds behind at ~12s block time)
- All providers degraded: Recommendations include least-bad option with warning

**Composite Score Formula:**
```
Score = (SuccessRate/100 * 0.5) + ((1 - P95ms/1000) * 0.3) + ((1 - BlockDelta/10) * 0.2)
```

This weighted formula prioritizes reliability (50%) over speed (30%) and freshness (20%), reflecting institutional operational priorities where correctness trumps performance.

### Configuration

```bash
# Use custom provider config file (defaults to config/providers.yaml)
./monitor snapshot --config /path/to/my_providers.yaml

# All commands support the --config flag
./monitor watch --config /path/to/custom_config.yaml
./monitor status --config /path/to/custom_config.yaml
```

## Provider Configuration

Edit `config/providers.yaml` to configure your endpoints:

```yaml
defaults:
  timeout: 10s              # Per-request timeout
  max_retries: 3            # Maximum retry attempts
  backoff_initial: 100ms    # Initial backoff duration
  backoff_max: 5s           # Maximum backoff duration (exponential backoff caps here)

providers:
  - name: alchemy
    url: https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
    type: enterprise
    # Optional: override default timeout for this provider
    # timeout: 15s

  - name: local-geth
    url: http://localhost:8545
    type: self_hosted
    timeout: 5s  # Lower timeout for local nodes (no network latency)

  - name: infura
    url: https://mainnet.infura.io/v3/YOUR_KEY
    type: public
```

**Provider Types:**

| Type | Description | Expected Performance | Typical Use Case |
|------|-------------|---------------------|------------------|
| `public` | Free tier endpoints | Rate limited, no SLA, variable latency | Development, testing, non-critical fallback |
| `enterprise` | Paid managed services | SLA-backed, dedicated capacity, optimized routing | Production primary endpoints |
| `self_hosted` | Self-operated nodes | Full control, no rate limits, network-dependent | Sovereignty requirements, high-volume operations |

**Configuration Best Practices:**

1. **Timeout Tuning:**
   - Local nodes: 3-5s (fast, predictable network)
   - Managed providers: 10-15s (accounts for network variability)
   - Public endpoints: 10-20s (may experience congestion)

2. **Retry Strategy:**
   - max_retries: 3 is recommended (balances resilience with responsiveness)
   - Higher values (5+) may mask fundamental provider issues
   - Lower values (1-2) provide faster failure detection but less resilience to transient errors

3. **Backoff Configuration:**
   - Initial backoff (100ms): First retry happens quickly for transient errors
   - Max backoff (5s): Prevents excessive wait times while avoiding thundering herd
   - Exponential progression: 100ms → 200ms → 400ms → 800ms → capped at 5s
   - Jitter: Random value (0 to backoff/2) added to prevent synchronized retries across multiple clients

**Production Configuration Example:**

For institutional deployments, a typical configuration might include:
- 2-3 enterprise providers (Alchemy, Infura, Blockdaemon) for primary traffic
- 1-2 self-hosted nodes for sovereignty and fallback
- 1 public endpoint for emergency fallback (rate-limited but zero cost)

The monitoring tool helps validate SLA compliance and identify optimal provider ordering for your specific workload and geographic location.

## Architecture

```
eth-rpc-monitor/
├── cmd/
│   └── monitor/
│       └── main.go              # CLI entry point, command definitions (snapshot, watch, blocks, txs, call, status)
│                                # Orchestrates data collection, metric calculation, and output rendering
│
├── internal/
│   ├── config/
│   │   └── config.go            # YAML configuration parsing and validation
│   │                            # Handles provider definitions, defaults, timeout settings
│   │
│   ├── rpc/
│   │   ├── client.go            # JSON-RPC client with production resilience patterns
│   │   │                        # - Exponential backoff with jitter (prevents thundering herd)
│   │   │                        # - Circuit breaker (3 failures → 30s cooldown)
│   │   │                        # - Automatic retry with configurable max attempts
│   │   │                        # - Error categorization (timeout, rate limit, server error, parse error)
│   │   │
│   │   ├── methods.go           # Ethereum RPC method implementations
│   │   │                        # - eth_getBlockByNumber (with full transaction support)
│   │   │                        # - eth_blockNumber
│   │   │                        # - eth_call (smart contract state queries)
│   │   │                        # Includes hex parsing, big.Int handling, and response marshaling
│   │   │
│   │   └── abi.go              # ABI encoding and decoding utilities
│   │       └── abi_test.go      # - Keccak-256 function selector computation
│   │                            # - Address padding and encoding (20 bytes → 32 bytes left-padded)
│   │                            # - uint256 decoding from hex
│   │                            # - Token amount formatting with decimals and thousand separators
│   │                            # - Address validation (length check, hex verification)
│   │
│   ├── metrics/
│   │   ├── collector.go         # Metrics aggregation and statistical analysis
│   │   │                        # - Latency percentile calculation (p50, p95, p99, max)
│   │   │                        # - Success rate computation
│   │   │                        # - Error categorization and counting
│   │   │                        # - Provider status determination (UP/SLOW/DEGRADED/DOWN)
│   │   │
│   │   ├── consistency.go       # Cross-provider consistency verification
│   │   │   └── consistency_test.go  # - Block height variance detection
│   │   │                        # - Two-phase hash consensus (compare only at reference height)
│   │   │                        # - Hash group identification (providers with matching hashes)
│   │   │                        # - Issue detection and reporting (reorgs, stale caches)
│   │
│   ├── provider/
│   │   └── selector.go          # Smart provider selection and health scoring
│   │                            # - Quick health check (configurable sample count)
│   │                            # - Composite score calculation (success 50%, latency 30%, freshness 20%)
│   │                            # - Exclusion logic (success <80%, blocks behind >5)
│   │                            # - Automatic ranking and best provider selection
│   │
│   └── output/
│       ├── terminal.go          # Terminal rendering with ANSI colors and tables
│       │                        # - Snapshot report formatting (provider performance, consistency analysis)
│       │                        # - Unicode box drawing characters for visual separation
│       │
│       ├── watch.go             # Live watch mode rendering
│       │                        # - Terminal clearing and redrawing for smooth updates
│       │                        # - Incident log with timestamps and severity
│       │                        # - Status transition tracking (UP→SLOW→DOWN)
│       │
│       ├── blocks.go            # Block data display
│       │                        # - Human-readable block metadata
│       │                        # - Gas utilization visualization
│       │                        # - Timestamp formatting (UTC + relative)
│       │
│       ├── call.go              # Smart contract call display
│       │                        # - Token balance formatting with decimals
│       │                        # - Calldata and response visualization
│       │
│       ├── status.go            # Provider status display
│       │                        # - Health ranking table
│       │                        # - Operational assessment with recommendations
│       │
│       └── json.go              # JSON output formatting
│                                # - Structured data for programmatic consumption
│                                # - Integration with monitoring platforms
│
└── config/
    └── providers.yaml           # Default provider configuration
                                 # - Example configurations for Alchemy, Infura, LlamaNodes, PublicNode
                                 # - Commented examples for self-hosted and enterprise setups
                                 # - Timeout and retry defaults
```

### Resilience Patterns

The RPC client (`internal/rpc/client.go`) implements production-grade resilience patterns critical for reliable infrastructure monitoring:

#### 1. Exponential Backoff with Jitter

**Purpose:** Prevent thundering herd when providers recover from outages.

**Implementation:**
```
Backoff duration = min(backoff_initial * 2^attempt, backoff_max) + random(0, backoff/2)
```

**Example sequence with defaults:**
- Attempt 1: 100ms base + 0-50ms jitter → 100-150ms wait
- Attempt 2: 200ms base + 0-100ms jitter → 200-300ms wait
- Attempt 3: 400ms base + 0-200ms jitter → 400-600ms wait
- Attempt 4: 800ms base + 0-400ms jitter → 800-1200ms wait
- Attempt 5+: Capped at 5s base + 0-2.5s jitter → 5s-7.5s wait

**Why jitter matters:**
Without jitter, multiple clients retrying simultaneously would create synchronized load spikes. Jitter staggers retries across time, allowing providers to recover gracefully.

**Institutional relevance:**
During provider outages, dozens of monitoring clients may be retrying simultaneously. Jittered backoff prevents overwhelming the provider the moment it comes back online, which could trigger a second failure.

#### 2. Circuit Breaker

**Purpose:** Stop hammering dead endpoints, allow graceful degradation.

**State machine:**
```
CLOSED (normal) → 3 consecutive failures → OPEN (blocked)
OPEN → 30 second cooldown → HALF_OPEN (single probe allowed)
HALF_OPEN → success → CLOSED | failure → OPEN
```

**Implementation details:**
- **Threshold:** 3 consecutive failures trigger circuit opening
- **Cooldown:** 30 seconds before allowing probe requests
- **Half-open state:** Next request after cooldown acts as a probe
  - Success: Circuit closes, normal operation resumes
  - Failure: Circuit reopens, 30-second cooldown restarts

**Operational impact:**
- **Fast failure:** Requests to circuit-broken providers return immediately with "circuit breaker open" error
- **Resource conservation:** No wasted retries against confirmed-dead endpoints
- **Automatic recovery:** Circuit tests provider health after cooldown without manual intervention

**Monitoring integration:**
The circuit breaker state is exposed via:
- `IsCircuitOpen()` - Current circuit state
- `ConsecutiveFailures()` - Failure count toward threshold

This allows external monitoring to track circuit state and alert on frequent circuit breaks indicating provider instability.

**Why 3 failures / 30 seconds:**
- 3 failures provides confidence the provider is truly down (not a single transient error)
- 30 seconds allows time for provider-side recovery without excessive wait
- These values are production-tested defaults but could be made configurable for specific environments

#### 3. Graceful Degradation

**Purpose:** Partial failures don't block healthy providers, system remains operational with reduced capacity.

**Implementation:**
- **Concurrent requests:** All providers are queried in parallel using `errgroup`
- **Independent failure handling:** Each provider's failure is isolated and recorded
- **Best-effort results:** Even if some providers fail, successful responses are collected and reported
- **Consistency checks:** Work with whatever data is available (minimum 2 providers for meaningful comparison)

**Example scenario:**
```
4 providers configured: Alchemy, Infura, LlamaNodes, PublicNode
- Alchemy: Success (150ms)
- Infura: Success (180ms)
- LlamaNodes: Timeout (circuit breaker opens)
- PublicNode: Rate limited (429)

Result: Report shows Alchemy and Infura data, warns about LlamaNodes/PublicNode failures
System remains operational with 50% provider capacity
```

**Institutional value:**
In production, partial outages are more common than total failures. A monitoring system that fails completely when any provider is down provides no visibility during the exact time you need it most. Graceful degradation ensures you always have visibility into the providers that *are* working.

#### 4. Error Categorization

All errors are classified into actionable categories:

| Error Type | Meaning | Retry Strategy | Operational Response |
|------------|---------|----------------|---------------------|
| **Timeout** | Request exceeded deadline | Retry with backoff | Check network connectivity, provider health |
| **Rate Limit** (429) | Exceeded API quota | Retry with backoff | Upgrade provider tier or reduce request rate |
| **Server Error** (5xx) | Provider infrastructure issue | Retry with backoff | Provider-side problem, failover recommended |
| **Parse Error** | Invalid JSON or unexpected format | No retry | Investigate provider API changes |
| **RPC Error** | Application-level error (invalid params, etc.) | No retry | Fix request parameters |
| **Circuit Open** | Circuit breaker active | No retry | Wait for cooldown or failover |

**Why categorization matters:**
Different error types require different operational responses. Timeouts may indicate network issues. Rate limits require throttling. Server errors suggest provider problems. Parse errors indicate API contract changes.

The categorized error counts in snapshot reports help diagnose systemic issues (e.g., 90% timeouts = network problem, 90% rate limits = quota exhaustion).

## RPC Methods and Institutional Impact

### eth_blockNumber

**Purpose:** Query the current block height of a provider.

**Institutional use cases:**
- **Sync status verification:** Ensure nodes are keeping up with chain tip
- **Freshness detection:** Identify providers serving stale data
- **Load balancer health checks:** Route traffic only to synced nodes

**What this tool checks:**
- Current block height from each provider
- Height variance across providers (threshold: ≤2 blocks / ~24 seconds)
- Block delta (blocks behind maximum) for each provider

**Failure modes:**
- **Timeout:** Provider unreachable, trigger failover to backup
- **Stale height:** Provider is responding but significantly behind chain tip
  - 1-2 blocks behind: Acceptable (normal propagation delay)
  - 3-5 blocks behind: Degraded (investigate sync issues)
  - 6+ blocks behind: Critical (data integrity risk for time-sensitive operations)

**Example operational scenario:**
A custody platform receives a deposit transaction in block N. Before crediting the user's account, the platform queries multiple providers to confirm the transaction. If Provider A reports block N but Provider B is still at block N-10, using Provider B could lead to:
- Delayed transaction confirmation
- Incorrect balance calculations
- User complaints about "missing" deposits

The monitoring tool's height variance detection catches this scenario before it impacts users.

### eth_getBlockByNumber

**Purpose:** Retrieve complete block data including hash, timestamp, transactions, and gas metrics.

**Institutional use cases:**
- **Transaction reconciliation:** Verify all expected transactions are in a block
- **Reorg detection:** Compare block hashes at the same height across providers
- **Data consistency verification:** Ensure providers agree on canonical chain state
- **Forensic analysis:** Investigate historical block data during incident response

**What this tool checks:**
- Block hash agreement at a common reference height (minimum height all providers have)
- Two-phase consistency verification:
  1. **Phase 1:** Collect latest block heights from all providers
  2. **Phase 2:** Fetch block hashes at the minimum height (ensuring fair comparison)
- Hash grouping: Identify which providers agree (majority) vs. disagree (minority)

**Why institutions care:**

Two providers reporting the same height but different hashes indicates one of three critical scenarios:

1. **Chain reorg in progress:**
   - One provider updated to the new canonical chain, another hasn't yet
   - Expected during uncle blocks or validator coordination issues
   - Duration: Typically resolves within 1-2 blocks (~12-24 seconds)
   - Risk: Transactions in the old fork may not be in the new fork (double-spend vulnerability)

2. **Stale cache:**
   - Provider serving cached data from an orphaned fork
   - May persist indefinitely if cache invalidation is broken
   - Risk: Incorrect transaction confirmations, balance discrepancies

3. **Network partition:**
   - Provider is seeing a different view of the network (extremely rare)
   - Indicates serious infrastructure issues
   - Risk: Complete data inconsistency, potential for significant operational errors

**For custody operations and reconciliation:**
Hash mismatches are critical because:
- **Double-counting risk:** A transaction in an orphaned block may be double-counted if not properly handled
- **Missing transactions:** Transactions in the canonical chain may be missed if stale data is used
- **Compliance issues:** Incorrect transaction histories can violate audit and regulatory requirements

**Example critical scenario:**
A trading desk executes a large ETH sale, broadcasting the transaction to Provider A. The transaction is included in block N on Provider A. The desk's settlement system, using Provider B for confirmation, doesn't see the transaction in block N (due to hash mismatch from a reorg). The desk's system incorrectly assumes the transaction failed and resubmits, resulting in a double-sell and significant capital loss.

**Tool's consistency detection:**
The monitor flags hash mismatches immediately:
```
⚠ Provider(s) [PublicNode] report different block hash at height 19234561
   (possible reorg or stale cache)
```

This allows operators to:
1. Investigate the discrepancy before relying on the data
2. Temporarily exclude the minority provider from production traffic
3. Wait for reorg resolution or escalate to provider support if cache issue persists

### eth_call

**Purpose:** Execute read-only smart contract function calls without sending a transaction.

**Institutional use cases:**
- **Balance verification:** Query ERC-20 token balances for custody reconciliation
- **Contract state inspection:** Read configuration parameters, allowances, ownership
- **Pre-flight validation:** Simulate transaction outcomes before broadcasting
- **Liquidation monitoring:** Check collateralization ratios in DeFi protocols

**What this tool demonstrates:**
- **ABI encoding:** Proper construction of calldata from function signature and parameters
- **Function selector computation:** Keccak-256 hashing and 4-byte extraction
- **Address encoding:** 20-byte address → 32-byte left-padded parameter
- **Return value decoding:** Hex-encoded uint256 → human-readable token amount
- **Decimal handling:** Converting raw token amounts (e.g., 100000000) to decimal notation (100.000000 USDC)

**Implementation reference (USDC balance query):**

1. **Function signature:** `balanceOf(address)` (ERC-20 standard)
2. **Selector computation:**
   - Keccak256("balanceOf(address)") → `0x70a08231...` (first 4 bytes)
3. **Parameter encoding:**
   - Input address: `0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045`
   - Padded: `0x000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045`
4. **Final calldata:**
   ```
   0x70a08231000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045
   ```
5. **RPC request:**
   ```json
   {
     "jsonrpc": "2.0",
     "method": "eth_call",
     "params": [
       {
         "to": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
         "data": "0x70a08231000000000000000000000000d8da6bf26964af9d7eed9e03e53415d37aa96045"
       },
       "latest"
     ],
     "id": 1
   }
   ```
6. **RPC response:**
   ```json
   {
     "jsonrpc": "2.0",
     "id": 1,
     "result": "0x0000000000000000000000000000000000000000000000000000000005f5e100"
   }
   ```
7. **Decoding:**
   - Hex: `0x05f5e100` = 100000000 (decimal)
   - With 6 decimals: 100.000000 USDC
8. **Formatted output:**
   ```
   Balance: 100.000000 USDC
   ```

**Educational value:**
This implementation serves as a complete reference for:
- Ethereum ABI encoding specification (from yellow paper)
- Keccak-256 hashing (using golang.org/x/crypto/sha3)
- Big integer handling for uint256 types
- ERC-20 standard interface implementation
- Provider response consistency verification (different providers should return identical balances)

**Extensibility:**
The ABI encoding logic can be extended to support:
- Other ERC-20 methods (totalSupply, allowance, symbol, decimals)
- ERC-721 NFT queries (ownerOf, balanceOf, tokenURI)
- Custom contract interfaces (governance voting, staking rewards, etc.)
- Multi-call aggregation (batch multiple queries into one RPC call)

## Deployment Reality

This tool monitors public RPC endpoints for demonstration and portability. In production institutional environments, the infrastructure landscape varies:

| Environment | Infrastructure | Performance Expectations | Use Case |
|-------------|---------------|-------------------------|----------|
| **Public Tier** | Free endpoints (Alchemy, Infura, LlamaNodes) | Rate limited (≤100 req/s), no SLA, variable latency (100-500ms), occasional downtime | Development, testing, low-volume applications, emergency fallback |
| **Enterprise Tier** | Paid managed services (Alchemy Growth/Scale, Infura Enterprise, Blockdaemon, Figment) | SLA-backed (99.9%+), dedicated capacity, optimized routing, <100ms latency, archive data access | Production primary endpoints, high-volume applications, institutional custody |
| **Self-Hosted** | Geth/Erigon execution + Prysm/Lighthouse consensus | Full control, no rate limits, network-dependent latency (LAN: <10ms, WAN: 50-200ms), operational overhead | Sovereignty requirements, MEV, high-frequency trading, regulatory compliance |
| **Hybrid** | Primary self-hosted + enterprise fallback | Best of both worlds, operational complexity | Large institutional deployments, critical infrastructure |

**The monitoring logic is identical regardless of endpoint type.** Only the SLAs, rate limits, and expected performance differ.

### Real-World Deployment Patterns

**Pattern 1: Multi-provider redundancy (most common)**
```yaml
Primary: Alchemy Enterprise (SLA-backed, low latency)
Secondary: Infura Enterprise (geographic diversity)
Tertiary: Self-hosted Geth (sovereignty, no rate limits)
Emergency: Public Alchemy (zero cost, rate limited)
```

**Pattern 2: Geographic distribution**
```yaml
US East: Alchemy endpoint in us-east-1
EU West: Infura endpoint in eu-west-1
Asia Pacific: Blockdaemon endpoint in ap-southeast-1
```

**Pattern 3: Workload segregation**
```yaml
Read-heavy queries (balances, block data): Managed providers (Alchemy/Infura)
Transaction broadcast: Self-hosted nodes (guaranteed delivery, no censorship)
Archive queries (historical state): Enterprise with archive access
```

**Monitoring role in these patterns:**

This tool helps validate that your provider mix is actually delivering the expected performance and consistency:

1. **SLA verification:** Enterprise providers should show <100ms p95 latency, 99.9%+ success rate
2. **Geographic optimization:** Endpoints closer to your infrastructure should show lower latency
3. **Consistency validation:** All providers should agree on block hashes (reorgs aside)
4. **Failover testing:** Circuit breaker and retry logic simulate provider outages
5. **Cost optimization:** If public tier performs adequately for your workload, enterprise tier may not be necessary

**Status command for capacity planning:**

The `status` command's provider ranking directly informs failover configuration:
```
Recommended priority: Alchemy → Infura → LlamaNodes
```

This ranking should be reflected in your application's provider list ordering, ensuring traffic is routed to the best-performing endpoint first.

## Sample Output

### Snapshot Report (Terminal Format)

```
╭─────────────────────────────────────────────────────────────────╮
│           Ethereum RPC Infrastructure Report                    │
│                    2025-01-14 14:32:01 UTC                      │
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

Error Breakdown
┌──────────────┬──────────┬────────────┬─────────┐
│ Provider     │ Timeouts │ Rate Limit │ Other   │
├──────────────┼──────────┼────────────┼─────────┤
│ Alchemy      │ 0        │ 0          │ 0       │
│ Infura       │ 1        │ 0          │ 0       │
│ LlamaNodes   │ 2        │ 0          │ 0       │
│ PublicNode   │ 15       │ 3          │ 2       │
└──────────────┴──────────┴────────────┴─────────┘

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

Hash Consensus (at reference height 19,234,561)
  ┌──────────────────────────────────────────────────────────────────┐
  │ Hash Group 1 (Majority - 3 providers):                           │
  │   0x742e...a3f1                                                  │
  │   Providers: Alchemy, Infura, LlamaNodes                         │
  │                                                                  │
  │ Hash Group 2 (Minority - 1 provider):                            │
  │   0x8b2d...c4e7                                                  │
  │   Providers: PublicNode                                          │
  └──────────────────────────────────────────────────────────────────┘

Operational Assessment
  ┌─────────────────────────────────────────────────────────────────┐
  │ ✗ PublicNode unsuitable for production use                      │
  │   - 66% failure rate (20 failures out of 30 attempts)           │
  │   - 6 blocks behind (72 seconds stale)                          │
  │   - Serving stale block hash (possible cache issue)             │
  │                                                                 │
  │ ⚠ LlamaNodes showing elevated latency, acceptable for fallback  │
  │   - P95 latency 876ms exceeds 500ms threshold                   │
  │   - 1 block behind (within acceptable drift)                    │
  │                                                                 │
  │ ✓ Alchemy, Infura performing within expected parameters         │
  │   - <300ms p99 latency                                          │
  │   - ≥98% success rate                                           │
  │   - Full sync with network tip                                  │
  │   - Block hash consensus confirmed                              │
  │                                                                 │
  │ Recommended priority: Alchemy → Infura → LlamaNodes             │
  │                                                                 │
  │ ⚠ WARNING: Hash mismatch detected at height 19,234,561          │
  │   PublicNode reporting different block hash than majority       │
  │   Possible reorg or stale cache - verify before using this data │
  └─────────────────────────────────────────────────────────────────┘
```

### Raw JSON Output (Example)

```json
{
  "timestamp": "2025-01-14T14:32:01Z",
  "sampleCount": 30,
  "providers": {
    "Alchemy": {
      "name": "Alchemy",
      "status": "UP",
      "latencyAvg": 0.152,
      "latencyP50": 0.138,
      "latencyP95": 0.201,
      "latencyP99": 0.287,
      "latencyMax": 0.312,
      "successRate": 100.0,
      "totalCalls": 30,
      "failures": 0,
      "timeouts": 0,
      "rateLimits": 0,
      "serverErrors": 0,
      "parseErrors": 0,
      "otherErrors": 0,
      "latestBlock": 19234567,
      "latestBlockHash": "0x742e...a3f1"
    },
    "Infura": {
      "name": "Infura",
      "status": "UP",
      "latencyAvg": 0.167,
      "latencyP50": 0.145,
      "latencyP95": 0.219,
      "latencyP99": 0.298,
      "latencyMax": 0.334,
      "successRate": 98.0,
      "totalCalls": 30,
      "failures": 1,
      "timeouts": 1,
      "rateLimits": 0,
      "serverErrors": 0,
      "parseErrors": 0,
      "otherErrors": 0,
      "latestBlock": 19234567,
      "latestBlockHash": "0x742e...a3f1"
    }
  },
  "consistency": {
    "heights": {
      "Alchemy": 19234567,
      "Infura": 19234567,
      "LlamaNodes": 19234566,
      "PublicNode": 19234561
    },
    "maxHeight": 19234567,
    "heightVariance": 6,
    "heightConsensus": false,
    "authoritativeProvider": "Alchemy",
    "referenceHeight": 19234561,
    "hashes": {
      "Alchemy": "0x742e...a3f1",
      "Infura": "0x742e...a3f1",
      "LlamaNodes": "0x742e...a3f1",
      "PublicNode": "0x8b2d...c4e7"
    },
    "hashConsensus": false,
    "hashGroups": [
      {
        "hash": "0x742e...a3f1",
        "providers": ["Alchemy", "Infura", "LlamaNodes"]
      },
      {
        "hash": "0x8b2d...c4e7",
        "providers": ["PublicNode"]
      }
    ],
    "consistent": false,
    "issues": [
      "Block height variance of 6 blocks exceeds threshold",
      "Provider(s) [PublicNode] report different block hash at height 19234561 (possible reorg or stale cache)"
    ]
  }
}
```

**JSON output use cases:**

1. **Monitoring platform integration:**
   ```bash
   ./monitor snapshot --format json | jq '.providers.Alchemy.latencyP95'
   # Feed to Prometheus, Datadog, Grafana, etc.
   ```

2. **Automated alerting:**
   ```bash
   successRate=$(./monitor snapshot --format json | jq '.providers.Alchemy.successRate')
   if (( $(echo "$successRate < 95" | bc -l) )); then
     send_alert "Alchemy success rate dropped to $successRate%"
   fi
   ```

3. **Historical trending:**
   ```bash
   # Collect snapshots every 60 seconds and store for analysis
   while true; do
     ./monitor snapshot --format json >> monitoring_log.jsonl
     sleep 60
   done
   ```

4. **SLA compliance reporting:**
   Parse JSON output to generate monthly uptime reports, latency distributions, and consistency metrics for management and auditors.

## Development

### Prerequisites

- **Go 1.21+** (tested with Go 1.24)
- Internet connectivity for RPC endpoint access
- (Optional) Local Ethereum node for self-hosted provider testing

### Build

```bash
# Standard build
go build -o monitor ./cmd/monitor

# Build with version information
go build -ldflags="-X 'main.Version=1.0.0'" -o monitor ./cmd/monitor

# Build for different platforms (cross-compilation)
GOOS=linux GOARCH=amd64 go build -o monitor-linux ./cmd/monitor
GOOS=darwin GOARCH=arm64 go build -o monitor-macos-arm ./cmd/monitor
```

### Test

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detector (detect concurrency issues)
go test -race ./...

# Run specific package tests
go test ./internal/rpc/...
go test ./internal/metrics/...

# Verbose test output
go test -v ./...
```

**Test coverage:**
Key packages with test coverage:
- `internal/rpc/abi_test.go`: ABI encoding/decoding logic
- `internal/metrics/consistency_test.go`: Consistency checking algorithms

**Future testing enhancements:**
- Integration tests with mock RPC servers
- Benchmark tests for latency calculation performance
- Fuzzing for ABI encoding edge cases

### Dependencies

| Package | Purpose | Version |
|---------|---------|---------|
| [github.com/spf13/cobra](https://github.com/spf13/cobra) | CLI framework with subcommands, flags, help generation | v1.8.0 |
| [github.com/fatih/color](https://github.com/fatih/color) | ANSI terminal colors for status indicators | v1.16.0 |
| [github.com/rodaine/table](https://github.com/rodaine/table) | ASCII table formatting for tabular output | v1.2.0 |
| [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) | YAML parsing for configuration files | v3.0.1 |
| [golang.org/x/crypto](https://golang.org/x/crypto) | Keccak-256 hashing for ABI function selectors | v0.47.0 |
| [golang.org/x/sync/errgroup](https://golang.org/x/sync) | Concurrent goroutine management with error handling | v0.19.0 |

**Dependency management:**
This project uses Go modules (`go.mod`) for reproducible builds. All dependencies are pinned to specific versions to ensure consistent behavior across environments.

**No external RPC libraries:**
Intentionally does not use libraries like `go-ethereum/ethclient` to demonstrate the raw JSON-RPC protocol from first principles. This makes the code more educational and avoids heavyweight dependencies.

### Project Structure Rationale

**Why `internal/` package:**
Go's `internal/` directory enforces package-private visibility, preventing external projects from importing these packages. This ensures the codebase remains focused on the CLI tool's needs without maintaining a stable public API.

**Separation of concerns:**
- **`rpc/`**: Protocol-level communication (JSON-RPC, HTTP, retries)
- **`metrics/`**: Business logic (statistical analysis, consistency checking)
- **`output/`**: Presentation layer (terminal formatting, JSON serialization)
- **`config/`**: Configuration management (YAML parsing, validation)
- **`provider/`**: Higher-level provider management (health checks, ranking)

This separation makes the codebase easier to test, extend, and maintain.

## Roadmap

**Completed phases:**
- ✅ Phase 1: Snapshot mode with latency percentiles and consistency checking
- ✅ Phase 2: Watch mode with real-time monitoring and incident logging
- ✅ Phase 3: Block and transaction query commands
- ✅ Phase 4: Smart contract call support (ERC-20 balance queries)
- ✅ Phase 5: Smart provider selection with health-based ranking

**Future enhancements:**
- **Automated provider recommendations:** Machine learning-based scoring that adapts to workload patterns
- **Additional RPC methods:** eth_getTransactionReceipt, eth_estimateGas, eth_getLogs
- **WebSocket support:** Subscription-based monitoring (newHeads, logs, pendingTransactions)
- **Prometheus metrics export:** Native integration with Prometheus for long-term monitoring
- **Alert webhook support:** POST to external URLs when thresholds are breached
- **Historical database:** Store monitoring data in SQLite or PostgreSQL for trend analysis
- **Web dashboard:** Browser-based UI for visualizing provider performance
- **Multi-chain support:** Extend to Polygon, Arbitrum, Optimism, Base

## License

MIT License - see LICENSE file for details.

## Author

**Daniel Magro** - [LinkedIn](https://linkedin.com/in/daniel-magro-2323941a2)

**Background:** Institutional fixed income trading and portfolio management, now focused on Ethereum infrastructure. This project fuses traditional finance operational rigor with blockchain infrastructure requirements, evolving through iterative enhancements to address real-world institutional needs.

**Professional context:**
In traditional finance, infrastructure monitoring is a mature discipline with established tools (Bloomberg Terminal, FIX protocol monitors, market data feed validators). This project applies that same operational rigor to blockchain infrastructure, where monitoring tooling is still developing.

**Design philosophy:**
- **Production-grade patterns:** Circuit breakers, exponential backoff, graceful degradation aren't academic exercises—they're requirements for systems handling millions of dollars in transaction value
- **Institutional focus:** Features prioritize reliability and data correctness over speed or convenience
- **Educational clarity:** Code is written to be read and learned from, with extensive comments explaining *why* choices were made
- **Iterative development:** Each phase adds meaningful functionality while maintaining code quality and clarity

**Contact:**
For questions, feedback, or collaboration opportunities, please reach out via [LinkedIn](https://linkedin.com/in/daniel-magro-2323941a2) or open an issue on [GitHub](https://github.com/dmagro/eth-rpc-monitor).

---

**Repository:** [github.com/dmagro/eth-rpc-monitor](https://github.com/dmagro/eth-rpc-monitor)

**Last updated:** January 14, 2025
