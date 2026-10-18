#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${NLI_BUILD_SCRATCH:?NLI_BUILD_SCRATCH is required}"
SCRATCH="$NLI_BUILD_SCRATCH/nli-webview-tests-$$"
PORT="${PORT:-12382}"
NAME="nli-webview-$$"
NETWORK="$NAME-net"
mkdir -p "$SCRATCH"/{tmp,gocache,gomodcache,state,web}
for asset in index.html styles.css app.js nli-mark.svg README.txt; do install -m 0644 "$ROOT/web/$asset" "$SCRATCH/web/$asset"; done
ADMIN_PASSWORD="Browser-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')!"
cat >"$SCRATCH/controller.json" <<'JSON'
{"listen_address":"0.0.0.0:12382","state_dir":"/scratch/state","web_dir":"/scratch/web","allow_insecure_remote":true,"storage_provider":"local","storage_dir":"/scratch/state/metadata","event_path":"/scratch/state/event-metadata.jsonl","event_retention":1000,"enable_legacy_event_ingest":false,"assurance_retention_enabled":true,"assurance_retention_max":50,"assurance_retention_hours":24,"artifact_public_keys":[]}
JSON
KEY="$(printf '0123456789abcdefghijklmnopqrstuv' | base64 | tr -d '\n')"
cleanup(){ docker rm -f "$NAME" >/dev/null 2>&1 || true; docker network rm "$NETWORK" >/dev/null 2>&1 || true; }
trap cleanup EXIT
if [[ ${NLI_SKIP_GO_GATE:-0} != 1 ]]; then
  docker run --rm --mount "type=bind,src=$ROOT,dst=/src,readonly" --mount "type=bind,src=$SCRATCH,dst=/scratch" --mount "type=volume,src=nli-go-build-cache,dst=/root/.cache/go-build" --mount "type=volume,src=nli-go-mod-cache,dst=/go/pkg/mod" -e TMPDIR=/scratch/tmp golang:1.24.9 bash -c 'cd /src && /usr/local/go/bin/go test -count=1 ./internal/auth ./internal/auditlog ./internal/middleware ./internal/store ./internal/eventstore ./internal/controlplane ./internal/webtest ./internal/webview'
fi
docker network create "$NETWORK" >/dev/null
docker run -d --name "$NAME" --network "$NETWORK" -p "127.0.0.1:$PORT:12382" --mount "type=bind,src=$ROOT,dst=/src,readonly" --mount "type=bind,src=$SCRATCH,dst=/scratch" --mount "type=volume,src=nli-go-build-cache,dst=/root/.cache/go-build" --mount "type=volume,src=nli-go-mod-cache,dst=/go/pkg/mod" -e TMPDIR=/scratch/tmp -e NLI_ENROLLMENT_TOKEN=Browser-enrollment-token-123456 -e NLI_BOOTSTRAP_ADMIN_PASSWORD="$ADMIN_PASSWORD" -e NLI_AUDIT_HMAC_KEY="$KEY" -e NLI_CSRF_HMAC_KEY="$KEY" -e NLI_ASSURANCE_ENCRYPTION_KEY="$KEY" golang:1.24.9 bash -c 'cd /src && exec /usr/local/go/bin/go run ./cmd/nli-control --config /scratch/controller.json' >/dev/null
for _ in $(seq 1 90); do curl -fsS "http://127.0.0.1:$PORT/api/v1/health" >/dev/null && break; sleep 1; done
curl -fsS "http://127.0.0.1:$PORT/api/v1/health" >/dev/null
docker run --rm --network "$NETWORK" --mount "type=bind,src=$ROOT,dst=/src,readonly" --mount "type=bind,src=$SCRATCH,dst=/scratch" --mount "type=volume,src=nli-npm-cache,dst=/root/.npm" -e NLI_BASE_URL="http://$NAME:12382" -e NLI_ADMIN_PASSWORD="$ADMIN_PASSWORD" -e NLI_E2E_OUTPUT=/scratch/e2e-results mcr.microsoft.com/playwright:v1.64.1-noble bash -c 'rm -rf /scratch/playwright-work && mkdir -p /scratch/playwright-work && cp /src/web/package.json /src/web/playwright.config.js /src/web/e2e.spec.js /scratch/playwright-work/ && cd /scratch/playwright-work && npm install --ignore-scripts --no-audit --no-fund && npx playwright test'
