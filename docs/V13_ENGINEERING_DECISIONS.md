# NLI V13 engineering decisions

## Installation and trust

V13 separates package authenticity from transport identity:

- RPM SHA-256 and optional RPM/GPG signatures prove that a package was not changed after release.
- TLS certificates prove the controller endpoint identity and encrypt controller/client traffic.
- A GPG key cannot replace TLS and embedding a shared private key in an RPM would give every installation the same compromised identity. V13 never packages a TLS private key or a reusable enrollment secret.
- The default restricted-network path creates a unique controller-local private CA and a server certificate containing the controller's actual IP/DNS identities. The one-use client bootstrap contains only the CA certificate, controller URL, and a single-use enrollment token.
- Corporate PKI and public ACME remain optional. Controller installation and client enrollment do not depend on public DNS, Nginx, ACME, Cloudflare, Supabase, or Internet access.

Red Hat's RHEL 9 certificate guidance describes private CAs and the system trust store: <https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html-single/securing_networks/index>.

## Input filtering

Input selection executes before parser attempts. In `filtered` mode, a record is parsed only when its extracted service, event, method, or hostname satisfies the configured exact allowlist. `hybrid` mode applies the selected family to matches and automatic detection to other records. `default` parses all records.

Exact string matching is intentional: configuration stays bounded, predictable, and resistant to operator-supplied regular expressions that can cause excessive CPU use. Parser rules remain versioned and reviewed through Assurance and rollout workflows.

## Derived privacy fields

`email_bucket` hashes the normalized email local part and exposes only a two-hex-character prefix. `network_bucket` hashes the IPv4 `/24` or IPv6 `/64` prefix and exposes only one hex character. They are grouping aids, not security boundaries, and raw customer fields remain governed by output projection and masking policy.

Loki receives only bounded labels. High-cardinality event fields and identifiers remain in the JSON body/structured metadata because Loki warns that unbounded labels create excessive streams: <https://grafana.com/docs/loki/latest/get-started/labels/bp-labels/>.

## GeoIP

GeoIP reads only administrator-provided MaxMind-compatible `.mmdb` files from explicit absolute paths. Missing files disable that database without stopping parsing. A present but malformed or unreadable database is a startup error so silent bad enrichment cannot enter production data. Private addresses receive `is_private_ip=true` and no invented country or city.

The reader is opened once and reused for concurrent lookups. The implementation uses the MaxMind DB Go reader: <https://github.com/oschwald/maxminddb-golang>.

## Outputs

- Local output is newline-delimited JSON with family-isolated normal and quarantine routes.
- Loki uses bounded stream labels; the complete normalized event remains JSON.
- ClickHouse uses `JSONEachRow`, retains the complete event JSON, and can map reviewed customer fields to explicitly configured columns. Unknown/unapproved fields remain in the JSON fallback rather than changing the table unexpectedly.
- Controller output carries bounded parser/fleet telemetry by default. Customer event bodies are sent only when an operator explicitly enables that destination and the controller policy accepts them.

ClickHouse recommends batching at least 1,000 rows for synchronous inserts, preferably 10,000–100,000, or enabling `async_insert=1` with `wait_for_async_insert=1`: <https://clickhouse.com/resources/engineering/clickhouse-optimize-table-final>.

## Throughput claim

V13 uses streaming file reads, bounded line sizes, cached compiled parsers, reused HTTP clients, bounded queues, and backpressure. Ten million lines per second is a hardware, line-size, parser-complexity, storage, and destination-dependent target; it is not a portable single-node guarantee. Release evidence reports measured throughput from the included benchmark instead of claiming an unmeasured number.

Go's scanner defaults to a 64 KiB token ceiling unless the caller configures a larger bound; V13 keeps explicit input bounds to prevent unbounded memory use: <https://pkg.go.dev/bufio>.

