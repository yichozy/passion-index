# passion-index

> Self-hosted alternative to the PageIndex cloud service. Receives PDF,
> orchestrates MinerU + MinerU-Popo + own post-processing, produces
> hopeai-rag-style structured tree JSON.

## Status

🚧 MVP in development — see the
[design spec](docs/superpowers/specs/2026-08-04-passion-index-design.md).

## Quick start

```bash
# 1. Install dependencies + download hopebox
make tidy

# 2. Configure environment
cp .env.example .env
# Edit .env to fill in real values (MinerU URL, PG credentials, etc.)

# 3. Run
make run
```

Server listens on `PASSION_INDEX_PORT` (default 8080).

## Architecture

See the [design spec](docs/superpowers/specs/2026-08-04-passion-index-design.md) §1.

```
┌────────────────────────────────────────────────────────────────┐
│                  passion-index (Go HTTP 服务)                  │
│                                                                │
│  gin + gqlgen        POST /query (GraphQL) + GET /images/* (REST)│
│  services/          pipeline_service    (编排 + worker pool)   │
│                     tree_service       (Popo→Doc + 页反查)     │
│                     imageresolve_service (图片补全,可插拔)     │
│  复用 hopebox:       mineru / mineru_popo / llm / dao / log / env │
│  internal/orm       documents 表(gorm)                │
│  internal/blobs     文件系统(PDF/images)                       │
└─────────────────────────────────┬──────────────────────────────┘
                                  │ HTTP
              ┌───────────────────┼───────────────────┐
              ▼                   ▼                   ▼
         MinerU HTTP       MinerU-Popo HTTP       LLM API
       (私有化部署)         (独立部署)         (DeepSeek/GLM)
```

## Project layout

Aligned with `curation-go` style (top-level `main.go`, no `cmd/`).

```
passion-index/
├── main.go                          # entry point
├── api/                             # REST handlers (image download)
├── graph/                           # GraphQL (gqlgen)
│   ├── schema/*.graphql             #   schema definition
│   ├── generated.go                 #   gqlgen-generated (don't edit)
│   ├── types/types_gen.go           #   gqlgen-generated types (don't edit)
│   └── *.resolvers.go               #   resolver implementations
├── models/                          # shared data models (Document/Node/Figure)
├── services/                        # business logic (_service suffix)
│   ├── pipeline_service/
│   ├── tree_service/
│   └── imageresolve_service/
├── internal/
│   ├── orm/                         # gorm models + DAO
│   └── blobs/                       # file system blob store
└── docs/superpowers/specs/          # design specs
```

## Development

```bash
make build      # compile binary
make run        # go run main.go
make gql        # regenerate GraphQL code (after editing schema/*.graphql)
make test       # run all tests
make fmt        # gofmt
make tidy       # go mod tidy
make hopebox    # bump hopebox dependency
make verify     # go vet + build smoke test
```

## External dependencies

This project reuses [hopebox](https://github.com/yichozy/hopebox) for:
- `hopebox/dao` — Postgres connection (POSTGRES_* env)
- `hopebox/log` — zap-based logger
- `hopebox/env` — `.env` loader
- `hopebox/mineru` — MinerU HTTP client
- `hopebox/llm` — multi-LLM client (OpenAI / Anthropic / GLM / DeepSeek / Gemini / vLLM)

Pending (tracked in spec §11.2):
- `hopebox/mineru_popo` — MinerU-Popo HTTP client (must be added to hopebox)
- MinerU-Popo HTTP service itself (currently only Python scripts; tracked as External E2 in the design spec)

## Tracking

- Upstream bug: [opendatalab/MinerU-Popo#11](https://github.com/opendatalab/MinerU-Popo/issues/11) — `img_path` propagation through `NormalizedBlock`
