# Ethereum RPC Monitor

Go CLI tools to measure Ethereum JSON-RPC **latency**, **health**, and **cross-provider agreement**. Four binaries share one YAML config.

---

## 1. Prerequisites

- **Go 1.24+** ([install](https://go.dev/dl/))
- At least one **Ethereum mainnet HTTP(S) RPC URL** (public endpoints work; paid keys optional)

---

## 2. Get the code

```bash
git clone https://github.com/dando385/eth-rpc-monitor.git
cd eth-rpc-monitor
```

---

## 3. Configure providers

All commands read **`config/providers.yaml`** by default.

1. **Copy the example file** (the real `providers.yaml` is gitignored so secrets stay local):

   ```bash
   cp config/providers.yaml.example config/providers.yaml
   ```

2. **Edit `config/providers.yaml`**:
   - Set **`defaults`**: `timeout`, `health_samples` (for `test`), `watch_interval` (for `monitor`).
   - Under **`providers`**, list each endpoint with `name`, `url`, and optional `type` (informational only).

3. **URLs and secrets**  
   Use `${VAR}` in URLs; values are filled with `os.ExpandEnv()` after load.

4. **Optional `.env` in the project root**  
   Each command calls `config.LoadEnv()` so a `.env` file can define keys without exporting them in your shell:

   ```bash
   # .env (do not commit)
   ALCHEMY_API_KEY=your_key
   INFURA_API_KEY=your_key
   ```

   You can instead `export` those variables before running—either works.

5. **Run from the repo root** (or pass `--config` with an absolute path) so the default `config/providers.yaml` resolves correctly.

---

## 4. Build

**Makefile (recommended):**

```bash
make build
```

This creates **`bin/block`**, **`bin/test`**, **`bin/snapshot`**, and **`bin/monitor`**.

**Manual:**

```bash
mkdir -p bin
go build -o bin/block ./cmd/block
go build -o bin/test ./cmd/test
go build -o bin/snapshot ./cmd/snapshot
go build -o bin/monitor ./cmd/monitor
```

**Run a command** (from repo root):

```bash
./bin/block latest
./bin/test --samples 5
```

---

## 5. Commands (walkthrough)

Global flag (where supported): **`--config <path>`** — defaults to `config/providers.yaml`.

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
./bin/block latest --json      # writes reports/block-YYYYMMDD-HHMMSS.json
```

**Flags:** `--config`, `--provider <name>`, `--json`

---

### `test` — Latency and success over many samples

Hits every provider repeatedly, prints a summary table (P50 / P95 / P99 / Max), and warns on height drift. Does a **warm-up** request first (not counted in stats).

```bash
./bin/test                     # sample count from config (health_samples)
./bin/test --samples 10
./bin/test --json              # writes reports/health-YYYYMMDD-HHMMSS.json
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

**Note:** Prefer **`latest`** or **hex** tags here; decimal heights may not match what your nodes expect for `eth_getBlockByNumber`. Use **`block`** for flexible decimal/hex handling on a single provider.

**Flags:** `--config` only

---

### `monitor` — Live dashboard

Clears/redraws the terminal on an interval; shows height, latency, and lag vs best head. **Ctrl+C** exits.

```bash
./bin/monitor                  # interval from config (watch_interval)
./bin/monitor --interval 10s   # override refresh
```

**Flags:** `--config`, `--interval <duration>` (use `0` or rely on default in config when `0` means “use YAML”—the binary uses `0` to mean “use config default” for `--interval`)

---

## 6. JSON reports (`block` and `test` only)

With **`-json`**, reports are written under **`reports/`** (created if needed), timestamped, pretty-printed. Errors still go to **stderr**; normal logging stays out of the JSON file path as designed for scripting.

```bash
./bin/block latest -json
./bin/test -json
```

---

## 7. Caching and connection behavior

This project does **not** keep an application-level cache of RPC responses (no memoized blocks, heights, or JSON in memory across commands). Anything that looks like “caching” falls into a few other buckets:

### HTTP connection reuse (Go `net/http`)

Each provider uses an `rpc.Client` wrapping the standard library **`http.Client`**. The default **`http.Transport`** keeps a small **connection pool** (TCP + TLS session reuse) for the lifetime of that client. The first request on a new client pays DNS, TCP, and TLS setup; later requests on the **same** client often reuse the open connection, so latency drops.

### Warm-up requests (measurement technique, not data cache)

**`block`**, **`test`**, and **`snapshot`** each issue at least one **discarded** `eth_blockNumber` (or equivalent) call before the timings you care about. That **warms** the connection so measured latencies reflect **steady-state** RPC work instead of one-off handshake cost. Those warm-up results are **not** written into percentile stats or JSON sample arrays.

### Where one client is reused

- **`test`**: One `rpc.NewClient` per provider for that provider’s entire run (warm-up + all samples), so samples share the same underlying connection pool.
- **`snapshot`**: One client per provider; warm-up `BlockNumber` then measured `GetBlock` share the pool.
- **`block` (auto-select)**: Phase 1 races all providers with separate clients; the **winning** provider’s client is **kept** and reused for warm-up + `GetBlock`, so the connection opened during selection is reused for the fetch.

### Where reuse is intentionally avoided

**`monitor`** creates a **new** `rpc.Client` on **every** refresh cycle. That is deliberate: each tick’s latency includes a more **end-to-end** picture (often including connection setup again), which matches “how expensive is a cold-ish poll right now?” rather than only warmed pooled calls.

### Provider-side caching (outside this repo)

RPC vendors and HTTP stacks may cache responses or serve slightly **stale** data. That can show up in **`snapshot`** as height/hash skew between providers even when your code is correct. The tool does not send cache-busting headers or control CDN behavior; treat odd consensus rows as a signal about the endpoint, not about the CLI.

---

## 8. Quick sanity checklist

| Step | Check |
|------|--------|
| Config exists | `ls config/providers.yaml` |
| Keys expanded | URLs should not contain literal `${...}` if the var is set |
| Build | `make build` completes |
| First run | `./bin/test --samples 3` |

---

## 9. Troubleshooting

| Symptom | What to try |
|--------|--------------|
| `failed to read config: no such file` | Copy the example YAML; or `./bin/block --config /absolute/path/to/providers.yaml` |
| Empty or broken URLs after expansion | Set env vars or `.env`; confirm names match `${VAR}` in YAML |
| `provider 'x' not found` | Name in `--provider` must match `name:` in YAML exactly |
| Very slow first request | Normal; `test` / `snapshot` warm-up hides part of that for measurements |

---

## 10. Project layout (short)

| Path | Role |
|------|------|
| `cmd/block`, `cmd/test`, `cmd/snapshot`, `cmd/monitor` | CLI entrypoints |
| `internal/rpc` | HTTP JSON-RPC client, types, hex/format helpers |
| `internal/config` | YAML load + env expansion |
| `internal/format` | Terminal output |
| `internal/reportjson` | Timestamped JSON reports (`block`, `test` `-json`) |
| `docs/architecture.md` | High-level module diagram |
| `config/providers.yaml.example` | Template for your `providers.yaml` |

---

## License

MIT.
