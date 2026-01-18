# Ethereum RPC Infrastructure Monitor

A command-line tool for monitoring **Ethereum RPC endpoint reliability, performance, and data correctness**.

This tool is designed primarily for **institutional operations, infrastructure, and risk teams** who rely on Ethereum data and need visibility into the health of their upstream dependencies.

> **Note (plain-language):**
> Ethereum applications do not talk to the blockchain directly.
> They talk to *RPC providers* (Alchemy, Infura, Blockdaemon, self-hosted nodes).
> If those providers are slow, stale, or inconsistent, your application inherits that risk.

At a high level, this tool helps answer one critical operational question:

> **"Can we trust the Ethereum data our systems are using right now?"**

---

## Why This Exists

Any system operating on Ethereum—custody platforms, trading systems, staking infrastructure, settlement pipelines, analytics, or compliance tooling—depends on **RPC providers** to answer questions like:

* What is the latest block height?
* Has this transaction been included?
* What is the balance of this address?
* What transactions occurred in a given block?

That dependency introduces **non-obvious failure modes**.

> **Operational scenario:**
> A provider is online, responding quickly, and returning valid JSON — but it is several blocks behind the network.
> From the application's perspective, everything "looks fine," yet users see missing deposits or delayed confirmations.

This tool exists to **detect those conditions before they cause user-facing or financial impact**.

> **Traditional finance analogy:**
> This is the blockchain equivalent of monitoring market data feeds, trade confirmations, and settlement systems — correctness matters more than speed.

---

## Features

### Snapshot Mode

Generate a **one-time diagnostic report** across all configured providers.

Snapshot mode is intended for:

* Provider evaluation
* SLA validation
* Capacity planning
* Incident post-mortems

It reports:

* **Latency percentiles** (p50, p95, p99, max) with comprehensive statistical analysis
* **Success rates and error classification** (timeouts, rate limits, server errors, parse errors)
* **Cross-provider block height consistency** with variance detection and drift analysis
* **Block hash verification** at a common reference height to detect chain reorgs and stale caches
* **Operational assessment** with automated provider ranking and failover recommendations

> **Note (plain-language): Block height**
> Block height (also called block number) is Ethereum's "ledger page number."
> If providers disagree here, one of them is behind.

> **Why this matters:**
> Using a lagging provider can delay deposits, misreport balances, or cause reconciliation failures.

Snapshot mode collects multiple samples (default: 30) with configurable intervals to produce **statistically meaningful results**, rather than relying on a single request.

**Sample Configuration Trade-offs:**
- **Fewer samples (10-20):** Fast execution, suitable for quick checks, higher variance
- **Default (30 samples):** Balanced statistical significance and execution time (~3 seconds)
- **More samples (50-100):** Higher confidence in percentile calculations, longer execution (~5-10 seconds)

---

### Watch Mode

Real-time monitoring with live updates and incident tracking.

Watch mode provides:

* **Provider status at a glance** (UP / SLOW / DEGRADED / DOWN) with visual indicators
* **Live latency and block height** for each provider, updated at configurable intervals
* **Rolling incident log** capturing status changes, latency spikes, and provider failures
* **Block hash consensus tracking** performed every 3 refresh cycles to balance thoroughness with performance
* **Immediate visibility** when infrastructure degrades, enabling rapid incident response

> **Note (plain-language): Hash consensus**
> Providers can report the same block number but disagree on the block's contents.
> Hash consensus confirms they are seeing the *same version* of the chain.

> **Operational scenario:**
> Three providers agree on a block hash. One does not.
> That outlier is unsafe to use until it realigns with the majority — this could indicate a stale cache or an ongoing chain reorganization.

Watch mode implements automatic refresh (default: 5 seconds) and maintains a rolling incident history to help operators identify patterns in provider behavior.

---

## On-Demand Queries

In addition to continuous monitoring, the tool provides **explicit inspection commands** for real-time investigation.

---

### Block Data Retrieval (`blocks` command)

