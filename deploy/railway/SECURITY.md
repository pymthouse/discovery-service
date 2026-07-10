# discovery-service Security Runbook

Operational security controls for Railway + Cloudflare production and GitHub CI enforcement.

## Required GitHub configuration

### Repository secrets and variables

| Name | Type | Used by | Rotation |
|------|------|---------|----------|
| `SONAR_TOKEN` | Secret | [`.github/workflows/sonarqube-cloud.yml`](../../.github/workflows/sonarqube-cloud.yml) | User/org token with analyze permission ([GitHub Actions setup](https://docs.sonarsource.com/sonarqube-cloud/analyzing-source-code/ci-based-analysis/github-actions-for-sonarcloud)) |
| `SONAR_ORGANIZATION` | Variable | SonarQube Cloud workflow | Sonar org key (Settings → Organization) |
| `SONAR_PROJECT_KEY` | Variable | SonarQube Cloud workflow | Project key from SonarQube Cloud project setup |
| *(none in CI)* | — | Railway only | — |

Railway / Neon secrets (never commit):

| Variable | Service | Notes |
|----------|---------|-------|
| `CRON_SECRET` | discoveryd, cron | Random 32+ chars; protects `POST /v1/discovery/dataset/refresh` |
| `DATABASE_URL` | discoveryd | Neon pooled URL, `sslmode=require` |
| `CLICKHOUSE_PASSWORD` | discoveryd | ClickHouse Cloud credential |
| `REDIS_URL` | discoveryd | Optional query cache |

Rotate `CRON_SECRET` by generating a new value, updating Railway shared variables for **discoveryd** and **cron**, then redeploying both services.

### Branch protection (`main`)

Require status checks before merge:

- **CI** (`quality`, `build`)
- **CodeQL** (`analyze`)
- **SonarQube Cloud** (`scan`) — after `SONAR_TOKEN`, `SONAR_ORGANIZATION`, and `SONAR_PROJECT_KEY` are configured

Enable in: **Settings → Branches → Branch protection rules → Require status checks**.

### GitHub Advanced Security (GHAS)

1. **Settings → Code security and analysis**
2. Enable **Code scanning** (CodeQL uses [`.github/workflows/codeql.yml`](../../.github/workflows/codeql.yml))
3. Optional: **Copilot Autofix** for CodeQL alerts on supported plans

Dependabot ([`.github/dependabot.yml`](../../.github/dependabot.yml)) opens weekly PRs for Go modules, Docker base images, and GitHub Actions.

### Security scanning (current stack)

| Tool | Workflow | Role |
|------|----------|------|
| CodeQL | [`.github/workflows/codeql.yml`](../../.github/workflows/codeql.yml) | GitHub-native SAST (Go) |
| SonarQube Cloud | [`.github/workflows/sonarqube-cloud.yml`](../../.github/workflows/sonarqube-cloud.yml), [`sonar-project.properties`](../../sonar-project.properties) | Code quality, security hotspots, coverage |
| Dependabot | [`.github/dependabot.yml`](../../.github/dependabot.yml) | Dependency and base-image updates |

Snyk is **not** used in this repository.

## Scanner triage

| Source | Owner | SLA | Fail policy |
|--------|-------|-----|-------------|
| CodeQL (GHAS) | Engineering | Critical: 7d, High: 14d | Blocks merge via required check |
| SonarQube Cloud | Engineering | Blocker/Critical: 7d, Major: 14d | Quality Gate / new issues on PR |
| Dependabot | On-call rotation | Security advisories: 7d | Manual PR review |

**Suppression policy:** Document reason + owner + expiration in PR or SonarQube/CodeQL dismissal comment. No permanent suppressions without security review.

## Edge rate limiting (Cloudflare — primary)

Apache enforces request size/time limits; **per-IP request rate** is enforced at Cloudflare in front of regional Apache URLs.

Recommended starting rules (tune after observing traffic):

| Rule | Path | Threshold | Action |
|------|------|-----------|--------|
| Global API | `*discovery.example.com/v1/discovery/*` | 120 req/min per IP | Managed challenge or block |
| Refresh | `*/v1/discovery/dataset/refresh` | 10 req/min per IP | Block (cron uses single IP) |
| Docs | `*/docs`, `*/openapi.yaml` | 300 req/min per IP | Challenge |

Enable **WAF** managed rulesets on the proxied hostname. Use Cloudflare Load Balancer health checks on `GET /healthz`.

## Cache policy

| Endpoint | Cache-Control | Rationale |
|----------|---------------|-----------|
| `/docs`, `/openapi.yaml` | `public, max-age=300` | Static docs; short TTL |
| `/v1/discovery/capabilities` | `public, max-age=300` | Changes only on dataset refresh |
| `/v1/discovery/query` (POST) | `no-store, private` | Client-specific filters; Go layer |
| `/v1/discovery/dataset/refresh` | `no-store, private` | Mutates dataset; Go layer |
| `/v1/discovery/freshness`, `/healthz` | `no-store` | Operational truth |

Go handlers in `internal/httpapi/server.go` set the values above. Apache [`httpd.conf.template`](../apache/httpd.conf.template) mirrors the same policy at the edge for proxied routes.

## Emergency controls

1. **Stop public refresh abuse:** Cloudflare block rule on `POST .../dataset/refresh` or rotate `CRON_SECRET` and update cron only.
2. **Region failover:** Disable unhealthy pool in Cloudflare LB; traffic shifts to other region.
3. **Rollback:** Redeploy previous Railway deployment for `apache` / `discoveryd`; Neon data is shared — no DB rollback needed for app-only regressions.
4. **Disable discoveryd exposure:** Ensure discoveryd has **no** public Railway URL (private networking only).

## Verification (smoke + security)

From a machine with `curl` and optional `hey`:

```bash
BASE=https://discovery.example.com

# Security headers (via Apache)
curl -sI "$BASE/docs" | grep -iE 'x-content-type|x-frame|cache-control|referrer'

# Cacheable capabilities
curl -sI "$BASE/v1/discovery/capabilities" | grep -i cache-control

# Non-cacheable query
curl -sI -X POST "$BASE/v1/discovery/query" \
  -H "Content-Type: application/json" \
  -d '{"capabilities":["streamdiffusion-sdxl"],"topN":1}' | grep -i cache-control

# Refresh requires auth
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$BASE/v1/discovery/dataset/refresh"
# Expect 401 without Bearer token
```

Burst test (optional, requires [hey](https://github.com/rakyll/hey)):

```bash
hey -n 200 -c 20 -m GET "$BASE/healthz"
```

Expect 200s; if Cloudflare rate limits are active, some 429/challenge responses are expected under extreme load.

## CI local parity

```bash
gofmt -l .          # must be empty
go vet ./...
go test -race ./...
golangci-lint run   # if installed
docker build -f deploy/apache/Dockerfile -t discovery-apache:test .
```
