#!/usr/bin/env python3
"""Verify every user-supplied parser reference has immutable V14 provenance."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import sys


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).resolve().parents[2]).resolve()
    base = root / "testdata/migration"
    manifest = json.loads((base / "source-parsers/manifest.json").read_text(encoding="utf-8-sig"))
    entries = manifest.get("entries", [])
    assert manifest.get("schema_version") == 1
    assert manifest.get("source_count") == len(entries) == 16
    assert "Supabase" in manifest.get("supabase_sync", "")

    catalog = (root / "configs/parser-catalog/nli-v14-parser-catalog.json").read_text(encoding="utf-8")
    seen: set[str] = set()
    repaired = 0
    for entry in entries:
        source_name = entry["source"]
        assert source_name not in seen, source_name
        seen.add(source_name)
        source = base / "source-parsers/original" / source_name
        fixture = base / entry["fixture"]
        assert source.is_file() and fixture.is_file(), source_name
        assert digest(source) == entry["sha256"], source_name
        assert digest(fixture) == entry["fixture_sha256"], source_name
        if entry["status"] == "byte_identical_reference":
            assert entry["sha256"] == entry["fixture_sha256"], source_name
        else:
            assert entry["status"] == "reviewed_minimal_repair", source_name
            repair = base / entry["repair"]
            repair_data = json.loads(repair.read_text(encoding="utf-8"))
            assert repair_data["original_sha256"] == entry["sha256"]
            assert repair_data["repaired_sha256"] == entry["fixture_sha256"]
            repaired += 1
        for pack_name in entry["packs"]:
            pack = root / pack_name
            assert pack.is_file(), f"{source_name}: {pack_name}"
            pack_id = json.loads(pack.read_text(encoding="utf-8"))["id"]
            assert pack_id in catalog, f"{source_name}: {pack_id} missing from catalog"

    assert repaired == 1
    print("source parser provenance: PASS (16/16 references, 15 byte-identical, 1 documented repair)")


if __name__ == "__main__":
    main()