Fetch details of a block by number, hex value, or tag (`latest`).

Displays:

* **Block number and hash**
* **Parent hash** (links to previous block)
* **Timestamp** (Unix and human-readable)
* **Gas used and gas limit with utilization percentage**
* **Base fee per gas** (post-EIP-1559 blocks)
* **Transaction count**

> **Note (plain-language): Parent hash**
> Each block references the hash of the previous block.
> This links blocks into a continuous chain.

> **Why this matters:**
> If the parent hash doesn't match expected values, the chain has forked or reorganized.

> **Note (plain-language): Gas utilization**
> Gas utilization shows how full a block was (0-100%).

> **Operational scenario:**
> Multiple consecutive blocks at 95–100% utilization explain slow confirmations and rising fees — not an application bug.
> This is network congestion, not a provider issue.

The `--raw` flag displays the full JSON-RPC response for debugging or audit purposes.

**Example usage:**
```bash
# Fetch latest block using auto-selected provider
./bin/monitor blocks latest

# Fetch specific block with explicit provider
./monitor blocks 19000000 --provider alchemy

# Show raw JSON-RPC response for audit
./bin/monitor blocks latest --raw
```

---

### Transaction Listing (`txs` command)

Display transactions within a block.

Each transaction includes:

* **Transaction hash**
* **From and to addresses**
* **Value transferred (ETH)**
* **Gas limit and gas price / fee**
* **Transaction index and nonce**
* **Calldata summary** (truncated for readability)

> **Note (plain-language): Calldata**
> Calldata encodes *what function was called and with what inputs*.
> For simple ETH transfers, calldata is empty.
> For contract interactions, it contains the encoded function call and parameters.

> **Operational scenario:**
> Two transactions send 0 ETH.
> One approves a token transfer; the other executes a trade.
> Without calldata inspection, they look identical — but their economic impact is vastly different.

**Example usage:**
```bash
# List transactions in block (default: first 25)
./bin/monitor txs 19000000

# Show all transactions
./bin/monitor txs 19000000 --limit 0

# Compare transaction lists across providers
./bin/monitor txs 19000000 --provider alchemy
./bin/monitor txs 19000000 --provider infura
```

---

### Smart Contract Call (`call` command)

Query on-chain data using `eth_call` (read-only smart contract execution).

**Example: USDC Balance Query**
```bash
./bin/monitor call usdc balance 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045
```

> **Note (plain-language): `eth_call`**
> `eth_call` simulates a contract function without creating a transaction or spending gas.
> It returns the result immediately — no waiting for block inclusion.

> **Why this matters:**
> Custody platforms rely on `eth_call` for constant balance verification.
> Trading systems use it to check token allowances before executing swaps.
> Any error here means incorrect data in production systems.

**What this demonstrates:**

* **Function selector computation**
  `balanceOf(address)` → Keccak-256 hash → First 4 bytes → `0x70a08231`

* **ABI parameter encoding**
  Address `0xd8dA...6045` → Left-padded to 32 bytes → `0x000...d8da...6045`

* **Return value decoding**
  Hex result `0x05f5e100` → Decimal 100000000 → 100.000000 USDC (6 decimals)

* **Token decimal handling**
  Raw integers → Human-readable amounts with proper decimal placement

> **Note (plain-language): Token decimals**
> Tokens store integers, not human-readable balances.
>
> * USDC: 6 decimals (1000000 = 1.000000 USDC)
> * WETH/USDT: 18 decimals (1000000000000000000 = 1.000000000000000000 ETH)

> **Operational scenario:**
> A system forgets to account for decimals.
> A 100 USDC balance (stored as 100000000) appears as "100000000.00 USDC" in reports.
> This triggers compliance alerts, audit failures, and customer service escalations.

The `--raw` flag shows exact calldata and hex-encoded responses for educational or debugging purposes.

---

### Status Check (`status` command)

Instant health summary and provider ranking.

Evaluates each provider on:

* **Success rate** (% of requests that succeed)
* **Latency** (P95, milliseconds)
* **Freshness** (blocks behind network maximum)

