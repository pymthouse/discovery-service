# discovery-service

Standalone Go service that aggregates orchestrator discovery sources, materializes results in Postgres, and serves client-driven runtime queries (filters, sort, topN).

Extracted from NaaP `orchestrator-leaderboard`. Plan filters remain a **client** concern: callers send filter/sort parameters on each `POST /v1/discovery/query`.

## Sources

| Kind | Env |
|------|-----|
| `livepeer-subgraph` | `SUBGRAPH_URL`, `SUBGRAPH_ID` |
| `livepeer-registry-manifest` | `REGISTRY_MANIFEST_*` (probes on-chain `serviceURI` manifests) |
| `livepeer-ai-registry-manifest` | `AI_SERVICE_REGISTRY_*`, `REGISTRY_MANIFEST_*` (reads AI Service Registry `getServiceURI`) |
| `clickhouse-query` | `CLICKHOUSE_*` or `CLICKHOUSE_GATEWAY_URL` |
| `naap-discover` | `DISCOVER_API_URL` |
| `naap-pricing` | `PRICING_API_URL` (disabled by default in DB seed) |
| `remote-signer` | `REMOTE_SIGNER_URL` (optional) |

During refresh, discovery-service also probes each known orchestrator's `GET /discovery`
for live-runner apps (`ORCH_DISCOVERY_*`). App IDs (e.g. `transcode/ffmpeg`) are
materialized as capabilities so `/v1/discovery/raw?caps=transcode/ffmpeg` returns
matching orch addresses. Clients then fan out to each orch's `/discovery` for runners.
These probes skip TLS certificate verification (self-signed / hostname-mismatched orch
certs are common); other HTTP sources still verify TLS normally.

Dataset rows carry an explicit `service_type`:

- `legacy` — ClickHouse, discover API, pricing, remote-signer
- `registry` — capabilities from on-chain `serviceURI` manifests (`livepeer-network-modules` v3 or coordinator envelope), including the AI Service Registry at `0x04C0b249740175999E5BF5c9ac1dA92431EF34C5`

Filter with `serviceTypes` on `POST /v1/discovery/query`, or `?serviceType=registry` on `/capabilities` and `/raw`.

## API documentation

When the service is running, open the home page for interactive docs:

- **http://localhost:8088/** → redirects to Scalar UI at `/docs`
- **http://localhost:8088/openapi.yaml** → OpenAPI 3.1 spec

In production, set `PUBLIC_BASE_URL` (or rely on Railway’s `RAILWAY_PUBLIC_DOMAIN`) so Scalar’s server URL is the real public hostname instead of `localhost`. This value comes from config only — never from request `Host` headers.

## Production deployment (Railway HA)

See **[deploy/railway/README.md](deploy/railway/README.md)** for multi-region Railway deployment with:

- Neon Postgres (HA + read replica)
- 3× `discoveryd` + 2× Apache LB per region
- Cloudflare GeoDNS
- Scheduled dataset refresh cron

## Docker Compose (recommended)

Postgres and `discoveryd` share the default compose network. **Postgres is not published to the host** — only `discovery` talks to it at `postgres:5432`.

```bash
cp .env.example .env   # ClickHouse, discover API, etc.
make compose-up
# or: docker compose up -d --build
```

| Service | Host access |
|---------|-------------|
| API (`discovery`) | http://localhost:8088 |
| Postgres | internal only (`postgres:5432` on compose network) |

For full compose, the `discovery` container clears `DATABASE_URL` and builds the internal Postgres connection from `DISCOVERY_PG_*` settings (`postgres:5432`). Your `.env` still supplies the password and other keys via `env_file`.

```bash
curl http://localhost:8088/healthz
curl http://localhost:8088/v1/discovery/freshness
make compose-logs
make compose-down
```

On first start, `REFRESH_ON_STARTUP=true` runs a dataset refresh when the DB is empty or stale.

### Host-side `go run` (optional Postgres port)

To run `discoveryd` on your machine while Postgres stays in Docker, use the dev overlay to expose **5433**:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d postgres
# .env: DATABASE_URL=postgres://discovery:discovery@localhost:5433/discovery?sslmode=disable
go run ./cmd/discoveryd
```

`discoveryd` loads `.env` from the repo root (via `godotenv`). Shell exports override `.env` values.

## API

- `GET /healthz` — liveness
- `GET /v1/discovery/freshness` — dataset age and row counts
- `GET /v1/discovery/capabilities?serviceType=registry` — capability names plus `entries` metadata
- `POST /v1/discovery/query` — client-driven ranked results (`serviceTypes: ["legacy","registry"]`)
- `GET /v1/discovery/raw?caps=...&serviceType=legacy` — webhook-compatible JSON for go-livepeer gateways (also accepts live-runner app IDs such as `caps=transcode/ffmpeg` from orch `/discovery` probes)
- `POST /v1/discovery/dataset/refresh` — refresh (Bearer `CRON_SECRET` or `X-Cron-Secret`)

## NaaP integration

Set in `apps/web-next`:

```
DISCOVERY_SERVICE_URL=http://localhost:8088
```

Dataset refresh and plan evaluation then delegate to this service.

## Security

Automated gates and edge hardening are defined in:

- **CI:** [`.github/workflows/ci.yml`](.github/workflows/ci.yml) — `go test`, `go vet`, `gofmt`, `golangci-lint`
- **CodeQL (GHAS):** [`.github/workflows/codeql.yml`](.github/workflows/codeql.yml)
- **SonarQube Cloud:** [`.github/workflows/sonarqube-cloud.yml`](.github/workflows/sonarqube-cloud.yml) + [`sonar-project.properties`](sonar-project.properties) — requires `SONAR_TOKEN`, `SONAR_ORGANIZATION`, `SONAR_PROJECT_KEY`
- **Dependabot:** [`.github/dependabot.yml`](.github/dependabot.yml)
- **Apache edge:** [`deploy/apache/httpd.conf.template`](deploy/apache/httpd.conf.template) — request limits, security headers, cache policy
- **Runbook:** [`deploy/railway/SECURITY.md`](deploy/railway/SECURITY.md) — secrets, branch protection, triage, Cloudflare rate limits

Enable **Code scanning** and required status checks on `main` before production rollout (see runbook).

## Tests

```bash
go test ./...
gofmt -l .    # should print nothing
go vet ./...
```

Install the pre-commit hook once per clone (auto-formats staged `.go` files; blocks commits that would fail CI `gofmt`):

```bash
make hooks
```
