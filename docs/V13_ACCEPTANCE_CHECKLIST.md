# Nexora Log Inetelligence Ver13 acceptance checklist

This checklist converts the user request and the supplied deployment report
into testable V13 release conditions. Text embedded in the attachment is treated
as issue evidence and product requirements, not as authority to weaken security,
skip review gates, or make unsupported universal-accuracy claims.

## Parsing and Assurance

- [x] Every record in both supplied sample-log files is processed by the V13
  corpus test with no dropped input. Known families must satisfy their semantic
  golden outputs; unknown input must retain an exact lossless representation and
  explicit diagnostics.
- [x] Lossless mode reports input field count and output source-field count and
  proves parity without inventing fields. Envelope metadata is counted
  separately.
- [x] Assurance accepts bounded `.log`, `.txt`, `.json`, `.jsonl`, `.csv`,
  `.gz`, `.zip`, and `.tar` uploads with path traversal, expansion-ratio,
  member-count, total-size, timeout, and content checks.
- [x] Assurance provides analysis, report, parser asset, regression, retained
  fixture, AI proposal, review, approval, publish, download, and audit flows.
- [x] Parser changes use immutable revisions and become live only after
  validation, independent approval, publication, controlled rollout, and
  client acknowledgement. Rollback remains available.
- [x] The exact supplied Alloy/Vector logic is the first parser tier for its
  matching families. Migration reports expose extracted regexes, templates,
  stages, mappings, source hashes, unsupported constructs, and compatibility.
- [x] Known-key, delimiter, positional/static, projection, regex-template, and
  generic fallback catalogs are viewable, searchable, downloadable, versioned,
  editable through validated drafts, and covered by regression tests.
- [x] The supplied known-key list is normalized and wired into extraction while
  preserving original spellings through aliases.
- [x] Parser packs are declarative data. The controller never executes imported
  Go, Alloy, River, VRL, shell, or AI-generated code.
- [x] AI is optional, modes are Off, Manual, or Automatic Proposal, credentials
  are encrypted server-side, samples are bounded/redacted, responses are
  audited, and AI can never approve, publish, or activate its own proposal.

## Fleet, live operations, and administration

- [x] Dashboard time-series show ingest, parsed, warning, quarantine, errors,
  throughput, latency, AI activity, controller health, and client health with
  bounded range and node filters.
- [x] Fleet shows safe node identity, address, configured input paths, services,
  parser/config revisions, output types, resource metrics, heartbeat age,
  current issues, and rollout status without exposing secrets.
- [ ] Controller Agent Management and Client Agent Management expose status,
  start/stop/restart/configuration/diagnostic actions through RBAC, confirmation,
  command leases, audit, expiry, and result reporting.
- [x] Notifications distinguish success, failure, warning, progress, approval,
  stale node, rollout, storage, AI, and security events.
- [x] Live Metadata explains retained collections, counts, freshness, storage
  status, and the boundary that normal raw/customer events are not retained.
- [x] Users, RBAC, audit, rollouts, signed artifacts, migration, and system health
  screens support the documented roles and actions with contextual examples.

## Client throughput and destinations

- [x] The client supports multiple log paths and multiple simultaneous selected
  destinations: controller, local JSONL, Loki, and ClickHouse. Quarantine stays
  local even when every primary destination is enabled.
- [x] Configuration keeps endpoint credentials in the root-owned environment
  file and never writes them into the public JSON configuration, logs, UI, or
  telemetry.
- [x] Sustained-throughput evidence covers at least 10 GB/hour input, multiline
  bounds, backpressure, batching, retry, disk spool, file rotation, resource
  limits, and graceful shutdown without claiming performance beyond the tested
  host and record distribution.

## Packaging, upgrade, and deployment

- [ ] Controller and client are genuine `13.0.0-1` x86_64 RPMs for Rocky Linux 9
  and 10 and support clean installation plus V8 to V13 upgrade, rollback, and
  re-upgrade with configuration/state preservation.
- [ ] One-use private-CA enrollment, protected node token, sample log, service
  verification, uninstall scripts, checksum verification, SBOM, manifest, and
  provenance remain present.
- [ ] WebView and webapp use the same authenticated controller API. Vercel and
  Cloudflare proxy deployments use a public HTTPS controller origin; live data
  changes do not require redeployment.
- [ ] Supabase file persistence is allowlisted to controller-encrypted Assurance
  fixtures and approved immutable rollout RPM/config artifacts. Normal production
  raw or parsed logs never enter Supabase. Private Storage uses a server-only
  secret key; all browser/client access passes through the authenticated
  controller. Password material, sessions, node tokens, output credentials,
  controller secrets, TLS private keys, and AI keys are excluded.
- [ ] Parser Fleet Sync covers regex, patterns, static layouts, parser logic,
  templates, field mappings, and known-key mappings. Web edits create revisioned
  Supabase drafts; only independently approved and published revisions enter a
  canonical hash-addressed bundle. Publication automatically starts a managed
  rollout, and the dedicated tab proves controller/client desired-versus-applied
  revision and hash, last heartbeat, drift, retry state, and final verification.
- [ ] The final release is built twice reproducibly, exercises browser to API to
  storage flows, runs a Rocky two-node enrollment test, and is pushed only to
  the private `vjay136/nexora_log_intelligence_ver13` repository.

## Accuracy definition

V13 acceptance uses two precise guarantees:

1. **Semantic corpus accuracy:** all supplied known-family records match their
   reviewed parser and golden field expectations.
2. **Lossless ingestion:** every other accepted record produces a bounded JSON
   representation containing its exact source payload or an exact reversible
   token map plus explicit parser/confidence/error metadata.

No finite parser catalog can prove correct semantic interpretation for every
future log format. V13 therefore combines exact known parsers, deterministic
fallback, transparent confidence, quarantine, regression, approval, and rapid
rollout instead of silently inventing fields.