Outputs:

* **Health rankings** sorted by composite score
* **Exclusions** (providers unsuitable for production use)
* **Recommended provider order** for failover configuration

> **Operational scenario:**
> A fallback provider is responsive but 8 blocks behind (96 seconds at 12s/block).
> Status flags it as unsafe before users report "missing" deposits.
> Operations can proactively exclude it from rotation until it catches up.

**Composite Score Formula:**
```
Score = (SuccessRate/100 * 0.5) + ((1 - P95ms/1000) * 0.3) + ((1 - BlockDelta/10) * 0.2)
```

This weighted formula prioritizes:
- **Reliability (50%):** Can we trust it to respond?
- **Speed (30%):** Will it respond quickly enough?
- **Freshness (20%):** Is it seeing current data?

**Exclusion criteria:**
- Success rate <80%: Too unreliable for production traffic
- Block delta >5: Serving stale data (>60 seconds behind)

**Example output:**
```
Provider Rankings
┌──────────────┬─────────┬─────────┬────────┬─────────┬───────┬────────┐
│ Provider     │ Status  │ Success │ Avg    │ P95     │ Block │ Score  │
├──────────────┼─────────┼─────────┼────────┼─────────┼───────┼────────┤
│ Alchemy      │ ✓ UP    │ 100.0%  │ 145ms  │ 189ms   │ 0     │ 0.912  │
│ Infura       │ ✓ UP    │ 100.0%  │ 167ms  │ 203ms   │ 0     │ 0.896  │
│ LlamaNodes   │ ⚠ SLOW  │ 100.0%  │ 423ms  │ 512ms   │ -1    │ 0.682  │
│ PublicNode   │ ✗ DOWN  │ 40.0%   │ —      │ —       │ -8    │ 0.124  │
└──────────────┴─────────┴─────────┴────────┴─────────┴───────┴────────┘

Recommended priority: Alchemy → Infura → LlamaNodes
```

---

## Smart Provider Selection

When no `--provider` flag is specified, the tool automatically selects the best available provider:

1. **Quick health check** (3 samples per provider, 10-second timeout)
2. **Score calculation** based on success rate, latency, and freshness
3. **Exclusion logic** removes unsafe providers
4. **Best selection** uses highest-scoring available provider

> **Note (plain-language):**
> Partial data is better than no data during incidents — but only if it's clearly labeled as degraded.

**Fallback behavior:**
If all providers are degraded or health check fails, the system falls back to the first configured provider with a warning, ensuring operations continue even during partial outages.

---

## Installation

```bash
git clone https://github.com/dmagro/eth-rpc-monitor
cd eth-rpc-monitor
go build -o bin/monitor ./cmd/monitor
```

**Requirements:**
- Go 1.21+ (tested with Go 1.24)
- Network access to configured RPC endpoints

**Build Output:**
- The compiled binary (~12MB) is placed in `bin/monitor` and is a self-contained executable with no runtime dependencies
- Generated reports are saved to `reports/` directory (gitignored)
- Both `bin/` and `reports/` directories are gitignored and excluded from version control

---

## Usage

### Snapshot Report

```bash
# Run with default settings (30 samples per provider, 100ms interval)
./bin/monitor snapshot

# Custom sample count and interval for high-resolution profiling
./bin/monitor snapshot --samples 50 --interval 100

# JSON output for automation and integration with monitoring systems
./bin/monitor snapshot --format json

# Save JSON report to timestamped file in reports/ directory
./bin/monitor snapshot --format json --output reports/

# Save JSON report to specific file
./bin/monitor snapshot --format json --output my-report.json
```

**Interval tuning:**
- Lower intervals (50ms): Faster execution but may trigger rate limits on public endpoints
- Higher intervals (200-500ms): More suitable for public tier endpoints with rate limits
- The interval provides a simple throttle mechanism to prevent overwhelming providers

