# Changelog

## [14.0.0-1] - 2026-10-17

- Added a noninteractive `--automatic` controller path with stable root-owned
  credentials, IMDSv2-aware AWS identity discovery, complete certificate SANs,
  listener/TLS/firewall diagnostics, and safe credential recovery on upgrade.
- Added client `--all-log-files` provisioning for every reviewed parser family,
  including family-specific inputs and hourly normalized/quarantine outputs.
- Reorganized the WebView by operator task and added an original eagle-shield
  identity with a reduced-motion-safe animated sign-in experience.
- Rebuilt the operational dashboard around one bounded aggregate request with
  live ingest, parse, quarantine, error, latency, route, pipeline-stage,
  backpressure, client-resource, and parser-family measurements.
- Added a complete Parser Creation Workspace for trimmed single/multiline
  samples, editable deterministic artifacts, validation, drafts, independent
  approval, publication, rollout, and audit history.
- Added encrypted, write-only OpenAI, Anthropic, and compatible-provider
  configuration. AI proposals run only after an explicit operator action and
  remain drafts until deterministic validation and independent approval.
- Preserved AWS/private-network, corporate certificate, Nginx/ACME, existing
  proxy, Cloudflare Tunnel, Vercel, and Cloudflare Pages deployment paths with
  same-origin authentication and streaming.

## [12.0.0-1] - 2026-10-13

- Made offline controller-local private CA deployment the restricted-network default and added validated existing-server-certificate discovery with safe fallback.
- Added exact service, event, Webtop method, and hostname input filtering with editable default and customer rules.
- Added privacy-preserving email/network buckets and optional exact-path MaxMind MMDB enrichment.
- Split the console into Input Parser Engine and Output Parser Engine workspaces.
- Added bounded ClickHouse field-column mapping while retaining the complete typed event JSON.
- Preserved normal systemd status metadata while removing only external GitHub documentation links.

## [10.0.0-1] - 2026-10-13

- Added guided public controller exposure through Nginx with ACME, Cloudflare Tunnel, or an existing HTTPS reverse proxy while keeping the controller on loopback.
- Added the API Monitor workspace and reorganized Assurance guidance around test, retain, draft, review, publish, fleet verification, and replay.
- Added exact event/service/hostname input selection with `default`, `filtered`, and `hybrid` modes, reviewed presets, automatic parser/path provisioning, and tail-from-end for existing production logs.
- Added an exact-raw durable client quarantine queue, local encrypted controller retention, authenticated downloads, targeted replay, per-record results, and replay history.
- Fixed client node identity permissions so both managed services can read `install.id` after fresh install or upgrade.
- Added parser catalog documentation and automatic self-tests for every bundled reviewed parser family.

## [10.0.0-4] - 2026-10-10

- Added one versioned release archive for clean-directory transfer to Rocky Linux nodes, with an external archive checksum and a complete internal release manifest.
- Added `RELEASE_ID` and made both installers require its exact version, release, architecture, and source revision before installation.
- Made installers verify every checksum-listed bundle file and require the RPM release to match the bundle release exactly, preventing mixed V10 releases.
- Fixed the release evidence unit fixture so GitHub Actions includes the required sample-field accounting artifact.
- Fixed a controller-readiness race that could clear a password entered while the initial anonymous session probe completed.
- Rewired managed clients to send authenticated, private-CA parser telemetry to the controller while keeping parsed customer fields in selected local, Loki, or ClickHouse outputs.
- Unified bounded telemetry metadata across Live Metadata, overview, and time-series APIs and included the current interval immediately.

## [10.0.0-3] - 2026-10-10

- Fixed the guided controller and client installers so valid RPM release values with Rocky Linux distribution suffixes, including `2.el9` and `3.el9`, pass validation.
- Added `--verify-only` to both installers and made the release artifact gate execute those exact operator validation paths before publication.
- Kept V10 package identity, exact `10.0.0` version, x86_64 architecture, and SHA-256 bundle checks fail closed.

## [10.0.0-2] - 2026-10-09

