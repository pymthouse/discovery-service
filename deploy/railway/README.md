# Railway HA Deployment Guide

Multi-region active-active deployment:

- **Neon Postgres** — shared HA database (primary + read replica)
- **discoveryd** — 3 replicas per region (private networking only)
- **apache** — 2 replicas per region (public URL, `mod_proxy_balancer` → discoveryd)
- **cron** — scheduled `POST /v1/discovery/dataset/refresh`
- **Cloudflare** — GeoDNS / load balancer across regional Apache endpoints

## Architecture

```
                    discovery.example.com (Cloudflare LB)
                              |
            +-----------------+-----------------+
            |                                   |
    Region A (e.g. us-west)              Region B (e.g. eu-west)
    apache x2 (public)                   apache x2 (public)
            |                                   |
    discoveryd x3 (private)              discoveryd x3 (private)
            |                                   |
            +-----------------+-----------------+
                              |
                    Neon Postgres (primary + replica)
```

## Prerequisites

- GitHub repo pushed (public or private with Railway access)
- [Railway](https://railway.app) account
- [Neon](https://neon.tech) account (Pro recommended for production — avoids auto-suspend)
- [Cloudflare](https://cloudflare.com) account (for multi-region DNS)
- ClickHouse / subgraph / discover API credentials

---

## 1. Neon Postgres (HA, shared)

1. Create a Neon project with **High Availability** enabled.
2. Set primary region to match Railway **Region A** (e.g. `aws-us-west-2`).
3. Add a **read replica** in the region matching Railway **Region B** (e.g. `aws-eu-central-1`).
4. Copy connection strings:
   - **Primary (pooled):** `DATABASE_URL` — used by all discoveryd instances for reads and writes.
   - **Replica (pooled):** `DATABASE_URL_READ` — optional v2 optimization for Region B reads.

Use the **pooled** connection string (`?sslmode=require`).

Neon handles automatic failover within the primary region. If the primary fails, Neon promotes a standby; update `DATABASE_URL` in Railway if the endpoint changes (Neon console shows the current primary).

---

## 2. Railway projects (two regions)

Create **two Railway projects** (or one project with two environments):

| Project | Railway region | Apache `LB_REGION` |
|---------|----------------|-------------------|
| `discovery-service-us` | US West | `us-west` |
| `discovery-service-eu` | EU West | `eu-west` |

In each project, create **three services** from the same GitHub repo:

### Service: `discoveryd` (private)

| Setting | Value |
|---------|-------|
| Dockerfile | `/Dockerfile` (repo root) |
| Replicas | **3** |
| Public networking | **Disabled** (private only) |
| Service name | `discoveryd` (required for `discoveryd.railway.internal`) |
| Config file | [`discoveryd.railway.toml`](discoveryd.railway.toml) |

**Variables** (use Railway shared variables or copy per project):

```env
HTTP_ADDR=:8088
DATABASE_URL=<neon-primary-pooled-url>
REDIS_URL=<optional-upstash-redis-url>
CRON_SECRET=<random-32+-chars>
LEADERBOARD_REFRESH_INTERVAL_MS=60000
MEMBERSHIP_STRATEGY=union
QUERY_CACHE_TTL_MS=120000
MAX_TOP_N=1000
CLICKHOUSE_URL=<your-clickhouse>
CLICKHOUSE_USER=default
CLICKHOUSE_PASSWORD=<secret>
SUBGRAPH_URL=https://gateway.thegraph.com/api/<SUBGRAPH_ID>/subgraphs/id/<id>
SUBGRAPH_ID=FE63YgkzcpVocxdCEyEYbvjYqEf2kb1A6daMYRxmejYC
DISCOVER_API_URL=https://naap-api.cloudspe.com/v1/discover/orchestrators
PRICING_API_URL=
REMOTE_SIGNER_URL=
REFRESH_ON_STARTUP=false
```

Set `REFRESH_ON_STARTUP=false` so 3 replicas do not all refresh on boot; use the cron service instead.

### Service: `apache` (public)

| Setting | Value |
|---------|-------|
| Dockerfile | `/deploy/apache/Dockerfile` |
| Root directory | repo root (Dockerfile path is relative) |
| Replicas | **2** |
| Public networking | **Enabled** |
| Config file | [`apache.railway.toml`](apache.railway.toml) |

**Variables:**

```env
PORT=8080
DISCOVERYD_UPSTREAM_LIST=http://discoveryd.railway.internal:8088
LB_REGION=us-west
```

Railway private DNS resolves `discoveryd.railway.internal` and load-balances across the 3 discoveryd replicas automatically. One `BalancerMember` is sufficient; Apache health-checks each request via `hcuri=/healthz`.

Generate a **public domain** for apache (e.g. `discovery-us.up.railway.app`).

### Service: `cron` (scheduled)

| Setting | Value |
|---------|-------|
| Dockerfile | `/deploy/cron/Dockerfile` |
| Cron schedule | `0 */1 * * *` (hourly) or `*/30 * * * *` |
| Config file | [`cron.railway.toml`](cron.railway.toml) |

**Variables:**

```env
DISCOVERY_URL=https://discovery-us.up.railway.app
CRON_SECRET=<same-as-discoveryd>
```

Point `DISCOVERY_URL` at the **apache public URL** in that region (or the global Cloudflare URL once configured).

Repeat all three services in **Region B** with `LB_REGION=eu-west` and Region B apache public URL for that region's cron (`DISCOVERY_URL` can be the global URL after Cloudflare is live).

---

## 3. Cloudflare multi-region DNS

After both regions deploy:

1. Note public Apache URLs:
   - Region A: `https://discovery-us-xxxx.up.railway.app`
   - Region B: `https://discovery-eu-xxxx.up.railway.app`
2. In Cloudflare DNS for your domain (e.g. `discovery.example.com`):

### Option A: Cloudflare Load Balancer (recommended)

1. Traffic → Load Balancing → Create load balancer
2. Hostname: `discovery.example.com`
3. Add two **pools** (one per region), each with one origin (Apache public URL hostname)
4. Health monitor: `GET /healthz`, interval 60s, retries 2
5. Steering: **Geo** or **Dynamic** nearest pool

### Option B: Simple DNS round-robin

Create two `CNAME` records pointing to each Railway Apache hostname (limited failover; LB is preferred).

### Custom domain on Railway

Optionally attach `discovery.example.com` to **both** Apache services in Railway (Settings → Domains), then use Cloudflare as DNS-only or proxied.

---

## 4. Smoke tests

Replace `BASE` with your public URL (Cloudflare or regional Apache).

```bash
BASE=https://discovery.example.com

# Docs home page (Scalar API reference)
curl -sI "$BASE/docs" | head -5
curl -sI "$BASE/" | head -5   # redirects to /docs

# OpenAPI spec
curl -s "$BASE/openapi.yaml" | head -20

# Health
curl -s "$BASE/healthz"

# Freshness (before refresh may show populated=false)
curl -s "$BASE/v1/discovery/freshness" | jq .

# Trigger refresh (use CRON_SECRET)
curl -s -X POST "$BASE/v1/discovery/dataset/refresh" \
  -H "Authorization: Bearer $CRON_SECRET" | jq .

# Capabilities
curl -s "$BASE/v1/discovery/capabilities" | jq .

# Query
curl -s -X POST "$BASE/v1/discovery/query" \
  -H "Content-Type: application/json" \
  -d '{"capabilities":["streamdiffusion-sdxl"],"topN":5,"sortBy":"slaScore"}' | jq .

# Webhook raw
curl -s "$BASE/v1/discovery/raw?caps=streamdiffusion-sdxl" | jq '.[0:3]'
```

### HA replica kill test

1. In Railway, open `discoveryd` → **Replicas** → stop or restart one replica.
2. Repeat smoke tests — requests should succeed via Apache → remaining replicas.
3. Stop one **apache** replica — public URL should still work via Railway edge + second apache replica.
4. Simulate region failure: disable one Cloudflare pool — traffic should route to the other region.

---

## 5. Security checklist

- [ ] `CRON_SECRET` is random and only on Railway secrets (never in git)
- [ ] `discoveryd` has **no** public URL
- [ ] Neon uses `sslmode=require`
- [ ] Railway variables marked as secrets for passwords
- [ ] Cloudflare proxy + WAF optional
- [ ] Read endpoints are public by design; add API keys later if needed

---

## 6. NaaP integration

In NaaP `apps/web-next`:

```env
DISCOVERY_SERVICE_URL=https://discovery.example.com
```

Dataset refresh and plan evaluation delegate to this URL when set.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Apache 502 | Check `discoveryd` is healthy; verify `DISCOVERYD_UPSTREAM_LIST=http://discoveryd.railway.internal:8088` and service is named `discoveryd` |
| Empty query results | Run refresh cron or `POST /v1/discovery/dataset/refresh` |
| ClickHouse errors in logs | Verify `CLICKHOUSE_*` secrets; check IP allowlist on ClickHouse Cloud |
| Neon connection refused | Use pooled URL; check SSL; verify Neon not suspended (upgrade from free tier) |
| Docs page blank | Check `/openapi.yaml` returns YAML; Scalar loads from CDN |
