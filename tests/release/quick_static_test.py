#!/usr/bin/env python3
"""Fast, platform-neutral V15 release invariants for the cached quick suite."""

from __future__ import annotations

import json
from pathlib import Path
import re
import sys


def fail(message: str) -> None:
    raise SystemExit(message)


root = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).parents[2]).resolve()

required = (
    "scripts/install-controller-ver15.sh",
    "scripts/install-client-ver15.sh",
    "scripts/install-controller-aws-ver15.sh",
    "scripts/install-client-aws-ver15.sh",
    "scripts/install-controller-aws-cloudflare-ver15.sh",
    "scripts/setup-cloudflare-tunnel-v15.sh",
    "scripts/verify-cloudflare-tunnel-v15.sh",
    "scripts/uninstall-cloudflare-tunnel-v15.sh",
    "scripts/cloudflare-quick-diagnosis-v15.sh",
    "scripts/vercel-preflight.mjs",
    "wrangler.jsonc",
    "scripts/uninstall-controller-ver15.sh",
    "scripts/uninstall-client-ver15.sh",
    "scripts/release/create-bundle.sh",
    "configs/parser-catalog/nli-v15-parser-catalog.json",
    "configs/catalog/nli-v15-parser-catalog.json",
    "testdata/migration/sample-logs/manifest.json",
    "docs/V15_AWS_RED_HAT_INSTALLATION.md",
    "docs/V15_CLOUDFLARE_TUNNEL.md",
    "docs/VERCEL_DEPLOYMENT.md",
    "docs/CLOUDFLARE_PAGES_DEPLOYMENT.md",
)
for name in required:
    if not (root / name).is_file():
        fail(f"missing V15 release input: {name}")

scan_roots = ("README.md", "docs", "packaging", "scripts", "web", ".github")
files: list[Path] = []
for name in scan_roots:
    path = root / name
    if path.is_file():
        files.append(path)
        continue
    for candidate in path.rglob("*"):
        if any(part in {"node_modules", "build", "dist", "__pycache__", ".playwright"} for part in candidate.parts):
            continue
        if candidate.is_file():
            files.append(candidate)

wrong_product = "Nexora Log Inetelligence V15"
publishable_key_pattern = re.compile(r"sb_publishable_[A-Za-z0-9_-]{20,}")
live_project_pattern = re.compile(r"https://[a-z0-9]{20}\.supabase\.co", re.IGNORECASE)
allowed_history = {root / "docs/NLI_V5_IMPLEMENTATION_SPEC.md", root / "CHANGELOG.md"}
for path in files:
    if path in allowed_history:
        continue
    try:
        text = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        continue
    if "1210.0.0.1" in text:
        fail(f"invalid loopback address remains in {path.relative_to(root)}")
    if wrong_product in text:
        fail(f"incorrect product name remains in {path.relative_to(root)}")
    if publishable_key_pattern.search(text):
        fail(f"live publishable key was committed in {path.relative_to(root)}")
    if live_project_pattern.search(text):
        fail(f"live Supabase project URL was committed in {path.relative_to(root)}")

catalog_a = (root / "configs/parser-catalog/nli-v15-parser-catalog.json").read_bytes()
catalog_b = (root / "configs/catalog/nli-v15-parser-catalog.json").read_bytes()
if catalog_a != catalog_b:
    fail("canonical and embedded V15 parser catalogs differ")

manifest = json.loads((root / "testdata/migration/sample-logs/manifest.json").read_text(encoding="utf-8"))
if manifest.get("sample_log_record_count") != 728:
    fail(
        "sample manifest expected 728 records, got "
        f"{manifest.get('sample_log_record_count')!r}"
    )

for installer_name in required[:2]:
    installer = (root / installer_name).read_text(encoding="utf-8")
    if "rpm_release == 1" in installer or "not release 1" in installer:
        fail(f"hard-coded RPM release validation remains in {installer_name}")
    for marker in ("validate_rpm_release", "^[1-9][0-9]*$", "--verify-only"):
        if marker not in installer:
            fail(f"{installer_name} is missing release validation marker {marker!r}")
    for marker in ("RELEASE_ID is missing", "release bundle checksum verification failed", "does not match this bundle release"):
        if marker not in installer:
            fail(f"{installer_name} is missing mixed-release protection {marker!r}")

print("V15 quick static release invariants: PASS")
