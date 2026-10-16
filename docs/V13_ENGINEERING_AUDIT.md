# NLI V13 engineering and release audit

This document separates repository evidence from product and release claims. It is a scope note for maintainers and release reviewers; it does not certify a release by itself.

## V13 parser and resource hardening

The V13 executor fails closed when a mixed-key/value stage leaves a nonempty remainder, so stop-key or malformed tails cannot disappear behind a successful parse. Explicitly configured leading text remains supported. Agent configuration reads are capped at 1 MiB and accept exactly one JSON document. Invalid UTF-8, NUL bytes, and unsupported control bytes bypass production parsers and take the visible unknown route.

Physical lines are processed in bounded 1 MiB segments instead of being drained without a work limit. Every segment is retained for the unknown route until the terminating newline; oversized input is never reported as a successful semantic parse. Multiline events have an 8 MiB hard ceiling and a 10,000-line ceiling, including the initial line. Drain state defaults to 10,000 clusters and clamps configured or restored state to 100,000. AI discovery queues default to 1,000 pending clusters and 16 KiB per sample, with truncation and dropped-queue counters exposed in agent metrics.

The unlabeled benchmark reports `successful_route_rate`; it does not call that value exact parse accuracy. Field-level precision, recall, or semantic accuracy requires labeled expected output. Corpus guarantees remain tied to the checked-in reviewed fixtures and their golden assertions.

## Packaging, installation, and lifecycle

The current release workflow builds x86_64 Linux binaries and RPM packages, then validates RPM contents and service behavior in Rocky Linux 9.6 and 10.2 containers. The package specs declare x86_64 and systemd dependencies. The prompted controller/client shell installers use the RPM toolchain and are documented for Rocky Linux 9 and 10. This is a Rocky/RHEL-family direction, not a universal Linux installer. Other distributions and architectures remain unclaimed until tested; see [the support matrix](V13_SUPPORT_MATRIX.md).

RPM configuration files use `config(noreplace)`, and uninstall scripts have preserve-state and explicit purge behavior. The Rocky lifecycle script tests install, upgrade, downgrade, and re-upgrade when prior RPMs are supplied. V13 migration acceptance must use genuine V8 8.0.0 client and controller RPMs. The package-infrastructure fixture creates a synthetic prior-version RPM from the current source/spec; it exercises packaging mechanics only and is not evidence of V8 compatibility. If the genuine V8 packages are absent, the V8-to-V13 migration gate remains unverified.

The packages install systemd units and rely on RPM scriptlets. No DEB, OCI image, non-systemd service adapter, or architecture other than x86_64 is presently part of the verified release surface. Broader formats require their own package contents, installation instructions, upgrade/rollback path, and target acceptance.

## Web deployment

The controller RPM embeds the authenticated WebView and serves its API. The webapp release bundle contains static frontend assets plus Vercel and Cloudflare function adapters; the Vercel deployment config and proxy contract checks are present. Hosted operation needs a reachable HTTPS controller origin. A plain static file host does not implement authenticated API requests and must not be described as a complete NLI deployment.

The repo's existence of a Cloudflare function adapter or Vercel config proves implementation inputs exist, not that a production deployment has passed end-to-end host acceptance. Treat Vercel as configured and contract-checked; treat Cloudflare and generic hosts as experimental until deployment artifacts and browser-to-controller behavior are exercised in the target environment. Keep the controller origin and credentials server-side.

## Evidence and claim discipline

Claims should cite a concrete artifact and check:

- Platform support: exact distro image/version/architecture and successful install/service/lifecycle evidence.
- Package identity: RPM metadata, payload, permissions, ownership, and hashes.
- Migration: genuine prior-release packages and state/config preservation across upgrade, rollback, and re-upgrade. Synthetic fixtures are labeled as such.
- Web hosting: built artifact plus host-specific routing and authenticated browser-to-API checks.
- Reproducibility: two builds from the same clean committed revision with byte-identical unsigned outputs.
- Integrity: `SHA256SUMS`, `SOURCE_REVISION`, release manifest, SBOM, binary provenance, and in-toto/SLSA statement generated for the exact release artifacts.
- Signatures: say “signed” only when signatures are present and verified against an explicit trust root. The default build is unsigned.

The release scripts require a clean committed revision and build with digest-pinned containers. Evidence manifests improve traceability but do not substitute for execution of the corresponding acceptance matrix. Record skipped gates and missing external inputs as unverified.

## Provenance and licensing policy

For each release, preserve the source revision, build image digests, build timestamp/epoch inputs, toolchain and dependency inventory, artifact hashes, SPDX SBOM, binary build metadata, and provenance statement together. The release must be reproducible from the same committed source and declared inputs; local credentials and signing keys stay outside the repository and build mounts. Sign only as a separate promotion step, then regenerate evidence over the signed bytes and verify signatures using the published trust root.

NLI is marked proprietary in the RPM metadata and repository documentation. Third-party libraries and imported/sample parser sources must retain their upstream license notices and attribution obligations in source distributions and release bundles. The SBOM is an inventory, not a license grant. Before redistribution, review each dependency and source-derived input for license compatibility, preserve required notices, identify any restricted or unclear input, and do not imply that proprietary NLI code changes the license of third-party material. A release should not be promoted while a material licensing obligation is unresolved.

## Remaining limits

- Tested Rocky 9/10 x86_64 RPM support does not imply support for all RHEL derivatives, all minor releases, other CPU architectures, or all Linux distributions.
- A static frontend alone cannot deliver the authenticated control plane; a compatible proxy/runtime and reachable controller are required.
- V8-to-V13 migration remains conditional on genuine V8 artifacts and a completed release matrix run.
- Upgrade rollback of RPM files does not by itself roll back external Supabase state or guarantee reversal of an incompatible schema/data change. Back up controller state and document schema compatibility before release.
- Provenance and hashes establish artifact identity and build inputs, not semantic correctness, production security, or third-party licensing clearance.
- Parser guarantees remain scoped to supplied reviewed corpora and explicit lossless fallback behavior; they do not establish semantic correctness for every future log format.
- One oversized physical line may produce several bounded unknown-route records. Their bytes are preserved in emission order and the condition is visible, but the records must be reassembled if an operator needs the original giant line as one object.
