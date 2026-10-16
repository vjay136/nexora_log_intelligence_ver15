# NLI v5 Production Implementation Specification

Status: implementation contract
Target version: 5.0.0
Repository: `C:\Users\Vjay\.kiro\crew\workspace\nli-v4`
Source corpus: `C:\Users\Vjay\.kiro\crew\uploads\6dfa218758fa44debf9489050b09b503_all_alloy_and_vector_parsers.zip` plus the 16 individually uploaded parser files

## 1. Binding outcome

Deliver and validate:

1. `nli_client_ver5.rpm` (also emit compatibility alias `nli_clinet_ver5.rpm`)
2. `nli_controller_agent_ver5.rpm`
3. `webview_ver5.zip`, with the same web assets embedded/included in the controller RPM
4. SHA-256 manifest, SBOM, test report, migration report, and install/runbook

Do not claim universal 100% semantic accuracy. The enforceable guarantee is: exact golden-case agreement, 100% input-event accounting, explicit uncertainty, no silent field invention, no silent drops, deterministic parser precedence, and per-destination delivery accounting.

## 2. Immediate golden regression (test zero)

This input currently detects `smtp_native` but emits `unknown` and must become an exact passing golden case:

```text
20260822 050048557+0200 severrgo001 smtp 230675 0 140347436861184 Note;SieveRuleMsg(2005/1) id=6A291BF5048F23DE:policy=outbound:checkpoint=AUTH:rule=INOUT_action:msg=PolicyMsgInfo:eventType=PolicyMsgInfo:action=disconnect:reason=badpassword:fromhost=11.22.33.44:geo=IL:helo=Site6:auth_ok=false:auth_user=managers999@012.meg.in:auth_fail=6:auth_tries=7:score=0
```

The parser policy must support a configurable static prefix and configurable mixed-delimiter tail. Expected event payload (no parser metadata and no raw field):

```json
{
  "date": "20260822",
  "time": "050048557+0200",
  "hostname": "severrgo001",
  "service": "smtp",
  "major_pid": "230675",
  "pid_status": 0,
  "minor_pid": "140347436861184",
  "event_type": "Note",
  "event": "SieveRuleMsg",
  "code": "2005/1",
  "id": "6A291BF5048F23DE",
  "policy": "outbound",
  "checkpoint": "AUTH",
  "rule": "INOUT_action",
  "msg": "PolicyMsgInfo",
  "eventType": "PolicyMsgInfo",
  "action": "disconnect",
  "reason": "badpassword",
  "fromhost": "11.22.33.44",
  "geo": "IL",
  "helo": "Site6",
  "auth_ok": false,
  "auth_user": "managers999@012.meg.in",
  "auth_fail": 6,
  "auth_tries": 7,
  "score": 0
}
```

IDs default to strings unless a rule explicitly selects numeric coercion. The normalized timestamp belongs to transport/telemetry metadata unless the output projection explicitly includes it.

## 3. Separate event data from parser telemetry

Replace the v4 all-in-one envelope at output boundaries with two records:

- `EventPayload`: only configured/captured customer fields. Valid JSON is guaranteed by `encoding/json`; keys are always strings and values use declared types.
- `ParseTelemetry`: event ID/hash, node ID, parser ID/version/policy hash, template ID, attempt count, ordered parser attempts and outcomes, latency, confidence, evidence coverage, ambiguity, input/output field counts, route, delivery acknowledgements, and error codes. It must not contain raw log text or the emitted JSON event.

Normal NLI operation must not retain raw logs or parsed JSON events in controller/client state. Store only bounded aggregate telemetry and hashes. Raw/event retention is allowed only for explicit Assurance Lab submissions under an admin-controlled retention policy, controller-side encryption at rest, audit log, authenticated download, and manual purge endpoint. Browser and client must never receive Supabase keys or the controller-only RPC secret.

Remote Loki/ClickHouse outputs carry `EventPayload`; parser telemetry is exposed separately in the webview and optional metadata sink. Remove raw text from normal controller ingestion. Existing compatibility envelopes may remain behind an explicit legacy-output option disabled by default.

## 4. Versioned parser-policy model

Add a safe declarative `ParserPack` / IR with strict JSON validation and no executable code:

- pack ID, semantic version, revision, SHA-256, source type, source hash, author, review/approval state, created/activated timestamps
- ordered parser rules with unbounded positive integer `priority` (lower executes first); ties sort by rule ID
- service/log-family selectors and include/exclude predicates
- one or more bounded stages: static positional layout, regex named capture, grok, mixed KV, logfmt, JSON, timestamp, replace, rename, type conversion, field projection, conditional match, drop/quarantine, and output mapping
- explicit failure policy per stage: continue, reject rule, quarantine
- resource limits: max input bytes, fields, captures, regex length, stage count, attempts, and execution deadline
- field schemas with type, required/optional, enum/range, sensitivity, preserve-leading-zero, aliases, and collision policy
- output projection modes: all, allowlist, denylist; allowlist must capture only listed fields
- regression cases containing raw input, exact desired payload, expected parser/rule, expected route, and optional expected telemetry assertions
- activation is forbidden until validation and all required regression cases pass