**JSON output use cases:**
- Integration with monitoring platforms (Prometheus, Datadog, Grafana)
- Automated alerting based on success rate or latency thresholds
- Historical trending and SLA compliance reporting

---

### Live Monitoring

```bash
# Default 5-second refresh interval
./monitor watch

# Faster refresh (2s) for active incident response
./monitor watch --refresh 2s

# Slower refresh (30s) for low-priority monitoring or rate-limited endpoints
./monitor watch --refresh 30s
```

**Watch mode implementation details:**
- Uses terminal clearing and redrawing for smooth live updates without scrolling
- Maintains a rolling incident log (last 10 events) with timestamps and severity
- Tracks status transitions (UP→SLOW, SLOW→DOWN, etc.) to identify flapping
- Supports graceful shutdown via Ctrl+C, preserving final state in terminal

---

### Provider Configuration

Edit `config/providers.yaml` to configure your endpoints:

```yaml
defaults:
  timeout: 10s              # Per-request timeout
  max_retries: 3            # Maximum retry attempts
  backoff_initial: 100ms    # Initial backoff duration
  backoff_max: 5s           # Maximum backoff duration

providers:
  - name: alchemy
    url: https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
    type: enterprise

  - name: local-geth
    url: http://localhost:8545
    type: self_hosted
    timeout: 5s  # Lower timeout for local nodes

  - name: infura
    url: https://mainnet.infura.io/v3/YOUR_KEY
    type: public
```

**Configuration best practices:**

1. **Timeout tuning:**
   - Local nodes: 3-5s (fast, predictable network)
   - Managed providers: 10-15s (accounts for network variability)
   - Public endpoints: 10-20s (may experience congestion)

2. **Retry strategy:**
   - max_retries: 3 is recommended (balances resilience with responsiveness)
   - Higher values (5+) may mask fundamental provider issues
   - Lower values (1-2) provide faster failure detection but less resilience

3. **Backoff configuration:**
   - Initial backoff (100ms): First retry happens quickly for transient errors
   - Max backoff (5s): Prevents excessive wait times while avoiding thundering herd
   - Exponential progression: 100ms → 200ms → 400ms → 800ms → capped at 5s
   - Jitter: Random value (0 to backoff/2) added to prevent synchronized retries

---

## Monitoring & Resilience Patterns

### Exponential Backoff with Jitter

> **Note (plain-language):**
> Retrying immediately after a failure can make outages worse.
> If 100 clients retry at the same instant, the provider gets overwhelmed again.

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

---

### Circuit Breaker

> **Note (plain-language):**
> Fast failure is better than slow failure.
> If a provider is clearly down, stop wasting time retrying it.

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

**Why 3 failures / 30 seconds:**
- 3 failures provides confidence the provider is truly down (not a single transient error)
- 30 seconds allows time for provider-side recovery without excessive wait
- These values are production-tested defaults but could be made configurable for specific environments

---

### Graceful Degradation

> **Note (plain-language):**
> The system continues operating with reduced capacity when some providers fail.

**Purpose:** Partial failures don't block healthy providers, system remains operational with reduced capacity.

**Implementation:**
- **Concurrent requests:** All providers are queried in parallel using `errgroup`
- **Independent failure handling:** Each provider's failure is isolated and recorded
- **Best-effort results:** Even if some providers fail, successful responses are collected and reported
- **Consistency checks:** Work with whatever data is available (minimum 2 providers for meaningful comparison)

> **Operational scenario:**
> Four providers configured: Alchemy, Infura, LlamaNodes, PublicNode
> - Alchemy: Success (150ms)
> - Infura: Success (180ms)
> - LlamaNodes: Timeout (circuit breaker opens)
> - PublicNode: Rate limited (429)
>
> Result: Report shows Alchemy and Infura data with warnings about failures.
> Operations continue with 50% capacity instead of failing completely.

**Institutional value:**
In production, partial outages are more common than total failures. A monitoring system that fails completely when any provider is down provides no visibility during the exact time you need it most. Graceful degradation ensures you always have visibility into the providers that *are* working.

---

### Error Categorization

