# passion-index

A self-hosted PDF structuring service. It ingests PDFs, runs them through MinerU (OCR) and MinerU-Popo (structuring), persists the result as a tree in PostgreSQL, and exposes document queries and image access via GraphQL and HTTP.

Supports two workflows:
- **Local dev**: `go run` + `.env`
- **Production**: Docker image + GitHub tag release + Helm chart

Design background: [2026-08-04-passion-index-design.md](docs/superpowers/specs/2026-08-04-passion-index-design.md).

## Overview

### Core capabilities

- Upload PDF: GraphQL `UploadDocument`
- Get single document: GraphQL `GetDocumentByID`
- Paginated document list: GraphQL `GetDocumentList`
- Look up tree nodes by pages: GraphQL `GetDocumentNodesByPages`
- Delete document: GraphQL `DeleteDocument`
- Get document image: `GET /documents/:docID/images/:name` (302 redirect to OSS signed URL)
- Health check: `GET /healthz`

### Endpoints

| Endpoint | Description |
|---|---|
| `POST /query` | GraphQL entry point (supports multipart upload) |
| `GET /` | GraphiQL playground (dev only) |
| `GET /healthz` | Health check |
| `GET /documents/:docID/images/:name` | Image access — redirects to OSS |

### Dependencies

- **PostgreSQL**: stores the `documents` table and tree results
- **Aliyun OSS**: stores uploaded PDFs and extracted figure images
- **MinerU**: OCR / document parsing (private deployment)
- **MinerU-Popo**: tree structuring (separate deployment)
- **LLM API**: leaf-node summarization (DeepSeek / Doubao)

## Local development

### Quick start

```bash
make tidy
cp .env.example .env  # edit with real values
make run
```

Default port is controlled by `PASSION_INDEX_PORT` (defaults to `8080`). The `.env.example` uses `8900`.

### Environment variables

When `ENV != prod`, the app automatically loads `.env`. Copy [`.env.example`](.env.example) and fill in real values.

**Runtime**
- `ENV`, `SILENT_LOG`, `PASSION_INDEX_PORT`

**PostgreSQL**
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USERNAME`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

**Aliyun OSS**
- `ALIYUN_OSS_ACCESS_KEY`, `ALIYUN_OSS_ACCESS_SECRET`, `ALIYUN_OSS_BUCKET`, `ALIYUN_OSS_ENDPOINT`

**MinerU / Popo**
- `MINERU_PRIVATE_BASE_URL`, `MINERU_PRIVATE_TOKEN`
- `MINERU_POPO_BASE_URL`, `MINERU_POPO_TOKEN`

**LLM**
- `DEEP_SEEK_API_KEY`, `HUO_SHANG_ARK_API_KEY`
- `PADDLE_OCR_KEY` — required for DeepSeek to "see" figure images (PaddleOCR converts them to text)
- `PASSION_INDEX_LEAF_MODEL` — primary model for leaf summaries (default: `deepseek-v4-flash`)
- `PASSION_INDEX_LEAF_FALLBACK_MODEL` — fallback model (default: `HUOSHANG-doubao-seed-evolving`)

### Make commands

```bash
make build      # compile binary to bin/passion-index
make run        # run locally
make gql        # regenerate gqlgen code after editing schema/*.graphql
make test       # run all tests
make fmt        # gofmt
make tidy       # go mod tidy
make hopebox    # upgrade hopebox dependency
make verify     # go vet + build smoke test
```

### Debug scripts

Located in `scripts/`:

| Script | Usage |
|---|---|
| `scripts/upload.sh <pdf>` | Upload a PDF, returns doc_id |
| `scripts/poll.sh <doc_id> [interval] [max_min]` | Poll status until DONE/FAILED |
| `scripts/doc.sh <doc_id>` | Show document metadata |
| `scripts/tree.sh <doc_id> [--raw]` | Show document tree (pretty outline or raw JSON) |

## GraphQL schema

Defined in [graph/schema/document.graphql](graph/schema/document.graphql).

**Types**: `Document` (metadata + tree), `TreeNode` (title, page range, summary, text, figures, children), `Figure` (name, page, caption), `DocumentList` (paginated items + total).

**Example queries**:

```graphql
query {
  GetDocumentList(limit: 10, offset: 0) {
    total
    items { doc_id filename status created_at }
  }
}

