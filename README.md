# Ethereum RPC Monitor

[![CI](https://github.com/dando385/eth-rpc-monitor/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/dando385/eth-rpc-monitor/actions/workflows/ci.yml)

Small **Go** CLIs for Ethereum **JSON-RPC over HTTP**: measure **tail latency** (P50 / P95 / P99 / Max), **reliability**, and **cross-provider agreement** on the same block—using one YAML config and no heavy SDK so behavior stays visible.

**What you get**

- **`block`** — One block from the best auto-selected provider (highest head, lowest latency among ties) or a pinned provider.
- **`test`** — Many samples per provider, colored table, height-drift warning; optional JSON report.
- **`snapshot`** — Same block tag from everyone; height and hash mismatch detection.
- **`monitor`** — Live terminal dashboard with intentional “cold” client per tick for realistic poll cost.

**Design stance:** no app-level response cache, **no automatic retries** (failures are signal), raw `net/http` + `encoding/json`. Contributor and agent rules live in **[`AGENTS.md`](AGENTS.md)**. Module layout diagram: **[`docs/architecture.md`](docs/architecture.md)**.

---

## Table of contents

1. [Quick start](#1-quick-start)  
2. [Why this matters](#2-why-this-matters)  
3. [Why this is now affordable](#3-why-this-is-now-affordable)  
4. [Prerequisites](#4-prerequisites)  
5. [Configure providers](#5-configure-providers)  
6. [Build](#6-build)  
7. [Commands](#7-commands)  
8. [JSON reports](#8-json-reports-block-and-test-only)  
9. [Caching and connection behavior](#9-caching-and-connection-behavior)  
10. [Development and testing](#10-development-and-testing)  
11. [Quick sanity checklist](#11-quick-sanity-checklist)  
12. [Troubleshooting](#12-troubleshooting)  
13. [Project layout](#13-project-layout)  
14. [License](#14-license)

---

## 1. Quick start

```bash
git clone https://github.com/dando385/eth-rpc-monitor.git
cd eth-rpc-monitor
cp config/providers.yaml.example config/providers.yaml
# Edit config/providers.yaml; optional: cp .env.example .env and set keys
make build
./bin/test --samples 3
```

Run from the **repo root** (or pass `--config` with an absolute path) so the default `config/providers.yaml` resolves.

---

## 2. Why this matters

Public-chain settlement collapses several layers a traditional firm runs internally. Both counterparties read the same canonical block, so confirmation **is** the matching engine: there is no internal trade-match queue and no nightly reconciliation gap to defend. Risk goes live as of the next block (~12s on Ethereum L1) instead of T+1 / T+2 — the position the desk *thinks* it has and the position the chain *says* it has are the same record.

That single ledger feeds every downstream consumer: front office marks-to-market, middle office trade capture, risk and margin systems, treasury and collateral, compliance and audit. One source of truth across the firm lowers operational risk in places that are usually invisible until they break.

The market also doesn't sleep. State can be queried at any time, which is exactly what `./bin/snapshot latest` and `./bin/monitor` are for: an honest answer to "what does the chain look like *right now*?" with no business-hours dependency.

---

## 3. Why this is now affordable

Cost is no longer the gating objection.

- **L1 reads** — what every binary in this repo does — are free at the protocol level; you pay only for the HTTP request itself.
- **L1 writes** in normal conditions are typically a few gwei in priority fee: pennies for a transfer, low single-digit dollars for a complex contract interaction.
- **L2 writes** (Base, Arbitrum, Optimism, zkSync, Linea, Scroll, …) routinely cost a fraction of a cent at much higher throughput.
- A **lower ETH spot price** is actually a tailwind for institutional adoption: gas is denominated in ETH, so a softer ETH price means lower fiat cost per transaction for any firm paying in fiat — bearish for headlines, bullish for usage.

> **Historical anchor.** Genesis (block 0) was mined **2015-07-30 15:26:13 UTC** with **8,893** pre-sale allocation transactions. Block **46147** carried the **first user-initiated transaction** on **2015-08-07 03:30:33 UTC** ([tx `0x5c504e…b22060` on Etherscan](https://etherscan.io/tx/0x5c504ed432cb51138bcf09aa5e8a410dd4a1e204ef84bfed1be16dfba1b22060)). Roughly a decade of continuous block production sits behind every `./bin/snapshot latest`.

---

## 4. Prerequisites

- **Go 1.24+** ([install](https://go.dev/dl/))
- At least one **Ethereum mainnet HTTP(S) RPC** URL (public endpoints work; paid keys optional)

**RPC methods used:** `eth_blockNumber`, `eth_getBlockByNumber` (full tx objects are not fetched; hashes only).

---

## 5. Configure providers

All commands read **`config/providers.yaml`** by default.

1. **Copy the example** (real `providers.yaml` is gitignored so secrets stay local):

   ```bash
   cp config/providers.yaml.example config/providers.yaml
   ```

2. **Edit `config/providers.yaml`**
   - **`defaults`:** `timeout`, `health_samples` (for `test`), `watch_interval` (for `monitor`).
   - **`providers`:** each entry needs `name`, `url`, and optional `type` (display only; does not change RPC behavior).

3. **`${VAR}` in URLs** — expanded with `os.ExpandEnv()` when the file is loaded.

4. **Secrets** — either `export` variables before running or add a **`.env`** in the project root. Every binary calls `config.LoadEnv()` on startup. See **`.env.example`** for common variable names.

5. **Optional** — copy `.env.example` to `.env` and fill values; never commit `.env`.

---

## 6. Build

**Makefile (recommended):**

```bash
make build        # produces bin/block, bin/test, bin/snapshot, bin/monitor
make test         # go test ./... -race
make vet          # go vet ./...
```

**Manual build:**

```bash
mkdir -p bin
go build -o bin/block ./cmd/block
go build -o bin/test ./cmd/test
go build -o bin/snapshot ./cmd/snapshot
go build -o bin/monitor ./cmd/monitor
```

**Tech stack:** Go 1.24+, `golang.org/x/sync/errgroup`, `gopkg.in/yaml.v3`, `github.com/fatih/color` for terminal output.

---

## 7. Commands

Global flag (where supported): **`--config <path>`** — defaults to `config/providers.yaml`. Standard `flag` package: **`-flag`** and **`--flag`** both work where applicable.

### `block` — Inspect one block

Picks a provider (fastest among those on the highest seen head, unless you pin one), then fetches and prints block details.

```bash
./bin/block                    # same as ./bin/block latest
./bin/block latest
./bin/block pending
./bin/block earliest
./bin/block 19000000           # decimal height
./bin/block 0x121eac0          # hex height
./bin/block latest --provider alchemy
./bin/block latest --json      # reports/block-YYYYMMDD-HHMMSS.json
```

**Flags:** `--config`, `--provider <name>`, `--json`

---

### `test` — Latency and success over many samples

Hits every provider repeatedly, prints a summary table (**P50 / P95 / P99 / Max**), and warns on height drift. A **warm-up** request runs first (not counted in stats).

```bash
./bin/test                     # sample count from config (health_samples)
./bin/test --samples 10
./bin/test --json              # reports/health-YYYYMMDD-HHMMSS.json
```

**Flags:** `--config`, `--samples <n>`, `--json`

---

### `snapshot` — Same block from everyone; compare hash / height

Fetches one block from **all** providers concurrently and groups results to spot lag or disagreement.

```bash
./bin/snapshot                 # latest
./bin/snapshot latest
./bin/snapshot 0x121eac0       # hex block tag
```

**Note:** Prefer **`latest`** or **hex** here; decimal tags are not normalized the way they are in **`block`**. Use **`block`** for flexible decimal/hex on a single provider.

**Flags:** `--config` only (no `-json` in this tool).

---

### `monitor` — Live dashboard

Clears/redraws the terminal on an interval; shows height, latency, and lag vs best head. **Ctrl+C** exits.

```bash
./bin/monitor                  # interval from config (watch_interval)
./bin/monitor --interval 10s   # override refresh
```

**Flags:** `--config`, `--interval <duration>` — use **`0`** to use the YAML `watch_interval` default.

---

## 8. JSON reports (`block` and `test` only)

With **`-json`**, reports are written under **`reports/`** (created if needed), timestamped, and pretty-printed. Diagnostics stay on **stderr** so scripts can rely on stdout/file behavior.

```bash
./bin/block latest -json
./bin/test -json
```

---

## 9. Caching and connection behavior

There is **no application-level cache** of RPC responses across commands. Anything that looks like “caching” is one of the following.

### HTTP connection reuse (`net/http`)

Each `rpc.Client` wraps `http.Client`; the default **`Transport`** pools connections for the **lifetime of that client**. First request pays DNS/TCP/TLS; later calls on the same client often reuse the connection.

### Warm-up (measurement, not a data cache)

**`block`**, **`test`**, and **`snapshot`** each issue at least one discarded **`eth_blockNumber`** (or equivalent) before measured work so numbers reflect **steady-state** RPC more than one-off handshake cost. Warm-up is **not** included in `test` percentiles or JSON sample arrays.

### Where the client is reused

- **`test`:** one client per provider for warm-up + all samples.
- **`snapshot`:** one client per provider; warm-up then measured `GetBlock` share the pool.
- **`block` (auto-select):** all providers are raced with separate clients; the **winner’s** client is reused for warm-up + `GetBlock`.

### Where reuse is intentionally avoided

**`monitor`** builds a **new** `rpc.Client` every refresh so each tick reflects a **colder**, more end-to-end poll cost.

### Provider-side behavior

Vendors may cache or serve slightly stale data. **`snapshot`** can show skew that reflects infrastructure, not necessarily a bug in this repo. There is no cache-busting.

### Self-hosted nodes and institutional SLAs

A self-hosted node (Geth, Nethermind, Reth, …) typically removes one network hop and the shared-rate-limit risk that comes with public endpoints; the realistic latency floor lives there, often single-digit ms. The example YAML at [`config/providers.yaml.example`](config/providers.yaml.example) ships a commented **`local-geth`** entry pointed at `http://localhost:8545` precisely so you can drop in your own node and compare it side-by-side against vendor URLs in the same `./bin/test` table.

Institutional users wrap managed providers (Alchemy, Infura, Quicknode, Blockdaemon, Coinbase Cloud, Chainstack, …) in real SLAs covering availability (99.9 %+), P99 latency floors, archive-node depth, dedicated capacity, and incident response. The hub-and-spoke pattern — own node for hot path, multiple SLA-backed providers for redundancy and cross-checking — is what this CLI is designed to **observe**.

---

## 10. Development and testing

- **Agents / conventions:** [`AGENTS.md`](AGENTS.md)  
- **CI:** [`.github/workflows/ci.yml`](.github/workflows/ci.yml) — `gofmt` check, `go vet`, **`go test ./... -race`**, and a **coverage floor (40%)** on **`./internal/...`** so gates stay meaningful without requiring full coverage of large `cmd` entrypoints.

```bash
make test
make vet
make build
```

---

## 11. Quick sanity checklist

| Step | Check |
|------|--------|
| Config exists | `ls config/providers.yaml` |
| Keys expanded | URLs should not contain literal `${...}` if the var is set |
| Build | `make build` completes |
| First run | `./bin/test --samples 3` |

---

## 12. Troubleshooting

| Symptom | What to try |
|---------|--------------|
| `failed to read config: no such file` | Copy the example YAML; or `./bin/block --config /absolute/path/to/providers.yaml` |
| Empty or broken URLs after expansion | Set env vars or `.env`; names must match `${VAR}` in YAML |
| `provider 'x' not found` | `--provider` must match `name:` in YAML exactly |
| Very slow first request | Normal; warm-up in `test` / `snapshot` reduces measurement bias |
| HTTP / JSON-RPC errors from `block` | Non-200 responses and malformed JSON now surface as errors from the client (check endpoint URL and auth) |

---

## 13. Project layout

| Path | Role |
|------|------|
| `cmd/block`, `cmd/test`, `cmd/snapshot`, `cmd/monitor` | CLI entrypoints |
| `internal/rpc` | HTTP JSON-RPC client, wire types, hex/format helpers |
| `internal/config` | YAML load + `${VAR}` expansion + optional `.env` |
| `internal/format` | Tables, colors, percentiles, monitor UI |
| `internal/reportjson` | Timestamped JSON reports for `block` / `test` `-json` |
| `docs/architecture.md` | High-level module diagram |
| `config/providers.yaml.example` | Template for `providers.yaml` |

---

## 14. License

MIT.
