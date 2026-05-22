#!/bin/sh
set -eu

DISCOVERY_URL="${DISCOVERY_URL:?DISCOVERY_URL required, e.g. https://discovery.example.com}"
CRON_SECRET="${CRON_SECRET:?CRON_SECRET required}"

echo "Refreshing discovery dataset at ${DISCOVERY_URL}..."

curl -sf -X POST "${DISCOVERY_URL}/v1/discovery/dataset/refresh" \
  -H "Authorization: Bearer ${CRON_SECRET}" \
  -H "X-Refreshed-By: railway-cron" \
  -H "Content-Type: application/json"

echo ""
echo "Refresh OK at $(date -u +%Y-%m-%dT%H:%M:%SZ)"
