#!/usr/bin/env bash
set -euo pipefail

CONFIG=${NLI_CONTROLLER_CONFIG:-/etc/nli/controller.json}
ENV_FILE=${NLI_CONTROLLER_ENV_FILE:-/etc/nli/controller.env}
SERVICE=${NLI_CONTROLLER_SERVICE:-nli-controller-agent.service}

fail() { printf 'nli-supabase-setup: %s\n' "$*" >&2; exit 1; }
info() { printf 'nli-supabase-setup: %s\n' "$*"; }
[[ ${EUID:-$(id -u)} -eq 0 ]] || fail 'run as root: sudo nli-supabase-setup'
for command in python3 curl systemctl; do command -v "$command" >/dev/null || fail "$command is required"; done
[[ -f $CONFIG && -f $ENV_FILE ]] || fail 'install and configure the controller RPM first'

read_env() {
  local key=$1
  sed -n "s/^${key}=//p" "$ENV_FILE" | tail -n 1
}

supabase_url=$(read_env SUPABASE_URL)
supabase_anon_key=$(read_env SUPABASE_PUBLISHABLE_KEY)
[[ -n $supabase_anon_key ]] || supabase_anon_key=$(read_env SUPABASE_ANON_KEY)
supabase_controller_secret=$(read_env NLI_SUPABASE_CONTROLLER_SECRET)
supabase_storage_secret=$(read_env SUPABASE_SECRET_KEY)
[[ $supabase_url =~ ^https://[a-z0-9-]+\.supabase\.co/?$ || ${NLI_SUPABASE_ALLOW_INSECURE:-0} == 1 ]] || fail 'SUPABASE_URL is missing or invalid; rerun sudo nli-controller-setup'
[[ $(LC_ALL=C printf '%s' "$supabase_anon_key" | wc -c) -ge 32 ]] || fail 'SUPABASE_PUBLISHABLE_KEY (or legacy SUPABASE_ANON_KEY) is missing; rerun sudo nli-controller-setup'
[[ $(LC_ALL=C printf '%s' "$supabase_controller_secret" | wc -c) -ge 32 ]] || fail 'NLI_SUPABASE_CONTROLLER_SECRET is missing; rerun sudo nli-controller-setup'
[[ $supabase_storage_secret == sb_secret_* ]] || fail 'SUPABASE_SECRET_KEY is missing or is not a server-only sb_secret_ key; rerun sudo nli-controller-setup'
supabase_url=${supabase_url%/}

auth_headers=()
[[ $supabase_anon_key == sb_* ]] || auth_headers=(-H "Authorization: Bearer $supabase_anon_key")
result=$(curl --fail --silent --show-error --max-time 15 \
  -H 'Content-Type: application/json' \
  -H "apikey: $supabase_anon_key" \
  "${auth_headers[@]}" \
  -H "X-NLI-Controller-Secret: $supabase_controller_secret" \
  -d '{}' "$supabase_url/rest/v1/rpc/nli_health") || \
  fail 'protected Supabase RPC failed; run /root/nli-supabase-bootstrap.sql in the project SQL Editor'
printf '%s' "$result" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("status")=="ok" and d.get("schema_version")==1' || fail 'Supabase returned an incompatible NLI schema'

for bucket in nli-assurance-fixtures nli-rollout-artifacts; do
  bucket_json=$(curl --fail --silent --show-error --max-time 15 \
    -H "apikey: $supabase_storage_secret" \
    -H "Authorization: Bearer $supabase_storage_secret" \
    "$supabase_url/storage/v1/bucket/$bucket") || \
    fail "private Supabase Storage bucket $bucket is unavailable; apply /root/nli-supabase-bootstrap.sql"
  printf '%s' "$bucket_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); expected=sys.argv[1]; assert d.get("id")==expected and d.get("public") is False' "$bucket" || \
    fail "Supabase Storage bucket $bucket is missing or public"
done

readarray -t endpoint < <(python3 - "$CONFIG" <<'PY'
import json, pathlib, sys
value = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
listen = value.get("listen_address", "127.0.0.1:12382")
port = listen.rsplit(":", 1)[-1]
print("https" if value.get("tls_cert") else "http")
print(port)
assert value.get("storage_provider") == "supabase"
assert value.get("assurance_retention_enabled") is True
assert value.get("assurance_retention_manual") is True
PY
)
[[ ${#endpoint[@]} -eq 2 ]] || fail 'controller configuration is invalid'
local_health="${endpoint[0]}://127.0.0.1:${endpoint[1]}/api/v1/health"

systemctl daemon-reload
systemctl enable "$SERVICE" >/dev/null
systemctl restart "$SERVICE" || { journalctl -u "$SERVICE" --no-pager -n 80 >&2 || true; fail 'controller failed to start'; }
health_json=''
for _ in $(seq 1 30); do
  if health_json=$(curl --insecure --fail --silent --show-error "$local_health" 2>/dev/null); then break; fi
  sleep 1
done
printf '%s' "$health_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("service")=="nli-controller-agent" and d.get("api_version")=="v1"' || fail 'controller health endpoint failed after Supabase setup'

if grep -q '^NLI_BOOTSTRAP_ADMIN_PASSWORD=.' "$ENV_FILE"; then
  tmp=$(mktemp "${ENV_FILE}.XXXXXX")
  sed 's/^NLI_BOOTSTRAP_ADMIN_PASSWORD=.*/NLI_BOOTSTRAP_ADMIN_PASSWORD=/' "$ENV_FILE" >"$tmp"
  install -o root -g nli -m 0640 "$tmp" "$ENV_FILE"
  rm -f "$tmp"
  systemctl restart "$SERVICE"
fi

supabase_anon_key=''
supabase_controller_secret=''
supabase_storage_secret=''
info 'PASS: Supabase protected RPC storage is healthy'
info 'PASS: private Assurance and Rollout file buckets are healthy'
info 'PASS: controller is active with encrypted manual Assurance retention'