All errors are classified into actionable categories:

| Error Type | Meaning | Retry Strategy | Operational Response |
|------------|---------|----------------|---------------------|
| **Timeout** | Request exceeded deadline | Retry with backoff | Check network connectivity, provider health |
| **Rate Limit** (429) | Exceeded API quota | Retry with backoff | Upgrade provider tier or reduce request rate |
| **Server Error** (5xx) | Provider infrastructure issue | Retry with backoff | Provider-side problem, failover recommended |
| **Parse Error** | Invalid JSON or unexpected format | No retry | Investigate provider API changes |
| **RPC Error** | Application-level error (invalid params) | No retry | Fix request parameters |
| **Circuit Open** | Circuit breaker active | No retry | Wait for cooldown or failover |

**Why categorization matters:**
Different error types require different operational responses. Timeouts may indicate network issues. Rate limits require throttling. Server errors suggest provider problems. Parse errors indicate API contract changes.

The categorized error counts in snapshot reports help diagnose systemic issues (e.g., 90% timeouts = network problem, 90% rate limits = quota exhaustion).

---

## Provider Economics & Expectations

Understanding provider tiers helps set realistic expectations and make informed infrastructure decisions.

### Public RPC Endpoints

**Characteristics:**
* Success rate: ~90–98%
* P95 latency: 300–800ms
* Rate limited (typically 10-100 req/s)
* No SLA or support
* Free

**Examples:** Alchemy free tier, Infura free tier, LlamaNodes, PublicNode

> **Use case:** Development, testing, emergency fallback

**Operational reality:**
Public endpoints are adequate for development but unsuitable for production custody or trading systems. Expect occasional downtime, rate limiting during network congestion, and no recourse when issues occur.

---

### Enterprise Providers

**Characteristics:**
* Success rate: ≥99.9%
* P95 latency: 50–150ms
* Dedicated capacity (thousands of req/s)
* SLA-backed uptime guarantees
* 24/7 support and incident response
* Archive node access for historical queries
* Cost: $500-$5,000+/month depending on volume

**Examples:** Alchemy Growth/Scale plans, Infura Enterprise, Blockdaemon, Figment

> **Use case:** Production systems handling real user funds, custody platforms, institutional trading

**Operational reality:**
Enterprise providers deliver consistent performance but at significant cost. The SLA provides recourse (credits, escalation) but doesn't prevent impact. Multiple enterprise providers in active-active configuration is common for critical systems.

---

### Self-Hosted Nodes

**Characteristics:**
* Success rate: 99–100% (when properly maintained)
* Latency: <10ms (local network), 50-200ms (WAN)
* No rate limits
* Full control over configuration, peering, and upgrades
* No third-party data exposure
* Cost: Infrastructure + operational overhead ($2,000-$10,000+/month all-in)

**Infrastructure:** Geth/Erigon execution client + Prysm/Lighthouse consensus client

> **Use case:** Sovereignty requirements, MEV operations, high-frequency trading, regulatory compliance

**Operational reality:**
Self-hosting eliminates third-party risk but introduces operational complexity: hardware failures, consensus client bugs, peer connectivity issues, storage growth, upgrade coordination. Requires dedicated devops expertise.

---

> **Decision-maker summary:**
>
> * **Public endpoints** optimize for cost (free) at expense of reliability
> * **Enterprise endpoints** optimize for reliability at significant cost
> * **Self-hosting** optimizes for control at expense of operational complexity
> * **Monitoring** turns hidden risk into visible tradeoffs, enabling informed decisions

**Common production patterns:**

**Pattern 1: Multi-provider redundancy (most common)**
```
Primary: Alchemy Enterprise (SLA-backed, low latency)
Secondary: Infura Enterprise (geographic diversity)
Tertiary: Self-hosted Geth (sovereignty, no rate limits)
Emergency: Public Alchemy (zero cost, rate limited)
```

