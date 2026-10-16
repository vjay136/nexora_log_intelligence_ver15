#!/usr/bin/env bash
set -euo pipefail

ROOT=${ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
ARTIFACT_DIR=${1:?usage: create-bundle.sh ARTIFACT_DIR}
# shellcheck disable=SC1091
source "$ROOT/packaging/release.env"

fail() { printf 'release bundle: %s\n' "$*" >&2; exit 1; }
for tool in python3 sha256sum tar gzip; do command -v "$tool" >/dev/null || fail "$tool is required"; done

manifest="$ARTIFACT_DIR/RELEASE_MANIFEST.json"
marker="$ARTIFACT_DIR/RELEASE_ID"
checksums="$ARTIFACT_DIR/SHA256SUMS"
[[ -s $manifest && -s $marker && -s $checksums ]] || fail 'release evidence, identity, or checksums are missing'

bundle_name="nli-v${NLI_VERSION}-${NLI_RELEASE}-${NLI_ARCH}"
archive="$ARTIFACT_DIR/$bundle_name.tar.gz"
sidecar="$archive.sha256"
epoch=$(python3 - "$manifest" <<'PY'
import json, pathlib, sys
value = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
print(value["source_date_epoch"])
PY
)
[[ $epoch =~ ^[0-9]+$ ]] || fail 'manifest source_date_epoch is invalid'

work=$(mktemp -d "${TMPDIR:-/tmp}/nli-release-bundle.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
stage="$work/$bundle_name"
mkdir -p "$stage"

while IFS= read -r -d '' source; do
  install -m "$(stat -c '%a' "$source")" "$source" "$stage/$(basename "$source")"
done < <(find "$ARTIFACT_DIR" -maxdepth 1 -type f ! -name 'nli-v*.tar.gz' ! -name 'nli-v*.tar.gz.sha256' -print0 | LC_ALL=C sort -z)

find "$stage" -exec touch -h -d "@$epoch" {} +
rm -f -- "$archive" "$sidecar"
tar --sort=name --format=gnu --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
  -C "$work" -cf - "$bundle_name" | gzip -n -9 >"$archive"
(cd "$ARTIFACT_DIR" && sha256sum "$(basename "$archive")" >"$(basename "$sidecar")")
printf 'release bundle: PASS (%s)\n' "$archive"