mutation {
  UploadDocument(file: null) { doc_id filename status }
}
```

Upload requires multipart form (see `scripts/upload.sh`).

## Docker

### Build locally

```bash
DOCKER_BUILDKIT=1 docker build \
  --build-arg GITHUB_TOKEN=<your_token> \
  -t passion-index:local .
```

`GITHUB_TOKEN` needs read access to the private `github.com/yichozy/hopebox` repository.

### CI/CD

GitHub Actions workflow: [`.github/workflows/dev.yml`](.github/workflows/dev.yml).

Publish flow:
- **Trigger**: push a semver tag (e.g., `v0.1.0`)
- **Image tag**: `${{ github.ref_name }}`
- **Registry**: `${{ vars.ALIYUN_REGISTRY_SG }}/knewta/passion-index:<tag>`

```bash
git tag v0.1.0
git push origin v0.1.0
```

Required GitHub repo configuration:
- **Variables**: `ALIYUN_REGISTRY_SG`
- **Secrets**: `ALIYUN_REGISTRY_USERNAME`, `ALIYUN_REGISTRY_PASSWORD`, `GH_PACKAGES_TOKEN`

## Helm deployment

Chart: [charts/passion-index](charts/passion-index).

```bash
helm upgrade --install passion-index ./charts/passion-index \
  --namespace yi-prod \
  --create-namespace \
  --set image.repository=<registry>/knewta/passion-index \
  --set image.tag=v0.1.0
```

### Kubernetes Secrets

Create these Secrets before deploying (chart does not manage them):

| Secret | Keys |
|---|---|
| `postgres` | `PASSION_INDEX_POSTGRES_HOST/PORT/USERNAME/PASSWORD/DB` (mapped to runtime `POSTGRES_*`) |
| `aliyun-oss` | `ALIYUN_OSS_ACCESS_KEY`, `ALIYUN_OSS_ACCESS_SECRET`, `ALIYUN_OSS_BUCKET`, `ALIYUN_OSS_ENDPOINT` |
| `mineru-private` | `MINERU_PRIVATE_BASE_URL`, `MINERU_POPO_BASE_URL`, `MINERU_PRIVATE_TOKEN`, `MINERU_POPO_TOKEN` |
| `paddle-orc` | `PADDLE_OCR_KEY` |
| `deepseek` | `DEEP_SEEK_API_KEY` |
| `huoshang` | `HUO_SHANG_ARK_API_KEY` |

The chart maps each key explicitly via `secretKeyRef` (no `envFrom`).

## Project structure

```
passion-index/
├── .github/workflows/dev.yml          # CI: tag-triggered image build & push
├── charts/passion-index/              # Helm chart
├── graph/                             # GraphQL schema, resolvers, generated code
├── internal/orm/                      # database migration (gorm AutoMigrate)
├── internal/orm_document/             # documents table CRUD
├── models/                            # Document / Node / Figure models
├── scripts/                           # local debugging scripts
├── services/document_service/         # upload, pipeline, tree mapping, summarization
├── .env.example
├── Dockerfile
├── Makefile
└── main.go
```

## Dependencies

Reuses [hopebox](https://github.com/yichozy/hopebox) for:
- `hopebox/env`, `hopebox/log`, `hopebox/dao` — config, logging, PostgreSQL
- `hopebox/aliyun` — OSS client
- `hopebox/mineru_private`, `hopebox/mineru_popo` — MinerU HTTP clients
- `hopebox/llm`, `hopebox/llm_types` — LLM abstraction (DeepSeek, Doubao, GLM, Claude)
- `hopebox/gatlin` — bounded-concurrency goroutine groups
- `hopebox/paddleocr` — image-to-text OCR for DeepSeek (which can't read image URLs natively)

External services (must be running):
- MinerU OCR (private deployment)
- MinerU-Popo structuring service (separate deployment)

## Known issues

- Image access uses HTTP 302 redirect to OSS signed URLs (not embedded in GraphQL responses)
- Upstream tracking: [opendatalab/MinerU-Popo#11](https://github.com/opendatalab/MinerU-Popo/issues/11) — `img_path` propagation through `NormalizedBlock`
