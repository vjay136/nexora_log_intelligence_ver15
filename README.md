# Nexora Log Intelligence Ver14

Nexora Log Intelligence Ver14 is a deterministic, review-gated log parsing and fleet-control platform for x86_64 RHEL/Rocky Linux 8, 9, and 10. It combines a managed client, a central controller, an authenticated WebView/webapp, Supabase-backed control metadata, private file storage for approved workflows, and optional AI-assisted parser proposals.

> Install from the single versioned release archive in a fresh directory. Verify its external `.sha256` file and the complete internal `SHA256SUMS`; never mix loose files from different V14 releases.

## Complete flow and architecture

[![Nexora Log Intelligence Ver14 complete flow and architecture](docs/architecture/NLI_V14_COMPLETE_FLOW_AND_ARCHITECTURE_8K.png)](docs/architecture/NLI_V14_COMPLETE_FLOW_AND_ARCHITECTURE.svg)

The linked SVG provides unlimited zoom. The 8K PNG is included for presentations and offline documentation.

## V14 capabilities

- Reviewed first-tier ParserPacks for IMAP, POP, MTA, SMTP-IN, SMTP-OUT, WebTop/Webmail, PSS/PABD, and MSS/CAPD, derived from the supplied working Alloy/Vector parser sources.
- A versioned catalog for known keys, delimiter rules, static layouts, regex, templates, projections, field mappings, semantic extraction, and bounded generic fallback.
- A dedicated Parser Fleet Sync control plane. Every web-created parser asset is stored immediately as revisioned Supabase metadata in draft state; reviewed and published revisions are materialized into a hash-addressed bundle, rolled out automatically, and verified against the controller and every connected client's reported applied hash.
- Dedicated Network Identity, URL, Email Address, and Encoded Subject engines for IPv4, canonical IPv6, domains, HTTP/HTTPS URLs, addresses, and bounded RFC 2047 Subject decoding.
- Source-field parity modes that distinguish source values, derived values, and envelope metadata, preserving exact evidence instead of inventing fields.
- Assurance Lab uploads for bounded text, JSON, CSV, gzip, ZIP, and TAR samples; live family/parser dropdowns; automatic all-parser detection or exact runtime-parser execution; mixed corpus defaults/overrides; live parser and fleet-sync counts; archive traversal and expansion checks; parser drafts; independent approval; controlled publication; authenticated downloads; and audit history.
- Optional controller-side OpenAI, Anthropic, or custom OpenAI-compatible proposals. A provider is contacted only after it is configured and an authorized operator explicitly requests analysis. The controller sends only a masked sample, stores the diagnosis and bounded declarative parser as a draft, and requires deterministic validation plus independent approval before the normal publish and rollout gates. Credentials stay encrypted on the controller and are never copied to clients.
- Live dashboards for ingest, parsed, quarantine, errors, parse rate, latency, AI activity, fleet resources, controller memory, and client health across bounded time ranges.
- Thirty-three authenticated WebView workspaces, including Live Streams, Input Log Paths, API Monitor, Field Integrity & Corpus Proof, Quarantine & Reprocessing, Template Drift, and Performance & Backpressure views backed by controller APIs rather than browser-only demo data.
- Fleet, controller, and client management with node identity, heartbeat age, service state, configured inputs, selected outputs, parser/config revisions, resource telemetry, command leases, results, and audit.
- Family-isolated parsed JSON and quarantine files for ten default inputs, plus simultaneous fan-out to Grafana Loki and ClickHouse with independent batching, retry, and local failure receipts. Authenticated bounded parser telemetry flows separately to the controller for live dashboards. Exact quarantined source records remain in the durable client queue and may be uploaded only when the operator enables bounded raw retention.
- Pre-parse `default`, `filtered`, and `hybrid` selection by exact event name, service, and hostname, including reviewed presets for ImapDisconnected, AcctBadPswd, auth.loginByToken, MtaRequestingDomainTableUpdate, CARDDAV, and SieveRuleMsg.
- x86_64 client and controller RPMs, one-use private-CA enrollment, fresh installation and V12-to-V14 upgrade acceptance (with genuine V12 RPMs), WebView/webapp archives, uninstall scripts, SBOM, provenance, and reproducible build checks. Historical V8/V10 packages remain accepted by the migration matrix.

