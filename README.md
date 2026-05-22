# discovery-service

Standalone Go service that aggregates orchestrator discovery sources, materializes results in Postgres, and serves client-driven runtime queries (filters, sort, topN).

Extracted from NaaP `orchestrator-leaderboard`. Plan filters remain a **client** concern: callers send filter/sort parameters on each `POST /v1/discovery/query`.

## Sources

| Kind | Env |
|------|-----|
| `livepeer-subgraph` | `SUBGRAPH_URL`, `SUBGRAPH_ID` |
| `clickhouse-query` | `CLICKHOUSE_*` or `CLICKHOUSE_GATEWAY_URL` |
| `naap-discover` | `DISCOVER_API_URL` |
| `naap-pricing` | `PRICING_API_URL` (disabled by default in DB seed) |
| `remote-signer` | `REMOTE_SIGNER_URL` (optional) |

## API documentation

When the service is running, open the home page for interactive docs:

- **http://localhost:8088/** → redirects to Scalar UI at `/docs`
- **http://localhost:8088/openapi.yaml** → OpenAPI 3.1 spec

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

`DATABASE_URL` in the `discovery` container is set by compose (`@postgres:5432`). Your `.env` still supplies other keys via `env_file`.

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
- `GET /v1/discovery/capabilities` — distinct capabilities
- `POST /v1/discovery/query` — client-driven ranked results
- `GET /v1/discovery/raw?caps=...` — webhook-compatible JSON for go-livepeer gateways
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
- **SonarQube Cloud:** [`.github/workflows/sonarqube-cloud.yml`](.github/workflows/sonarqube-cloud.yml) — requires `SONAR_TOKEN`, `SONAR_ORGANIZATION`, `SONAR_PROJECT_KEY`
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