**Pattern 2: Geographic distribution**
```
US East: Alchemy endpoint in us-east-1
EU West: Infura endpoint in eu-west-1
Asia Pacific: Blockdaemon endpoint in ap-southeast-1
```

**Pattern 3: Workload segregation**
```
Read-heavy queries (balances, block data): Managed providers (Alchemy/Infura)
Transaction broadcast: Self-hosted nodes (guaranteed delivery, no censorship risk)
Archive queries (historical state): Enterprise with archive access
```

---

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

> **Operational scenario:**
> A custody platform receives a deposit transaction in block N.
> Before crediting the user's account, the platform queries multiple providers to confirm the transaction.
> If Provider A reports block N but Provider B is still at block N-10, using Provider B could lead to:
> - Delayed transaction confirmation (user complains about "missing" deposit)
> - Incorrect balance calculations (reconciliation failures)
> - Compliance violations (audit trail inconsistencies)
>
> The monitoring tool's height variance detection catches this scenario before it impacts users.

---

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

> **Critical scenario:**
> A trading desk executes a large ETH sale, broadcasting the transaction to Provider A.
> The transaction is included in block N on Provider A.
> The desk's settlement system, using Provider B for confirmation, doesn't see the transaction in block N (due to hash mismatch from a reorg).
> The desk's system incorrectly assumes the transaction failed and resubmits, resulting in a double-sell and significant capital loss.

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

---

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
- **Decimal handling:** Converting raw token amounts to decimal notation with proper formatting

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
5. **Return value decoding:**
   - RPC returns: `0x0000000000000000000000000000000000000000000000000000000005f5e100`
   - Hex: `0x05f5e100` = 100000000 (decimal)
   - With 6 decimals: 100.000000 USDC

**Educational value:**
This implementation serves as a complete reference for Ethereum ABI encoding specification, Keccak-256 hashing, big integer handling, and ERC-20 standard interface implementation.

---

## Architecture

```
eth-rpc-monitor/
├── cmd/
│   └── monitor/
│       ├── main.go              # Minimal CLI entry point, Cobra setup
│       ├── snapshot.go           # Snapshot command and execution logic
│       ├── watch.go              # Watch command and live monitoring logic
│       ├── blocks.go             # Blocks command, execution, and block arg parsing
│       ├── txs.go                # Transactions command and execution logic
│       ├── call.go               # Contract call command and execution logic
│       ├── status.go             # Status command and health check logic
│       └── helpers.go            # Shared utilities (config loading, client building, provider selection, consistency checks)
│
├── internal/
│   ├── config/
│   │   └── config.go            # YAML configuration parsing and validation
│   │
│   ├── rpc/
│   │   ├── client.go            # JSON-RPC client with production resilience
│   │   │                        # - Exponential backoff with jitter
│   │   │                        # - Circuit breaker (3 failures → 30s cooldown)
│   │   │                        # - Automatic retry with configurable attempts
│   │   │                        # - Error categorization
│   │   │
│   │   ├── methods.go          # Ethereum RPC method implementations
│   │   │                        # - eth_getBlockByNumber
│   │   │                        # - eth_blockNumber
│   │   │                        # - eth_call
│   │   │
│   │   ├── abi.go              # ABI encoding and decoding utilities
│   │   │                        # - Keccak-256 function selector computation
│   │   │                        # - Address padding and encoding
│   │   │                        # - uint256 decoding and formatting
│   │   │
│   │   └── abi_test.go          # ABI encoding tests
│   │
│   ├── metrics/
│   │   ├── collector.go        # Metrics aggregation and statistical analysis
│   │   │                        # - Latency percentile calculation
│   │   │                        # - Success rate computation
│   │   │                        # - Error categorization
│   │   │
│   │   ├── consistency.go      # Cross-provider consistency verification
│   │   │                        # - Block height variance detection
│   │   │                        # - Two-phase hash consensus
│   │   │                        # - Hash group identification
│   │   │
│   │   └── consistency_test.go # Consistency checker tests
│   │
│   ├── provider/
│   │   └── selector.go         # Smart provider selection and health scoring
│   │                            # - Quick health check
│   │                            # - Composite score calculation
│   │                            # - Exclusion logic
│   │
│   └── output/
│       ├── terminal.go         # Terminal rendering with ANSI colors
│       ├── json.go             # JSON output formatting
│       ├── blocks.go           # Block and transaction rendering
│       ├── call.go              # Contract call result rendering
│       ├── status.go           # Provider status/ranking rendering
│       └── watch.go            # Watch mode live display
│
└── config/
    └── providers.yaml           # Default provider configuration
```