## Accuracy contract

V14 treats accuracy as two measurable guarantees:

1. Every supplied known-family corpus record must match its reviewed parser and golden field expectations.
2. Every other accepted record must remain lossless through an exact source payload or reversible token representation with explicit parser, confidence, and diagnostic metadata.

A finite catalog cannot prove the meaning of every future log format. Unknown or ambiguous input is therefore quarantined and made reviewable rather than silently assigned invented semantics.

The pinned V14 verification corpus contains 728 records. All 728 are accounted for: 723 valid records have exact raw-field-to-generated-JSON parity (`13,101 = 13,101`), and five fixed malformed or truncated specimens are hash-pinned to the expected quarantine set. All 278 declared family-scoped known keys are exercised. The release also records byte-level provenance for all 16 supplied Alloy/Vector/VRL source files; 15 are preserved byte-identically and one SMTP-OUT source has a documented one-brace repair. These numbers are evidence for the supplied corpus, not a universal accuracy claim for arbitrary future logs.

The V14 throughput qualification processed an actual 5 GiB generated JSONL workload through four production-agent source lanes, reviewed ParserPack attempt/fallback logic, durable local output, and durable checkpoints in 466.717 seconds (7 minutes 46.717 seconds). It accounted for all 1,310,720 records and 5,368,709,120 bytes with zero invalid output and zero quarantine records on the recorded six-logical-CPU Linux/amd64 Docker host. See [the machine-readable result](docs/evidence/THROUGHPUT_5G.json); this is host-specific evidence rather than a universal performance guarantee.

## Data and security boundaries

Normal production raw logs and parsed customer events are not stored in Supabase. Supabase holds bounded control-plane metadata plus files from only two explicit workflows:

- Assurance fixtures retained by an authorized operator, encrypted by the controller before upload.
- Approved rollout RPM or configuration artifacts, stored immutably under a SHA-256-addressed path with signature, size, provenance, and approval metadata.

Regex definitions, parser patterns and logic, static layouts, templates, field mappings, and known-key mappings are revisioned control-plane metadata rather than general file uploads. Drafts never affect running nodes. After independent approval and publication, the controller builds the canonical parser asset bundle, starts the fleet rollout, and compares desired and applied revision/hash values until every eligible node is aligned or reports a visible error.

File buckets are private. Browsers never receive `SUPABASE_SECRET_KEY`, object paths are not public URLs, and downloads pass through the authenticated controller. Password material, sessions, node tokens, output credentials, TLS private keys, controller secrets, and AI keys are excluded from Supabase metadata.

The publishable key is used only for the protected metadata RPC contract. Server-side private Storage requires a dedicated `sb_secret_...` key in the root-owned controller environment; it must never be committed, sent to Vercel, copied to a client RPM, or placed in a `NEXT_PUBLIC_` variable.

## Installation

On the Rocky/RHEL controller, the lowest-interaction path is:

```bash
sudo bash install-controller-ver14.sh --automatic
```

This discovers private/public AWS and host identities, persists generated secrets in root-owned files, creates TLS material, configures the listener/firewall, and prints concrete diagnostics. Run without `--automatic` to choose private-network TLS, public Nginx/ACME, Cloudflare Tunnel, a corporate certificate, or an existing public HTTPS proxy. The installer creates a root-only, one-use `/root/nli-client-bootstrap.env` and a recoverable root-only `/root/nli-controller-credentials.env`; it does not print secret values to the terminal.

On each client, provision all reviewed families and standard paths with:

```bash
sudo bash install-client-ver14.sh --all-log-files
```

The interactive installer remains available for custom log paths, parser families, event/service/hostname selection, and destinations. Enter `automatic` for both paths and parser families to create and verify one reviewed sample path for every bundled parser. For each additional client, first create a new one-use enrollment file on the controller:

```bash
sudo nli-controller-setup --rotate-enrollment
```

Use the separate uninstall scripts when required:

```bash
sudo bash uninstall-client-ver14.sh
sudo bash uninstall-controller-ver14.sh
```

See [INSTALLATION_AND_CONFIGURATION.md](docs/INSTALLATION_AND_CONFIGURATION.md) for VMware/MobaXterm steps, connectivity checks, Vercel/Cloudflare configuration, Supabase bootstrap, upgrades, and optional purge behavior.

For the two-node AWS Red Hat path, use
[V14_AWS_RED_HAT_INSTALLATION.md](docs/V14_AWS_RED_HAT_INSTALLATION.md). The
single additive controller command that installs private VPC TLS and then
starts a named Cloudflare Tunnel is:

```bash
sudo bash install-controller-aws-cloudflare-ver14.sh \
  --rpm ./nli_controller_agent_ver14.rpm \
  --private-endpoint 10.20.1.10 \
  --public-hostname nli-controller.example.com \
  --tunnel-token-file /root/cloudflare-tunnel-token.txt
```

See [V14_CLOUDFLARE_TUNNEL.md](docs/V14_CLOUDFLARE_TUNNEL.md),
[VERCEL_DEPLOYMENT.md](docs/VERCEL_DEPLOYMENT.md), and
[CLOUDFLARE_PAGES_DEPLOYMENT.md](docs/CLOUDFLARE_PAGES_DEPLOYMENT.md) for the
two supported hosted WebApp paths.

See [WEBAPP_OPERATOR_AND_PARSER_GUIDE.md](docs/WEBAPP_OPERATOR_AND_PARSER_GUIDE.md) for the complete WebApp exercise: Assurance samples, parser and policy drafts, editing, independent approval, publication, fleet verification, retry, deletion limits, users/RBAC, and controller/client operations.

See [V14 support matrix](docs/V14_SUPPORT_MATRIX.md) for tested platform and hosting scope, and [V14 engineering audit](docs/V14_ENGINEERING_AUDIT.md) for evidence, release claims, provenance, and known limits.

## WebView and hosted webapp

The controller serves the WebView directly through its private address or a named Cloudflare Tunnel. Vercel and Cloudflare Pages can host the same static application when the server-only `NLI_CONTROLLER_ORIGIN` points to the stable public Cloudflare controller hostname. The Supabase URL, a private VPC address, an EC2 `.compute.internal` hostname, and the hosted WebApp URL are not controller origins.

No data-sync script is required. The webapp proxies authenticated API calls to the controller, and the controller performs authorized Supabase operations. Live data changes do not require a Vercel redeployment.

## Cached verification

Use the reusable content-hash runner. A successful suite is skipped until a relevant source byte changes; every run writes a text log and JSON result outside the repository by default.

```powershell
python scripts/verify_cached.py --suite quick
python scripts/verify_cached.py --suite full
python scripts/verify_cached.py --suite release
powershell -File scripts/verify-sample-field-accounting.ps1
```

Pass `--root C:\path\to\future\nli-vN` to reuse it with a copied future version, `--output-dir` to choose the evidence directory, and `--force` only when the environment changed while the source did not.

## Repository structure

```text
cmd/                 Go command entry points
configs/             Controller/client defaults and reviewed parser assets
internal/            Runtime, controller, storage, migration, and sync packages
parser-policy/       Declarative ParserPack model and executor
projection/          Field projection and parity policies
static-layout/       Positional parsing primitives
supabase/             Protected schema/RPC and private Storage bootstrap
telemetry/            Event and metrics contracts
web/                  Production WebView and browser tests
packaging/            RPM specs, systemd units, and reproducible build inputs
scripts/              Guided setup, uninstall, deployment, and cached verification
tests/                Corpus, API, browser, package, Supabase, and Rocky checks
docs/                 Architecture, release, and operating documentation
```

## Repository

<https://github.com/vjay136/nexora_log_intelligence_ver14.git>

## License

Proprietary — Nexora. Third-party dependencies and source-derived release inputs are recorded in the SBOM and provenance evidence.