RE2-compatible Go regex only. Reject unsupported backreferences/lookbehind rather than approximating them. Reject duplicate field names, unsafe paths, arbitrary templates/code, secret literals, and pathological sizes.

## 5. Static-layout and mixed-delimiter parsing

Implement first-class UI/API/DSL stages for:

### Static Layouts

Support whitespace tokens, fixed-width slices, regex/grok captures, literals, optional tokens, rest-of-line capture, and transforms such as trim/split/strip-prefix/strip-suffix. The SMTP prefix is configurable as:

- token 0 -> date
- token 1 -> time
- token 2 -> hostname
- token 3 -> service
- token 4 -> major_pid
- token 5 -> pid_status
- token 6 -> minor_pid
- token 7 split `;`, left -> event_type, right parse `event(code)`
- remaining text -> mixed-KV stage

### Delimiters & KV Rules

Support multiple configurable pair separators (`:`, `;`, comma, whitespace, pipe), assignment separators (`=`, `:`), single/double quotes, escapes, nested bracket pairs, empty values, duplicate-key policy, key allow/deny patterns, stop keys, key aliases, and per-key types. A separator counts only at top-level and only when followed by a valid configured key plus assignment separator. Therefore colons in URLs, IPv6, times, messages, and quoted values do not split fields.

Track consumed spans. Every emitted field must point to input evidence or an explicit constant/derived rule. Constants/derived fields are separately counted. Never infer an unconfigured key name.

## 6. Ordered fallback and assurance

For each event:

1. Select eligible rules by service/source predicates.
2. Execute in ascending priority with deterministic tie-breaking.
3. Record every attempted rule and stage outcome in telemetry.
4. Stop only on a rule whose required fields, types, schema, evidence, and exact-count policy pass.
5. Continue through bounded fallbacks when a rule rejects/fails.
6. Quarantine logically (telemetry status only, no retained raw event) when all rules fail unless an explicitly configured remote quarantine destination receives the payload.

Expose counters by node, service, parser, rule, stage, result, fallback depth, and error code. Show selected parser name, version, priority, total attempts, and why earlier candidates failed.

Support count policies:

- `exact_source_fields`: emitted source-backed fields must equal recognized raw fields
- `allow_derived`: source-backed counts must match while declared derived fields are reported separately
- `projection`: output count must equal selected allowed fields found/required

## 7. Alloy/River and Vector migration center

Implement upload/import of Alloy/River, Vector TOML/VRL, and NLI parser packs. Initial supported translations must cover constructs found in the supplied corpus:

- Alloy: `loki.source.file`, `loki.process`, `stage.regex`, `stage.template`, `stage.timestamp`, `stage.labels`, `stage.match`, `stage.replace`, `stage.logfmt`, relabel/write metadata
- Vector: filter/remap transforms, `parse_regex`, `parse_timestamp`, `format_timestamp`, `parse_key_value`, `to_string`, `to_int`, `replace`, `merge`, `del`, conditionals, and null coalescing

Import pipeline: upload -> size/type/hash scan -> parse AST/subset -> normalized IR draft -> compatibility report -> unsupported construct list -> generated golden candidates -> Assurance Lab -> human approval -> versioned parser pack -> canary rollout. Never execute uploaded parser source. Never auto-activate an imported parser. Preserve original source hash and line mappings for audit.

Use all supplied parser families as migration fixtures: IMAP, POP, MTA, SMTP-IN, SMTP-OUT, WEBTOP-WEBMAIL, PABD/PSS, and CAPD/MSS. Build deterministic parity tests from actual examples where available; mark corpus rules without sample logs as unverified rather than claiming parity.

## 8. Controller/client synchronization and rollout

The controller is the authoritative policy catalog. Extend the authenticated protocol with:

- desired and active pack/config/release hashes
- capability negotiation and minimum compatible client version
- stage, validate, activate, rollback commands
- signed manifest/checksum verification
- command idempotency and leases
- per-node staged/active/failed/rolled-back status and error detail
- canary selectors by node/tags/percentage, approval gate, progressive waves, pause/cancel/rollback
- reconnect resynchronization and drift detection

Clients download only controller-authorized immutable artifacts, validate in a staging directory, run local regression/smoke checks, atomically activate, preserve one rollback generation, and report status. No arbitrary shell command execution. RPM installation remains an explicit allowlisted package action with checksum, signature, host/source restrictions, and admin RBAC.

## 9. Webview v5

Replace the monolithic placeholder-prone page with maintainable modules or rigorously separated assets. Every tab must use real API data, show loading/empty/error/stale states, reconnect after stream loss, and periodically reconcile snapshots with stream updates.

Required tabs:

1. Operations Dashboard
2. Live Metadata (no raw/event bodies)
3. Fleet & Sync
4. Parser Catalog & Priority
5. Delimiters & KV Rules
6. Static Layouts
7. Field Projection
8. Migration Center
9. Assurance Lab
10. Releases & Rollouts
11. Metadata Explorer
12. Security & Audit