- Added deterministic field-accounting evidence for all 728 supplied records: 723 valid records preserve exact `13,101 = 13,101` raw-field-to-generated-JSON parity, while five fixed malformed or truncated specimens are explicitly hash-pinned to the expected quarantine set.
- Verified all 278 declared known keys and added byte-level provenance for all 16 supplied Alloy/Vector/VRL parser sources, including the documented minimal repair applied to the SMTP-OUT source.
- Added authenticated Field Integrity & Corpus Proof, Unknown & Quarantine, Template Drift, and Performance & Backpressure workspaces, bringing the live WebView to 29 tabs.
- Added the corpus proof API and embedded release evidence, including RPM/web release validation of `SAMPLE_FIELD_ACCOUNTING.json`.
- Decoded bounded RFC 2047 Q/B content when semantic extraction reads a previously parsed field, with focused IPv4, IPv6, domain, URL, email, encoded-subject, and telemetry tests.
- Kept unknown and structurally invalid input fail closed; V10 does not fabricate fields or claim universal semantic accuracy for future unseen formats.

## [10.0.0-1] - 2026-10-09

- Added a corpus-derived, tiered V10 parser catalog with editable/downloadable known-key, delimiter, static-layout, projection, regex, template, and semantic-extraction assets.
- Added bounded IPv4, IPv6, domain, URL, email-address, and RFC 2047 encoded-Subject extraction as separately named parser capabilities.
- Added safe Assurance file/archive upload, encrypted opt-in retention, persistent AI proposal memory, independent approval, and complete trace/parity reporting.
- Added 25 live WebView workspaces, selectable time-series dashboards, detailed fleet/controller/client management, migration inspection, and parser asset studios.
- Added simultaneous controller, JSONL, Loki, and ClickHouse client outputs with independent batching, retries, and failure receipts.
- Restricted Supabase file persistence to retained Assurance fixtures and approved rollout RPM/config artifacts; normal production logs remain outside Supabase.
- Added V10 controller/client RPM packaging, V8 upgrade coverage, guided install/uninstall scripts, an 8K architecture diagram, and content-hash cached verification evidence.

## [7.0.0-1] - 2026-10-07

- Added the V7 Assurance Lab with scoring, coverage, parser candidate validation, regression history, and approval-gated publication.
- Added encrypted server-side configuration for optional OpenAI-compatible parser proposals.
- Added live user metadata mirroring to Supabase with credential exclusion and retryable partial-sync handling.
- Added one-run Rocky Linux controller/client enrollment, firewalld configuration, sample-log processing, and verified heartbeat startup.
- Added V7 product branding, notification center, repaired live-data workspaces, authenticated downloads, and Vercel/Cloudflare proxies.
- Added V7 RPM, WebView, webapp, provenance, SBOM, checksum, and reproducible release packaging.

## [6.0.0-2] - 2026-10-07

### Added
- Single-command controller and client installers that validate release RPMs, guide enrollment, and verify active services.
- Interactive client input/output configuration for one or more real log paths and controller, file, Loki, or ClickHouse delivery.
- Reconfiguration mode for changing client paths or destinations without reinstalling or re-enrolling.
- Separate safe uninstallers with preserved-data defaults and explicit purge confirmation.
- Three-node VMware/MobaXterm deployment runbook and Vercel live-sync guidance.

### Fixed
- Release staging normalizes Linux scripts, configuration, and systemd units to LF so Windows-hosted builds execute correctly on Rocky Linux.

## [6.0.0-1] - 2026-10-07

### Added
- Supabase PostgREST storage protected by RLS and an installation-specific controller secret.
- Production ParserPack execution and trace evidence in Assurance Lab, with encrypted manual-retention fixtures and authenticated raw-log downloads.
- Vercel-ready same-origin web application and bounded controller proxy, plus responsive tests for all 14 workspaces.
- Authenticated Loki, ClickHouse, controller, file, and stdout destinations with retry, custom-CA, multi-tenant, and delivery-accounting support.
- Reproducible client/controller RPM pipeline, web artifacts, SBOM, provenance, checksums, and Rocky lifecycle validation.

### Fixed
- Input cursors now advance only after complete multiline records reach acknowledged destinations.
- Supabase publishable keys are sent using their supported API-key semantics without invalid bearer authentication.
- Assurance inputs and results survive background reconciliation until an operator clears them.
- Fleet, parser catalog, and migration workspaces render actionable empty states instead of blank panels.
- Single-use enrollment replay, proxy authentication forwarding, TLS onboarding, and disabled-retention health behavior remain fail closed.

## [0.4.0] - 2026-10-02

