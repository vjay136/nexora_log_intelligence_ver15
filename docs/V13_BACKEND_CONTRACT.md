# Nexora Log Inetelligence Ver13 backend contract

## Identity and parser execution

The Go module is `nexora.local/nli-v13`; the runtime reports `13.0.0`, and the controller health header is `X-NLI-Controller: v13`.

The parsing priority is:

1. The reviewed family ParserPacks in `configs/parsers/` run first. These assets encode the migrated IMAP, POP, WEBTOP/WEBMAIL, MTA, PSS/PABD, MSS/CAPD, SMTP-IN and SMTP-OUT behavior. Their static header stage is followed by an allowlisted mixed key/value stage.
2. Mixed key/value extraction recognizes configured keys and configured separators, aliases, quote and escape rules, and duplicate policies. New field names belong in the Known Keys catalog; they do not require new generated regexes.
3. Named static layouts, regex, Grok, logfmt, JSON and templates can describe additional formats. Regex uses Go RE2 and is size bounded.
4. Opt-in semantic extraction can enrich a parsed event with IPv4, IPv6, domain, URL and email entities and can safely decode RFC 2047 encoded MIME headers.
5. Drain templates and generic structured-format parsing are bounded fallbacks. Unknown, ambiguous and unsupported input remains observable and can be quarantined.

Lossless rules use `projection_mode: all` and `count_policy: exact_source_fields`. Intentional field selection uses `allowlist` or `denylist` with `count_policy: projection`. Derived output fields require `count_policy: allow_derived`. This keeps source-field counts auditable.

`configs/parser-catalog/nli-v13-parser-catalog.json` records the tiers, sample-family key inventory, parser asset IDs, extracted source inventory, and the scope of the sample evidence. The supplied corpus contains 728 records: 678 records across eight known families from the pinned archive plus 50 records from the additional standalone sample. Migration tests execute the 678 known-family records against reviewed profiles, and the additional-sample tests account for all 50 remaining records and their provenance. The tests do not establish a universal accuracy guarantee for arbitrary unseen logs.

## Parser asset APIs

All endpoints require an authenticated session. Read operations require parser-read access; create, edit and delete operations require parser-management access and CSRF protection.

| Method and path | Contract |
| --- | --- |
| `GET /api/v1/config/{kv,static-patterns,projections,templates,regex,known-keys,semantic-extraction}` | List assets: `{ "items": [documentView...], "count": n }` |
| `POST /api/v1/config/{collection}` | Create a draft: `{ "id": "...", "name": "...", "config": { ... } }` |
| `GET /api/v1/config/{collection}/{id}` | View the current revision |
| `PUT /api/v1/config/{collection}/{id}` | Replace a draft with a new immutable revision; body is `{ "name": "...", "config": { ... } }` |
| `GET /api/v1/config/{collection}/{id}/download` | Download the JSON payload as an attachment |
| `DELETE /api/v1/config/{collection}/{id}` | Soft-delete the asset |

`collection` is one of `kv`, `static-patterns`, `projections`, `templates`, `regex`, `known-keys`, or `semantic-extraction`. Regex assets require `config.pattern`, compile as RE2, and have a 4096-byte maximum. Known-key assets require `config.keys`, with 1–10000 unique identifier-like keys; callers may also save family, separators, aliases, and duplicate handling in the declarative config. Semantic extraction assets require a unique `config.entities` array selected from `ipv4`, `ipv6`, `domain`, `url`, and `email`; optional `decode_mime_headers` must be boolean and `max_input_bytes` must be at most 1 MiB. Executable/script/secret fields are rejected. Assets are draft metadata and are not active parsers until translated into a ParserPack and passed through the parser review/activation lifecycle.