Assurance Lab is highest priority. It must support single raw input, multiline paste, safe file upload, parser-source upload, selected service/source, exact desired JSON editor, field mapping editor, ordered trace, raw-token/evidence visualization, actual-vs-expected structural diff, count parity, parser/fallback counters, latency, pass/fail gates, save/version/re-run, batch corpus test, and promotion to draft parser rule. Raw and desired/actual JSON may be retained only for explicit Lab cases under configured encrypted retention; provide delete/purge controls and auditing.

Parser forms must allow teams to create regex/grok/static/KV/composite rules, set priorities, choose field capture allowlists/denylists, types, include/exclude predicates, and add exact raw->JSON regression cases.

Release upload UI accepts only configured RPM/config/parser artifact types, enforces size limits/checksums/signatures/RBAC, stores immutable metadata, supports specific-node canary then progressive rollout, and displays live status. Never accept arbitrary executable uploads or run user-provided commands.

## 10. Storage and Supabase

Store interfaces keep deterministic local test support while production uses a Supabase-backed controller adapter for:

- parser-pack metadata and revisions
- desired/active rollout state
- node synchronization metadata and aggregate counters
- explicitly retained encrypted Assurance Lab cases/results
- migration reports, configuration revisions, and signed artifact metadata

Do not store normal raw logs or normal parsed JSON events in Supabase. The WebView accesses data only through authenticated controller APIs. Direct `anon` and `authenticated` table access is denied by RLS. Protected PostgREST RPCs require both the public project key and an installation-specific controller secret; Supabase stores only its SHA-256 digest. The secret comes from a root-readable environment file and is never returned through APIs or browser assets. Production must fail closed when Supabase configuration or schema verification is missing rather than silently changing persistence modes.

## 11. Security

- RBAC: viewer, parser_author, reviewer, ops, admin with least privilege
- CSRF protection for cookie-authenticated mutations, secure/samesite cookies, request/body limits, strict content types, security headers
- immutable audit trail for login, parser edits/reviews/activation, uploads, rollout actions, rollback, retention changes, and Lab deletion
- sensitive fields redacted or transformed by policy before remote output
- secrets via environment/vault only
- TLS by default outside isolated lab mode; never bind a placeholder/nonlocal address by default
- no uploaded source execution, no arbitrary shell rollout, no path traversal, no symlink extraction, no ZIP bombs
- regex and parser execution bounded and fuzz-tested

## 12. Version and compatibility

Set engine/module/product version to `5.0.0`; RPM `Version: 5.0.0`, `Release: 1`. Auto-migrate v1/v2 configs without data loss and preserve existing v4 CLI commands. Provide an explicit legacy-envelope compatibility mode, disabled by default. Preserve cursor/checkpoint behavior.

## 13. Validation gates

At minimum:

- tests for every new package and endpoint
- exact golden test for the supplied SMTP line
- tests for mixed delimiters, quotes, escapes, URLs, IPv6, empty values, duplicate keys, nested brackets, delimiter collisions, Unicode, malformed input, max sizes, and parser priority/fallback counts
- exact event-field count and no-invented-field tests
- allowlist/denylist projection tests
- telemetry/raw separation and no-normal-retention tests
- migration fixtures and compatibility reports for every supplied parser family
- controller/client stage/activate/rollback/drift/reconnect tests
- authorization, CSRF, upload validation, signature/checksum, and audit tests
- real-time web reconnect and tab API tests
- `go test ./...`, race tests for core parser/sync packages, vet, frontend checks, and build
- benchmark with throughput, p50/p95/p99, allocations, fallback depths, exact golden accuracy, field precision/recall, record accounting, and migration parity
- clean Rocky Linux 9 and 10 container install/upgrade/service/API/UI smoke tests
- RPM payload inspection confirms all binaries, services, configs, parsers, and web assets
- browser-rendered UI verification, including every tab and Assurance Lab workflow

A release cannot pass if any golden expected field differs, any unconfigured output field is emitted, normal raw/event content is retained by NLI, real-time tabs use placeholders, parser attempts are not counted, or rollout status cannot prove desired hash equals active hash.

## 14. Execution sequence

1. Establish baseline and protect existing uncommitted v4 work on a v5 feature branch.
2. Add the failing SMTP golden case and parser-policy model tests.
3. Implement IR, static layout, mixed KV, projection, priorities, and telemetry split.
4. Build migration parsers and corpus compatibility reports.
5. Extend controller APIs/store and client sync/rollback protocol.
6. Implement the protected Supabase adapter and RLS/RPC schema assets.
7. Rebuild webview tabs and highest-priority Assurance Lab.
8. Add secure artifact upload and progressive rollout.
9. Run unit/integration/fuzz/race/build/browser tests.
10. Build and clean-install both RPMs on Rocky 9 and Rocky 10; generate release evidence.

Work in small independently validated milestones. Do not push, force-push, tag, or publish. Do not overwrite user changes. Do not mark the release complete until all gates pass; report blocked gates exactly.
