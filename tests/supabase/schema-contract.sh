#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck disable=SC1091
source "$ROOT/packaging/docker/images.env"
container="nli-supabase-schema-$$"
scratch=$(mktemp -d "${NLI_BUILD_SCRATCH:?NLI_BUILD_SCRATCH is required}/nli-supabase.XXXXXXXX")
secret='nli-schema-contract-secret-0123456789abcdef'
digest=$(printf '%s' "$secret" | sha256sum | awk '{print $1}')
payload_digest=$(printf '%s' '{"value":1}' | sha256sum | awk '{print $1}')
sed "s/__NLI_CONTROLLER_SECRET_SHA256__/$digest/" "$ROOT/supabase/nli_schema.sql" >"$scratch/schema.sql"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT

docker run -d --name "$container" -e POSTGRES_HOST_AUTH_METHOD=trust "$POSTGRES_IMAGE" >/dev/null
for _ in $(seq 1 60); do
  docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null
docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -U postgres -c 'create role anon nologin; create role authenticated nologin; create schema storage; create table storage.buckets (id text primary key, name text not null, public boolean not null, file_size_limit bigint, allowed_mime_types text[]);' >/dev/null
docker exec -i "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -U postgres <"$scratch/schema.sql" >/dev/null
[[ $(docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -At -U postgres -c "select count(*) from storage.buckets where id in ('nli-assurance-fixtures','nli-rollout-artifacts','nli-geoip-databases') and public=false") == '3' ]] || { echo 'NLI Storage buckets are not private or missing' >&2; exit 1; }
[[ $(docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -At -U postgres -c "select allowed_mime_types = array['application/octet-stream']::text[] and file_size_limit=8388608 from storage.buckets where id='nli-assurance-fixtures'") == 't' ]] || { echo 'Assurance bucket must accept only bounded encrypted octet-stream objects' >&2; exit 1; }
[[ $(docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -At -U postgres -c "select allowed_mime_types @> array['application/x-rpm','application/json']::text[] and cardinality(allowed_mime_types)=3 and file_size_limit=536870912 from storage.buckets where id='nli-rollout-artifacts'") == 't' ]] || { echo 'Rollout bucket MIME or size allowlist is invalid' >&2; exit 1; }
[[ $(docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -At -U postgres -c "select allowed_mime_types = array['application/octet-stream']::text[] and file_size_limit=134217728 from storage.buckets where id='nli-geoip-databases'") == 't' ]] || { echo 'GeoIP bucket must be private octet-stream with a 128 MiB object bound' >&2; exit 1; }

docker exec -i "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -qAt -U postgres <<SQL >"$scratch/result"
set role anon;
set request.headers = '{"x-nli-controller-secret":"$secret"}';
select public.nli_health()->>'status';
select public.nli_put_document('contract','item','draft','{"value":1}'::jsonb,'$payload_digest','contract-test',false)->>'revision';
select jsonb_array_length(public.nli_list_documents('contract','',10,0));
select public.nli_get_document('contract','item',0)->'payload'->>'value';
select public.nli_delete_document('contract','item',true,'contract-test')->>'status';
SQL
[[ $(tr '\n' ' ' <"$scratch/result") == 'ok 1 1 1 purged ' ]] || {
  echo 'Supabase protected RPC contract returned unexpected results' >&2
  cat "$scratch/result" >&2
  exit 1
}
if docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -At -U postgres -c "set role anon; set request.headers = '{\"x-nli-controller-secret\":\"wrong-secret\"}'; select public.nli_health();" >/dev/null 2>&1; then
  echo 'Supabase RPC accepted an incorrect controller secret' >&2
  exit 1
fi
if docker exec "$container" psql -h 127.0.0.1 -v ON_ERROR_STOP=1 -At -U postgres -c 'set role anon; select count(*) from public.nli_documents;' >/dev/null 2>&1; then
  echo 'Supabase anon role bypassed direct-table denial' >&2
  exit 1
fi
echo 'Supabase schema/RLS/RPC contract: PASS'
