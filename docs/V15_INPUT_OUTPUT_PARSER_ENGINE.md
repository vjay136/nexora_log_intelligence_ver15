# Nexora Log Intelligence Ver15: Input and Output Parser Engine

## Input Parser Engine

The **Input Parser Engine** group contains the Parser Catalog, Parser Fleet Sync, Migration Center, KV delimiter policy, static layouts, field projection, regex/template studio, known-key catalog, semantic extraction and network/URL/email/encoded-subject engines. Each catalog item is an editable declarative draft. Test it in Assurance Lab, submit it for review, approve it with an independent reviewer, then publish it. Publication makes the desired parser revision available to the existing parser-fleet synchronization workflow.

Use `Automatic parser detection` when the source family is unknown. Select a reviewed parser family or a specific parser when a source is known. Field projection is an explicit allowlist; enable it only when intentional field reduction is required. The default lossless path preserves customer fields. GeoIP enrichment requires local MMDB files on the node and should be enabled only with the organization’s licensed database and privacy policy.

## Endpoint Configuration

The separate **Endpoint Configuration** tab builds local JSONL, Loki, ClickHouse and controller destination entries. Create, edit, remove drafts, test, submit for review, approve and publish revisioned profiles there. The configuration test validates structure and safe routing; perform reachability checks from the client host. The editor validates endpoint scheme, safe identifiers, field mapping syntax, and environment-variable names. It never stores secret values. The Loki URL example is an editable placeholder; replace it with your endpoint. Use protected environment files or root-owned credential files on client nodes.

Local normalised JSONL is independently optional. The profile always writes exact unparsed input to its required local quarantine JSONL path, and remote-only client configs receive a default quarantine path under `state_dir` when none is supplied. A writer-level route guard allows only fully parsed `normal` and `warning` records to Loki, ClickHouse, or controller endpoints even when a route filter is omitted or misconfigured. Quarantined records remain in the local retryable quarantine queue. Loki and ClickHouse configurations include a local fallback spool so remote delivery failures can be accounted for. In production, remote endpoints must use HTTPS and their CA chain must be trusted by the client host.

File destinations may use an absolute `path_template` containing one `{hour_utc}` token. The client resolves it to `YYYYMMDD_HH00` and switches files at each UTC hour, for example `imap_normalised_20261014_0800.jsonl` and `imap_quarantine_20261014_0800.jsonl`. The family-specific quarantine file retains the exact raw input record needed for replay. The aggregate fallback quarantine remains enabled when a rollout supplies no global quarantine destination.

## Input Log Paths

The separate **Input Log Paths** tab stores revisioned profiles for up to 100 unique absolute paths. Use the family dropdown to add `auto`, IMAP, POP, Webtop/Webmail, MTA, SMTP-IN, SMTP-OUT, PABD, CAPD, or PSS paths; **Load all standard paths** fills the complete RPM layout. `auto` invokes the registered automatic parser pipeline. A rollout names exact enrolled node IDs and enters the independent approval queue.

Input rollouts use `config_patch`, so the client preserves existing enrollment, telemetry, parser, enrichment and endpoint settings. The optional **replace outputs** control adds hourly normalised and quarantine files for every reviewed family. Leave it off when the node already has Loki/ClickHouse or custom file outputs. The client merges the patch, validates the complete candidate configuration, writes it atomically, restarts the agent, and restores the previous file if restart fails.

### Apply a profile to a client

Submit, independently approve, and publish the endpoint profile. Then choose that active profile and exact target node IDs in **Roll out endpoint profile**. The WebApp creates an approval-gated outputs-only `config_patch`; a different authorized operator must approve the rollout. Each client preserves its other configuration, validates the merged candidate, restarts, and reports success or rollback. The profile download remains available for offline review.

Set secret values separately on the client. Example names used by a profile are identifiers such as `NLI_LOKI_TOKEN` and `NLI_CLICKHOUSE_PASSWORD`; never put the token/password itself in Supabase, the profile JSON, or source control. Keep secret files readable only by the `nli` service account and administrators.

### ClickHouse field mapping

`field_columns` maps an input JSON field name to a ClickHouse column name, for example `{"fromhost":"client_ip"}`. ClickHouse creates mapped and other safe customer fields as `Nullable(String)` columns, while retaining the complete typed customer object in `event_json`. Scalars use their readable text value and nested values use canonical JSON text in their individual columns. This permits field-name queries without discarding the source JSON or guessing unsafe SQL types. Database, table, and mapped column identifiers are restricted to safe ASCII identifiers; no customer field is interpolated into a SQL expression without validation.

Keep field cardinality bounded. V15 limits each row to 256 top-level columns by default. Use explicit mappings for stable fields and keep `event_json` as the typed source of truth for fields that vary across log families.

## Known limits

The profile editor validates configuration structure; it does not prove that a remote system is reachable from a client network. Run a connection test from the client host or a controlled staging node. A saved profile becomes effective on a client only after the corresponding client configuration is installed and the agent reloads it. Certificate trust, firewall rules, DNS and remote permissions remain properties of the target environment.
