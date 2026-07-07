# Architecture (overview)

A single CLI (`cmd/ethrpc`, built on cobra) exposes four subcommands that share YAML config and the `internal/` libraries. Command workflows live in `internal/cli`; the cobra layer is a thin flag-wiring shell. Operational detail lives in [`AGENTS.md`](../AGENTS.md).

```mermaid
flowchart LR
  subgraph cmd [cmd/ethrpc]
    ROOT[ethrpc root<br/>--config, LoadEnv+Load]
    B[block]
    T[test]
    S[snapshot]
    M[monitor]
  end
  subgraph internal [internal]
    CLI[cli<br/>Run* workflows]
    CFG[config]
    RPC[rpc]
    FMT[format]
    RJ[reportjson]
  end
  EP[Ethereum JSON-RPC HTTPS]
  ROOT --> B
  ROOT --> T
  ROOT --> S
  ROOT --> M
  B --> CLI
  T --> CLI
  S --> CLI
  M --> CLI
  CLI --> CFG
  CLI --> RPC
  CLI --> FMT
  CLI --> RJ
  RPC --> EP
```
