#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
ARTIFACT_DIR=${1:?usage: regenerate-evidence.sh ARTIFACT_DIR}
# shellcheck disable=SC1091
source "$ROOT/packaging/release.env"
# shellcheck disable=SC1091
source "$ROOT/packaging/docker/images.env"
revision=$(tr -d '\r\n' <"$ARTIFACT_DIR/SOURCE_REVISION")
[[ $revision =~ ^[0-9a-f]{40}$ ]] || { echo 'invalid SOURCE_REVISION' >&2; exit 1; }
epoch=$(git -C "$ROOT" show -s --format=%ct "$revision")
docker run --rm \
  --mount "type=bind,src=$ROOT,dst=/src,readonly" \
  --mount "type=bind,src=$ARTIFACT_DIR,dst=/artifacts" \
  "$EVIDENCE_IMAGE" python /src/scripts/release/generate-evidence.py \
  --artifacts /artifacts --version "$NLI_VERSION" --release "$NLI_RELEASE" \
  --revision "$revision" --source-date-epoch "$epoch" --images /src/packaging/docker/images.env
docker run --rm \
  --mount "type=bind,src=$ROOT,dst=/src,readonly" \
  --mount "type=bind,src=$ARTIFACT_DIR,dst=/artifacts" \
  "$RPM_BUILD_IMAGE" bash -lc 'bash /src/packaging/docker/install-rpm-tools.sh >/dev/null; ROOT=/src bash /src/scripts/release/create-bundle.sh /artifacts'
echo 'release evidence regenerated'
