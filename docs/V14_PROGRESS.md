# V14 engineering status

V14 implements the controller, client, WebView, deployable WebApp, Supabase metadata plane, reviewed parser corpus, and approval-gated fleet configuration lifecycle in this source tree.

## Data path

Clients tail configured family files and apply exact service, event, method, and hostname gates before parsing. Reviewed parser packs and automatic detection feed deterministic extraction and enrichment. Validated records can be delivered simultaneously to Loki, ClickHouse, the controller, and optional hourly local normalised files. Exact raw failures remain in mandatory hourly quarantine storage for diagnosis and bounded replay.

## Control path

Input-path and endpoint profiles are revisioned resources. Publishing creates a proposal; an authorized reviewer approves it; rollout targets explicit node IDs. Clients merge a bounded top-level `config_patch` into the existing configuration, validate the full result, write atomically, restart, report applied state, and roll back on failure. Parser asset publication follows the separate signed/hash-addressed parser bundle path.

## Operator path

The WebApp provides dropdowns populated from live parser-family and corpus catalogs while retaining validated custom-entry paths. Assurance uses the production parser adapter. Unknown & Quarantine exposes exact-raw inspection and download, parser diagnosis links, retry history, bounded replay, and dead-letter state. AI providers are optional and can only propose declarative draft parsers; they cannot activate production changes.

## Verification model

The release process runs uncached Go tests and vet, Web/API tests, browser tests, installer/static checks, Supabase security-contract tests, reproducible two-pass binaries/RPMs, Rocky Linux 9 and 10 install/upgrade/uninstall lifecycles, two-node TLS/enrollment tests, corpus accounting, checksums, SBOM generation, and artifact verification. Results are recorded in the generated release manifest and final verification report for the frozen source revision.

## Current measured evidence

- The uncached Go format, unit, vet, full race, and mixed-KV fuzz gate passed. The fuzz run executed 154,670 inputs without a crash.
- The real-controller browser flow passed all 6 scenarios and visits every one of the 33 authenticated workspaces, including Live Streams and Input Log Paths.
- The mocked browser regression suite passed all 28/28 scenarios from the final source, including the aggregate live dashboard and Parser Creation Workspace.
- The supplied corpus accounts for 728/728 inputs: 723 valid records have exact source-field parity, five hash-pinned malformed or truncated records reach expected quarantine, and 278/278 known family keys are covered.
- The production agent processed and durably checkpointed 5,368,709,120 bytes and 1,310,720 records in 466.717 seconds on the recorded six-logical-CPU Linux/amd64 Docker host. The report is `docs/evidence/THROUGHPUT_5G.json`.

Release packaging, Rocky lifecycle acceptance, private GitHub publication, and remote asset verification are recorded against the immutable commit in the generated release evidence and GitHub workflow results.
