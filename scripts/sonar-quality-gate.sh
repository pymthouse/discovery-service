#!/usr/bin/env bash
# Run SonarScanner locally and block until the SonarCloud quality gate completes.
#
# Prerequisites:
#   - SonarScanner CLI on PATH (https://docs.sonarsource.com/sonarqube-cloud/analyzing-source-code/scanners/sonarscanner/)
#   - Automatic Analysis DISABLED for this project (Administration → Analysis Method)
#   - User token (not project token): https://sonarcloud.io/account/security
#
# Usage:
#   export SONAR_TOKEN='<your-user-token>'
#   ./scripts/sonar-quality-gate.sh
#
# Optional overrides:
#   SONAR_ORGANIZATION (default: pymthouse)
#   SONAR_PROJECT_KEY  (default: pymthouse_discovery-service)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SONAR_ORGANIZATION="${SONAR_ORGANIZATION:-eliteprox}"
SONAR_PROJECT_KEY="${SONAR_PROJECT_KEY:-eliteprox_discovery-service}"
SONAR_HOST_URL="${SONAR_HOST_URL:-https://sonarcloud.io}"

if [[ -z "${SONAR_TOKEN:-}" ]]; then
  echo "SONAR_TOKEN is required (SonarCloud user token with analyze permission)." >&2
  exit 1
fi

echo "==> Go tests with coverage"
go test -race -coverprofile=coverage.out -covermode=atomic ./...

echo "==> SonarScanner (quality gate wait, timeout 300s)"
sonar-scanner \
  -Dsonar.host.url="${SONAR_HOST_URL}" \
  -Dsonar.organization="${SONAR_ORGANIZATION}" \
  -Dsonar.projectKey="${SONAR_PROJECT_KEY}" \
  -Dsonar.projectName=discovery-service \
  -Dsonar.qualitygate.wait=true \
  -Dsonar.qualitygate.timeout=300

echo "==> Quality gate passed"
