# NLI v15 release infrastructure

This directory documents the only supported V15 release path. It builds source-derived static Linux binaries in a digest-pinned Go image, creates genuine RPMs with `rpmbuild` in a digest-pinned Rocky image, performs a two-pass byte-for-byte reproducibility check, and emits checksums, SPDX SBOM, release manifest, binary provenance, and an in-toto/SLSA provenance statement.

## Safety and prerequisites

- Release input must be one clean, committed, frozen Git revision. Dirty or untracked source is refused.
- Outputs go to `NLI_BUILD_SCRATCH` or an explicit output directory; the repository `dist` directory is never used.
- `nli_clinet_ver15.rpm` is a byte-identical filename alias of `nli_client_ver15.rpm`. Both contain the same `nli-client` NEVRA and must not be treated as separate packages.
- Upgrade acceptance uses genuine V12 client and controller RPMs when they are supplied to the V15 release matrix. V8-to-V15 migration evidence remains a separate gate when genuine V8 artifacts are supplied.
- The release remains unsigned unless an explicit signing key is supplied. Unsigned output must never be described as signed.
- Operators receive one `nli-v<version>-<release>-x86_64.tar.gz` archive plus its external `.sha256`. The archive contains `RELEASE_ID`, all RPM/web/operator artifacts, and the complete internal checksum manifest. It must be extracted into a fresh versioned directory.

## Commands

```bash
export NLI_BUILD_SCRATCH=/absolute/session-owned/scratch
make release-static
make release-package-test
make release-lifecycle-test
make release-preflight
make release-build OUTPUT_DIR="$NLI_BUILD_SCRATCH/nli-v15-release"
make release-rocky \
  OUTPUT_DIR="$NLI_BUILD_SCRATCH/nli-v15-release" \
  PREVIOUS_DIR=/absolute/path/to/genuine/previous-rpms
```

Windows PowerShell uses the same pinned two-pass build:

```powershell
$env:NLI_BUILD_SCRATCH = 'C:\absolute\session-owned\scratch'
.\packaging\build-release.ps1 -OutputDirectory "$env:NLI_BUILD_SCRATCH\nli-v15-release"
```

## Signing hooks

Unsigned reproducibility is checked before signing because signatures intentionally add nondeterministic key material and timestamps. Run Rocky acceptance against the verified unsigned bytes, then sign for promotion. RPM signing must be followed by evidence regeneration because it changes RPM hashes:

```bash
REQUIRE_SIGNATURES=1 RPM_GPG_NAME='NLI Release' \
  make release-sign-rpm OUTPUT_DIR="$NLI_BUILD_SCRATCH/nli-v15-release"
make release-evidence OUTPUT_DIR="$NLI_BUILD_SCRATCH/nli-v15-release"
REQUIRE_SIGNATURES=1 COSIGN_KEY=... \
  make release-sign OUTPUT_DIR="$NLI_BUILD_SCRATCH/nli-v15-release"
```

To use GPG detached signatures instead, set `GPG_KEY_ID`. Private keys are never read from the repository and are not mounted by the default build.

Verify promotion signatures with an explicit trust root:

```bash
COSIGN_PUBLIC_KEY=/absolute/path/to/cosign.pub \
  make release-sign-verify OUTPUT_DIR="$NLI_BUILD_SCRATCH/nli-v15-release"
```

## Rocky acceptance

`tests/rocky/run-matrix.sh` builds systemd-capable containers from digest-pinned Rocky Linux 9.6 and official Rocky Linux 10.2 images. It validates:
- RPM magic, digest, name/version/architecture, payload, ownership, and modes;
- byte-identical compatibility alias behavior;
- clean install, real previous-release upgrade, downgrade rollback, and re-upgrade;
- all three systemd services and `systemd-analyze verify`;
- exact 26-field SMTP CLI output;
- controller health, authenticated API, and embedded WebView;
- service restart persistence and state/config preservation on uninstall.

## Integration inputs still required

A final release run must wait until other writers stop and one integrated revision is frozen. That revision must provide:

1. compiling and passing Go source, vet, race, and fuzz gates;
2. runtime binaries reporting `15.0.0` with clean `nexora.local/nli-v15` VCS provenance;
3. exact SMTP CLI output matching `tests/release/smtp_expected.json`;
4. a complete WebView without stale `0.4.0`, `v1.0.0`, or `webview_ver1` identifiers;
5. no disabled tests; and
6. genuine previous-release client/controller RPMs for upgrade, rollback, and re-upgrade acceptance.