Semantic extraction is bounded enrichment. It follows [RFC 2047](https://www.rfc-editor.org/rfc/rfc2047) encoded-word Q/B decoding and [RFC 5322](https://www.rfc-editor.org/rfc/rfc5322) MIME header unfolding. URL host parsing follows [RFC 3986](https://www.rfc-editor.org/rfc/rfc3986); IPv6 output uses canonical form from [RFC 5952](https://www.rfc-editor.org/rfc/rfc5952). Values that exceed configured byte limits are rejected or left unchanged with diagnostics. The parser may detect encrypted-content markers but cannot decrypt arbitrary encrypted mail without customer-provided keys and a supported format; no such decryption is implied.

ParserPack endpoints remain the authoritative release surface: `GET/POST /api/v1/parser-packs`, `GET/PUT/DELETE /api/v1/parser-packs/{id}`, and `POST /api/v1/parser-packs/{id}/{submit,approve,reject,activate}`. The creator cannot approve their own pack. Migration import and extraction reports are available at `/api/v1/migration/import` and `/api/v1/migration/reports[/{id}]`.

## AI proposal memory and review

`POST /api/v1/assurance/ai-proposal` requires deterministic assurance to fail and an AI provider to be configured. OpenAI, Anthropic, and custom OpenAI-compatible protocols are adapted by the controller. A successful response includes an `id` and a saved draft. `GET /api/v1/assurance/ai-proposals` lists saved records; `GET /api/v1/assurance/ai-proposals/{id}` reads one; `POST /api/v1/assurance/ai-proposals/{id}/approve` requires parser-review permission and a reviewer other than the creator. Proposal create/approval and provider status emit masked WebSocket notifications; a hosted WebApp reconciles the same state through bounded polling.

Proposal memory stores the declarative proposal, bounded failure diagnosis, suggested method, and SHA-256 of the source input. It omits examples, tests and regression inputs and never stores the prompt's raw log line. Approval records reviewer and state only; it does not create, publish or activate a parser. The WebApp requires the matching failed sample to be rerun before it can create and deterministically validate a versioned parser draft. That draft must still pass its cases and the normal submit, independent approval, publication and fleet rollout lifecycle.

## Assurance sample upload and retention

`POST /api/v1/assurance/upload` requires an authenticated session with parse-preview permission and a CSRF token. Multipart accepts exactly one `file`, optional `service` (defaults to `auto`) and optional `retain` (`false` by default). It accepts `.log`, `.txt`, `.json`, `.jsonl`, `.csv`, `.gz`, `.zip` and `.tar`. JSONL requires one valid JSON object per nonblank line; CSV requires unique identifier-like headers and produces one JSON record per row.

Upload bounds: compressed request file is at most 4 MiB; expanded content at most 8 MiB total, 32 regular members, 200:1 expansion ratio, 1000 records total, 1 MiB per line/record, and a 10-second processing deadline. Retention is separately authorized with Assurance-save permission and disabled unless encryption retention is configured. Every retained sample (uploaded or pasted in Assurance Lab) is AES-GCM encrypted by the controller. With Supabase, only ciphertext bytes go to the private `nli-assurance-fixtures` bucket and the protected metadata RPC receives a descriptor; local metadata mode stores ciphertext in its local store. The descriptor includes sanitized filename, media/archive type, compressed/expanded size, original-content SHA-256, service, parse summary, actor, revision and audit timestamps. Authenticated `/api/v1/assurance/cases/{id}` retrieval reads the private object through the controller and decrypts locally; purge and expiry remove the object. With `retain=false`, upload content is not persisted; ordinary logs remain outside metadata and Storage.

## Private Supabase file storage

Supabase metadata continues to use the publishable/anon project key plus the controller-specific `NLI_SUPABASE_CONTROLLER_SECRET` over protected RPCs. Only approved rollout RPM and JSON configuration files use the separate server-only `SUPABASE_SECRET_KEY` (`sb_secret_...`) for the Supabase Storage API. Parser definitions remain revisioned control metadata and cannot be uploaded as rollout files. The setup script writes the secret key only to the root-owned controller environment file. Never provide it to a web app, Vercel/browser environment, an agent, or any client. If it is unset, the controller can start and reports `artifact_storage.ready=false`; Supabase-backed artifact upload fails closed with HTTP 503.

The schema provisions private `nli-assurance-fixtures`, `nli-rollout-artifacts`, and `nli-geoip-databases` buckets (no anon/authenticated Storage object policies). Assurance paths accept only bounded `.enc` objects with `application/octet-stream`. Rollout objects accept only the `rpm/` or `config/` purpose prefixes with SHA-256 in the path, `x-upsert:false`, verified signature, hash, size and MIME metadata; parser assets are rejected by file artifact endpoints and stay in revisioned metadata APIs. The private controller download route checks RBAC/node authorization and streams the object. Local metadata mode retains its explicitly local immutable file behavior. Ordinary production raw logs, parsed events, metrics, client spools, and migration/parser data are never permitted in these buckets. The GeoIP bucket accepts only the three fixed GeoLite2 MMDB names under SHA-256 paths, bounded to 128 MiB per object; uploading requires explicit administrator license acceptance and remains pending separate approval. No public URL or browser-direct object listing/upload is exposed.

Objects up to 6 MiB use the standard object API. Larger approved artifacts use bounded 6 MiB TUS chunks through the direct Storage hostname; every returned upload location is required to remain same-origin HTTPS before the controller sends its secret key. A failed or interrupted upload returns an error and is never recorded as a validated stored artifact.

## Live metrics

`GET /api/v1/metrics/timeseries?range=1h&step=1m&node_id={optional}` requires telemetry-read permission. `range` is one of `1h`, `6h`, `24h`, `7d`, `30d`; `step` is one of `1m`, `5m`, `15m`, `1h`, `6h`, `1d`. The server rejects unknown values and requests that would exceed 1000 buckets.

Response schema:

```json
{
  "schema_version": "1",
  "range": "1h",
  "step": "1m",
  "from": "RFC3339 timestamp",
  "to": "RFC3339 timestamp",
  "node_id": "",
  "points": [{
    "timestamp": "RFC3339 timestamp",
    "logs_received": 0,
    "logs_parsed": 0,
    "logs_quarantined": 0,
    "errors": 0,
    "parse_latency_ms_avg": 0,
    "parse_rate_pct": 0,
    "ai_requests": 0,
    "ai_success": 0,
    "ai_failure": 0,
    "ai_latency_ms_avg": 0
  }],
  "fleet": { "stats": {}, "clients": [] },
  "system": {
    "controller_cpu_count": 0,
    "controller_memory_alloc_bytes": 0,
    "controller_memory_sys_bytes": 0,
    "controller_goroutines": 0,
    "client_cpu_percent_avg": null,
    "client_memory_percent_avg": null,
    "client_load_1m_avg": null,
    "availability": {}
  },
  "retention": {
    "raw_logs": false,
    "event_payloads": false,
    "secrets": false,
    "series_source": "bounded_parser_metadata_and_latest_heartbeat"
  }
}
```

`fleet.clients` contains one latest heartbeat sample per reporting client: `node_id`, `hostname`, `updated_at`, `cpu_percent`, `memory_bytes`, `memory_percent`, `load_1m`, `load_5m`, `load_15m` and `parse_latency_ms`. These host metrics are optional heartbeat fields and remain unavailable until a client reports them. Controller CPU percentage and load are marked unavailable; controller memory and goroutine count come from Go runtime statistics. Parse series uses the event metadata retention window. AI series uses a bounded in-memory history of at most 4096 proposal calls and resets when the controller restarts. No metric includes raw event payloads, prompts, credentials or secrets.

## Client management and outputs

The existing node API remains the management contract: `POST /api/v1/node/register`, `POST /api/v1/node/heartbeat`, `GET /api/v1/node/commands/next?node_id=...`, `POST /api/v1/node/commands/{id}/result`, and `POST /api/v1/node/events`. Heartbeats can report counters and optional host-resource gauges. Command payloads are declarative and versioned; parser updates use signed/approved artifacts and config updates use the existing bounded config validation.

Agent output fan-out is configured with the `outputs` array. Each output is an independent route; telemetry has a separate `telemetry_outputs` array. The output types and their fields are defined by `internal/output.Config`. The controller metadata store stores parser and configuration documents; ordinary customer log/event payloads are not retained there.

## Input path profiles

`/api/v1/config/input-paths` uses the same authenticated, revisioned configuration document lifecycle as the parser configuration collections. A profile's `config.inputs` is an array of 1–100 agent `InputConfig` entries. Each entry uses the client contract fields `path` (absolute path) and `service` (one of `auto`, `imap`, `pop`, `webtop-webmail`, `webtop`, `mta`, `smtp-in`, `smtp-out`, `pss`, `pabd`, or `capd`), with optional `selection`, `multiline`, and `mask` objects. Paths must be unique within a profile. Edits create revisions; deletion is a soft delete through the generic config resource route.

The WebApp rollout form reads a saved profile and submits a `config_push` proposal through `/api/v1/rollouts` with explicit `target_nodes`. The proposal carries `config_patch`, so the client merges only the selected top-level keys and preserves enrollment, telemetry, parser, enrichment, and unrelated output settings. The merged file is validated before atomic activation and restart; a failed restart restores the backup. Independent reviewer approval is required before delivery. File outputs accept either `path` or an absolute `path_template` containing exactly one `{hour_utc}` token. The runtime resolves this token as `YYYYMMDD_HH00` in UTC and changes files at the hour boundary while retaining size rotation.