**Project structure rationale:**

**Why `internal/` package:**
Go's `internal/` directory enforces package-private visibility, preventing external projects from importing these packages. This ensures the codebase remains focused on the CLI tool's needs without maintaining a stable public API.

**Separation of concerns:**
- **`rpc/`**: Protocol-level communication (JSON-RPC, HTTP, retries)
- **`metrics/`**: Business logic (statistical analysis, consistency checking)
- **`output/`**: Presentation layer (terminal formatting, JSON serialization)
- **`config/`**: Configuration management (YAML parsing, validation)
- **`provider/`**: Higher-level provider management (health checks, ranking)

This separation makes the codebase easier to test, extend, and maintain.

---

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

---

## Development

### Prerequisites

- **Go 1.21+** (tested with Go 1.24)
- Internet connectivity for RPC endpoint access
- (Optional) Local Ethereum node for self-hosted provider testing

### Build

```bash
# Standard build
go build -o monitor ./cmd/monitor

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
```

### Dependencies

| Package | Purpose | Version |
|---------|---------|---------|
| [github.com/spf13/cobra](https://github.com/spf13/cobra) | CLI framework with subcommands, flags, help generation | v1.8.0 |
| [github.com/fatih/color](https://github.com/fatih/color) | ANSI terminal colors for status indicators | v1.16.0 |
| [github.com/rodaine/table](https://github.com/rodaine/table) | ASCII table formatting for tabular output | v1.2.0 |
| [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) | YAML parsing for configuration files | v3.0.1 |
| [golang.org/x/crypto](https://golang.org/x/crypto) | Keccak-256 hashing for ABI function selectors | v0.47.0 |
| [golang.org/x/sync/errgroup](https://golang.org/x/sync) | Concurrent goroutine management with error handling | v0.19.0 |

**No external RPC libraries:**
Intentionally does not use libraries like `go-ethereum/ethclient` to demonstrate the raw JSON-RPC protocol from first principles. This makes the code more educational and avoids heavyweight dependencies.

---

## Roadmap

**Completed phases:**
- ✅ Phase 1: Snapshot mode with latency percentiles and consistency checking
- ✅ Phase 2: Watch mode with real-time monitoring and incident logging
- ✅ Phase 3: Block and transaction query commands
- ✅ Phase 4: Smart contract call support (ERC-20 balance queries)
- ✅ Phase 5: Smart provider selection with health-based ranking

**Future enhancements:**
- **Additional RPC methods:** eth_getTransactionReceipt, eth_estimateGas, eth_getLogs
- **WebSocket support:** Subscription-based monitoring (newHeads, logs, pendingTransactions)
- **Prometheus metrics export:** Native integration with Prometheus for long-term monitoring
- **Alert webhook support:** POST to external URLs when thresholds are breached
- **Historical database:** Store monitoring data for trend analysis
- **Multi-chain support:** Extend to Polygon, Arbitrum, Optimism, Base

---

## Final Takeaway

Ethereum itself is resilient.
**RPC infrastructure is not.**

This tool helps ensure that when your system asks:

> "Has this transaction been included?"
> "Is this balance correct?"
> "Is this block final?"

You can say:

> **"We verified it across providers, at the block and transaction level."**

That verification — the ability to detect stale data, inconsistent hashes, and lagging providers **before** they cause user impact — is the difference between infrastructure you trust and infrastructure you hope works.

---

## License

MIT License - see LICENSE file for details.

---

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

**Last updated:** January 15, 2025
