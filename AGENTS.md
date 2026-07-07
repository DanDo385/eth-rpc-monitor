# Agent instructions — Ethereum RPC Monitor

Use this file as the **single source of truth** for how to work in this repository. Human-oriented setup and command walkthroughs live in [`README.md`](README.md).

---

## 1. What this project is

Portfolio-grade **Go CLI** for Ethereum **JSON-RPC over raw HTTP**: measure **latency**, **tail stats**, and **cross-provider agreement**. It demonstrates operational thinking (RPC quality matters for anything time-sensitive on-chain).

**In scope:** a single `ethrpc` CLI with `block`, `test`, `snapshot`, `monitor` subcommands (cobra); YAML config; colored terminal output; optional JSON reports.

**Out of scope:** `go-ethereum` / `ethclient`, trading or signing, web UI, durable storage, retries and heavy defensive validation. Keep the design intentionally small and observable.

---

## 2. Layout

```
cmd/
  ethrpc/            Thin cobra layer — one file per subcommand wiring flags to cli.Run*
    main.go          func main → Execute()
    root.go          rootCmd, persistent --config, PersistentPreRunE → LoadEnv() + Load()
    block.go         blockCmd  → cli.RunBlock
    test.go          testCmd   → cli.RunTest
    snapshot.go      snapshotCmd → cli.RunSnapshot
    monitor.go       monitorCmd → cli.RunMonitor

internal/
  cli/               Command workflows (RunBlock/RunTest/RunSnapshot/RunMonitor) + types, importable + tested
  rpc/               HTTP JSON-RPC client, wire types, hex/format helpers
  config/            YAML load, ${VAR} expansion, optional .env via LoadEnv()
  format/            Terminal tables, colors, percentiles, monitor UI
  reportjson/        Timestamped JSON report writer under reports/

config/
  providers.yaml.example   Template — copy to providers.yaml (gitignored for secrets)
```

---

## 3. Commands — behavior agents must not get wrong

All subcommands hang off the single `ethrpc` binary. The cobra layer in `cmd/ethrpc/` only wires flags; the actual workflows are `cli.Run*` in `internal/cli/`.

| Subcommand | Role | Notable flags |
|--------|------|----------------|
| `ethrpc block [block]` | Latest or specific block on one provider | `--config`, `--provider`, `--json`/`-j` |
| `ethrpc test` | Concurrent per-provider samples, warm-up, P50/P95/P99/Max | `--config`, `--samples`/`-s`, `--json`/`-j` |
| `ethrpc snapshot [block]` | Concurrent `GetBlock` for same tag on everyone | `--config` only |
| `ethrpc monitor` | Ticker loop, new RPC client each refresh | `--config`, `--interval`/`-i` (`0` = use YAML `watch_interval`) |

`--config` is a **persistent** flag on the root command (also `-c`); it triggers `config.LoadEnv()` + `config.Load()` once via `PersistentPreRunE` before any subcommand runs.

**JSON export:** Implemented for **`block`** and **`test`** only (`--json` → under `reports/`, timestamped). Do **not** assume `snapshot` or `monitor` support `--json` unless the code has been added.

**Flags:** cobra (`github.com/spf13/cobra`) — long flags use `--flag`, shorthands use `-x` (aliases noted above). Default config path: `config/providers.yaml`.

**`snapshot` block argument:** Prefer `latest` or hex; decimal tags are not normalized here the way `block` does. Point users at `ethrpc block` for flexible decimal input.

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

**JSON mode (`block` / `test`):** Write pretty-printed JSON under `reports/`; **errors and progress to stderr**; do not mix diagnostic noise into report payloads. Filenames use timestamp pattern (e.g. `block-YYYYMMDD-HHMMSS.json`, `test` may use `health-` prefix — follow the `reportjson.Write` helper).

---

## 9. Code quality (non-negotiables)

- Package comments on **all** packages; doc comments on **exported** symbols (purpose, params, returns); inline comments where the algorithm is non-obvious.
- Wrap errors: `fmt.Errorf("context: %w", err)`; the cobra layer lets cobra print user-facing errors (`SilenceUsage: true`) — do not double-print from `RunE`.
- **CLI framework:** `github.com/spf13/cobra`. Command workflows MUST live in `internal/cli` (importable, testable, and covered by the CI 40% floor on `./internal/...`); `cmd/ethrpc` stays a thin flag-wiring shell. Do not push workflow logic back into `package main`.
- **Do not:** `go-ethereum`, web UI, DB persistence, hardcoded provider defaults in Go, comparing hashes at **different** block heights, retry storms, or stdout/stderr mixing in JSON mode.

---

## 10. After you change code

```bash
make build
./bin/ethrpc block latest
./bin/ethrpc test --samples 5
./bin/ethrpc snapshot latest
./bin/ethrpc monitor --interval 5s   # Ctrl+C to exit
```

If you touched JSON paths: `./bin/ethrpc block latest --json` and `./bin/ethrpc test --json` and confirm files under `reports/`.

---

## 11. Where to look

| Task | Start here |
|------|----------------|
| HTTP + latency | `internal/rpc/client.go` |
| Wire / block types | `internal/rpc/types.go` |
| Hex / units / time | `internal/rpc/format.go` |
| YAML + env | `internal/config/config.go` |
| Command workflows (Run*) | `internal/cli/block.go`, `test.go`, `snapshot.go`, `monitor.go` |
| Cobra flags / root / config load | `cmd/ethrpc/root.go` + per-subcommand files |
| Provider race / warm-up | `internal/cli/block.go`, `test.go`, `snapshot.go` |
| Monitor loop / tick | `internal/cli/monitor.go` |

When in doubt, prefer **small, focused diffs** and behavior that matches this document and **`README.md`**.