### Added
- **13-stage layered parse pipeline**: multiline → format detect → timestamp extract → variable mask → Drain template mine → deterministic parse → AI discovery → field extract → type infer → schema validate → confidence → output route
- **Multiline assembler** (`internal/multiline`): Fluent Bit-inspired state machine with Java/Python/Go stack trace rules, flush_timeout (4s default), 2MB buffer limit with truncation flag
- **Format detector** (`internal/detector`): JSON, syslog RFC3164, syslog RFC5424, logfmt, Apache CLF, nginx, CEF, SMTP native, kv-pairs, plain (short-circuit detection)
- **Timestamp extractor** (`internal/timestamp`): 8 format variants (ISO 8601 variants, Apache CLF, syslog RFC3164, unix epoch, epoch ms, SMTP native), all normalized to RFC3339Nano UTC
- **Variable masker** (`internal/masker`): IPv4, IPv6, email, UUID, URL, hex IDs, integers, paths, secret fields; applied before template mining and on output fields; configurable per-input
- **Drain3-style template miner** (`internal/drain`): Fixed-depth prefix tree (ICWS 2017 algorithm), configurable sim_th/depth/max_children/max_clusters with LRU eviction, JSON persistence, thread-safe with RWMutex
- **Grok-style pattern library** (`internal/grok`): 20+ named patterns (USERNAME, IPV4, HOSTNAME, URI, EMAILADDRESS, UUID, TIMESTAMP_ISO8601, SYSLOGBASE, COMMONAPACHELOG, etc.), recursive substitution with cycle detection, pure Go, no GPL
- **Parser registry** (`internal/registry`): Declarative ParserDef JSON DSL (never executable code), loadable from directory, regression tests
- **16 built-in parsers**: `generic.json`, `generic.kv`, `generic.logfmt`, `syslog.rfc3164`, `syslog.rfc5424`, `web.apache_clf`, `web.nginx_access`, `web.nginx_error`, `smtp.dmarc.aggregate` (v4), `smtp.postfix`, `smtp.imap`, `security.sshd`, `security.sudo`, `kernel.audit`, `docker.json`, `cef.generic`
- **AI parser discovery** (`internal/ai`): Triggered only when cluster reaches min_cluster_size (default 10), batches up to 20 clusters per call, AI output constrained to ParserDef JSON only, regression test + quarantine on failure; supports ollama/openai/anthropic/none
- **Loki push output** (`internal/output/loki.go`): Batch HTTP push to `/loki/api/v1/push`, no external SDK
- **ClickHouse output** stub (`internal/output/output.go`): Uses `clickhouse-go/v2` (Apache-2.0)
- **Output router** (`internal/output`): Multiple destinations per run, per-route filtering (normal/warning/quarantine/unknown)
- **nli-manager** (`cmd/nli-manager`): HTTP REST API for multi-client management, client heartbeat registry (90s timeout), web UI served from `/usr/share/nli/web`
- **nli-bench** (`cmd/nli-bench`): Benchmark with 14 metrics, `run` and `compare` commands, BENCHMARK.md output
- **Replay mode** (`nli replay`): Re-parses quarantined logs against updated parser registry
- **Config v2 schema**: New fields: `outputs[]`, `drain{}`, `ai{}`, `replay{}`; backward-compatible v1 auto-migration

### Changed
- `engine.Version` = `"0.4.0"`
- Config schema bumped to version 2 (v1 configs auto-migrated with warning)
- `engine.Parse()` now accepts `*Deps` to inject pipeline components
- RPM bumped to `Version: 0.4.0`
- 4 binaries now packaged: `nli`, `nli-agent`, `nli-manager`, `nli-bench`
- 2 systemd units: `nli-agent.service`, `nli-manager.service`

### Fixed
- **Root cause of parsing failures**: All non-DMARC structured logs were falling to `_unknown` because `engine.go` had only 3 parsers. v4 adds format detection covering 10 formats and 16 built-in parsers.

## [0.1.0] - 2026-10-01

- First standalone deterministic test agent and CLI package
- SMTP DMARC aggregate parser, JSON parser, key=value parser
- Systemd service, cursor/checkpoint file tailer, metrics endpoint
# 12.0.0-1 — 2026-10-13

- Make offline controller-local private CA deployment the restricted-network default and add validated existing-server-certificate discovery with safe fallback.
- Add exact service, event, Webtop method, and hostname input filtering with editable default and customer rules.
- Add privacy-preserving email/network buckets and optional exact-path MaxMind MMDB enrichment.
- Split the console into Input Parser Engine and Output Parser Engine workspaces.
- Add bounded ClickHouse field-column mapping while retaining the complete typed event JSON.
- Preserve normal systemd status metadata while removing only external GitHub documentation links.

