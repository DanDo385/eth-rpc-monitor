# Agent instructions — Ethereum RPC Monitor

Use this file as the **single source of truth** for how to work in this repository. Human-oriented setup and command walkthroughs live in [`README.md`](README.md).

---

## 1. What this project is

Portfolio-grade **Go CLI** for Ethereum **JSON-RPC over raw HTTP**: measure **latency**, **tail stats**, and **cross-provider agreement**. It demonstrates operational thinking (RPC quality matters for anything time-sensitive on-chain).

**In scope:** `block`, `test`, `snapshot`, `monitor` binaries; YAML config; colored terminal output; optional JSON reports.

**Out of scope:** `go-ethereum` / `ethclient`, trading or signing, web UI, durable storage, retries and heavy defensive validation. Keep the design intentionally small and observable.

---

## 2. Layout

```
cmd/
  block/main.go      Inspect one block; auto-select fastest provider on highest head (or --provider)
  test/main.go       Health / latency samples + percentiles
  snapshot/main.go   Same block from all providers; compare hash & height
  monitor/main.go    Live dashboard loop

internal/
  rpc/               HTTP JSON-RPC client, wire types, hex/format helpers
  config/            YAML load, ${VAR} expansion, optional .env via LoadEnv()
  format/            Terminal tables, colors, percentiles, monitor UI

config/
  providers.yaml.example   Template — copy to providers.yaml (gitignored for secrets)
```

---

## 3. Commands — behavior agents must not get wrong

| Binary | Role | Notable flags |
|--------|------|----------------|
| `block` | Latest or specific block on one provider | `--config`, `--provider`, `-json` |
| `test` | Concurrent per-provider samples, warm-up, P50/P95/P99/Max | `--config`, `--samples`, `-json` |
| `snapshot` | Concurrent `GetBlock` for same tag on everyone | `--config` only |
| `monitor` | Ticker loop, new RPC client each refresh | `--config`, `--interval` (`0` = use YAML `watch_interval`) |

**JSON export:** Implemented for **`block`** and **`test`** only (`-json` → under `reports/`, timestamped). Do **not** assume `snapshot` or `monitor` support `-json` unless the code has been added.

**Flags:** Standard library `flag` — both `-flag` and `--flag` work where applicable. Default config path: `config/providers.yaml`.

**`snapshot` block argument:** Prefer `latest` or hex; decimal tags are not normalized here the way `block` does. Point users at `block` for flexible decimal input.

---

## 4. Configuration rules

- **Single source of truth:** `config/providers.yaml` (from `providers.yaml.example`). Defaults (`timeout`, `health_samples`, `watch_interval`) belong in YAML — **do not invent silent fallbacks in Go** for missing config.
- **`${VAR}` in URLs** → `os.ExpandEnv()` at load time.
- **`.env`** in repo root is optional; every command calls `config.LoadEnv()` so keys can live there without shell exports.
- **`type` on providers** is informational only (display); it does not change RPC behavior.

---

## 5. RPC and HTTP design

- **Wire protocol:** JSON-RPC 2.0 over HTTP POST. Struct tags on wire types in `internal/rpc/types.go` are for JSON-RPC, not YAML.
- **Methods in use:** `eth_blockNumber`, `eth_getBlockByNumber` (and related block payload fields as defined in types).
- **No `go-ethereum`**, no external RPC SDK — `net/http` + `encoding/json` so latency and behavior stay visible.
- **No retry layer** — failures surface as reliability signal.
- **`rpc.Client`:** One instance per provider for a given workflow. Each wraps `http.Client`; the default **`http.Transport` pools connections** for the lifetime of that client. Project comments sometimes say “no client pooling” meaning **no long-lived global pool of `rpc.Client`s** — not “HTTP never reuses TCP.”
- **`monitor`:** Intentionally creates a **new** `rpc.Client` each refresh so each tick reflects a colder, more end-to-end poll cost.

---

## 6. Warm-ups, caching, and fairness

- **No app-level response cache** — do not add memoization of blocks or RPC results across commands unless explicitly requested.
- **`block`**, **`test`**, **`snapshot`:** At least one discarded **`eth_blockNumber`** (or equivalent) **before** measured work to prime DNS/TCP/TLS and connection pool; warm-up is **not** counted in `test` stats or JSON sample arrays.
- **`block` auto-select:** Phase 1 races providers; the **winning** client is **reused** for warm-up + `GetBlock`.
- **Provider-side HTTP/RPC caches** can cause **`snapshot`** skew; the tool does not cache-bust. Treat mismatches as possibly infrastructural, not automatically a logic bug in this repo.

---

## 7. Concurrency and algorithms

- Use **`golang.org/x/sync/errgroup`** with `errgroup.WithContext(ctx)` for concurrent provider work; capture range variables (`p := p`) in goroutines.
- **`block` selection:** Concurrent `eth_blockNumber` → highest height wins definition of “latest”; among providers on that height, pick **lowest latency**.
- **Percentiles (`test`):** Nearest-rank, e.g. `index := int(math.Ceil(float64(n)*p)) - 1` so small samples behave sensibly (P95/P99 can equal Max).
- **Hex:** Use `internal/rpc/format.go` — `ParseHexUint64`, `ParseHexBigInt` as appropriate.

---

## 8. Output contracts

**Terminal:** Colors via `github.com/fatih/color` — green for fast (under 100ms), yellow for 100–300ms, red above 300ms, bold headers, dim secondary.

**JSON mode (`block` / `test`):** Write pretty-printed JSON under `reports/`; **errors and progress to stderr**; do not mix diagnostic noise into report payloads. Filenames use timestamp pattern (e.g. `block-YYYYMMDD-HHMMSS.json`, `test` may use `health-` prefix — follow existing `writeJSON` helpers in each command).

---

## 9. Code quality (non-negotiables)

- Package comments on **all** packages; doc comments on **exported** symbols (purpose, params, returns); inline comments where the algorithm is non-obvious.
- Wrap errors: `fmt.Errorf("context: %w", err)`; `main` prints user-facing messages.
- **Do not:** `go-ethereum`, web UI, DB persistence, hardcoded provider defaults in Go, comparing hashes at **different** block heights, retry storms, CLI frameworks beyond `flag`, or stdout/stderr mixing in JSON mode.

---

## 10. After you change code

```bash
make build
./bin/block latest
./bin/test -samples 5
./bin/snapshot latest
./bin/monitor -interval 5s   # Ctrl+C to exit
```

If you touched JSON paths: `./bin/block latest -json` and `./bin/test -json` and confirm files under `reports/`.

---

## 11. Where to look

| Task | Start here |
|------|----------------|
| HTTP + latency | `internal/rpc/client.go` |
| Wire / block types | `internal/rpc/types.go` |
| Hex / units / time | `internal/rpc/format.go` |
| YAML + env | `internal/config/config.go` |
| Provider race / warm-up | `cmd/block/main.go`, `cmd/test/main.go`, `cmd/snapshot/main.go` |
| Monitor loop / tick | `cmd/monitor/main.go` |

When in doubt, prefer **small, focused diffs** and behavior that matches this document and **`README.md`**.
