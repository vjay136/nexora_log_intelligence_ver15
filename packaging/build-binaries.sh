#!/usr/bin/env bash
set -euo pipefail

ROOT=${ROOT:-/src}
OUTPUT_DIR=${OUTPUT_DIR:-/out}
GO=${GO:-go}
SOURCE_REVISION=${SOURCE_REVISION:-}
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-}
# shellcheck disable=SC1091
source "$ROOT/packaging/release.env"

fail() { echo "binary build: $*" >&2; exit 1; }
[[ -n $SOURCE_REVISION ]] || fail "SOURCE_REVISION is required"
git config --global --add safe.directory "$ROOT"

[[ -n $SOURCE_DATE_EPOCH ]] || fail "SOURCE_DATE_EPOCH is required"
[[ $(git -C "$ROOT" rev-parse HEAD) == "$SOURCE_REVISION" ]] || fail "source revision mismatch"
[[ -z $(git -C "$ROOT" status --porcelain) ]] || fail "source checkout is not clean"
mkdir -p "$OUTPUT_DIR/bin"
[[ -z $(find "$OUTPUT_DIR/bin" -mindepth 1 -print -quit) ]] || fail "output directory is not empty"

export GOFLAGS='-mod=readonly -buildvcs=true'
export SOURCE_DATE_EPOCH
cd "$ROOT"
"$GO" version | grep -F 'go1.24.9 ' >/dev/null || fail "Go 1.24.9 is required"

if [[ ${NLI_SKIP_TESTS:-0} != 1 ]]; then
"$GO" test -count=1 ./...
"$GO" vet ./...
CGO_ENABLED=1 "$GO" test -count=1 -race ./internal/mixedkv ./parser-policy ./internal/sync ./internal/controlclient ./internal/controlplane
"$GO" test ./internal/mixedkv -run='^$' -fuzz=FuzzParseNeverPanics -fuzztime="${FUZZTIME:-10s}"
fi

export CGO_ENABLED=0 GOOS="$NLI_GOOS" GOARCH="$NLI_GOARCH"
build() {
  local output=$1 package=$2
  "$GO" build -trimpath -buildvcs=true -ldflags='-s -w -buildid=' -o "$OUTPUT_DIR/bin/$output" "$package"
}
build nli ./cmd/nli-cli
build nli-agent ./cmd/nli-agent
build nli-client-control ./cmd/nli-client-control
build nli-bench ./cmd/nli-bench
build nli-controller-agent ./cmd/nli-control

: >"$OUTPUT_DIR/SOURCE_BINARY_PROVENANCE.txt"
for binary in "$OUTPUT_DIR"/bin/*; do
  [[ -x $binary ]] || fail "non-executable output: $binary"
  [[ $(od -An -tx1 -N4 "$binary" | tr -d ' \n') == 7f454c46 ]] || fail "not an ELF binary: $binary"
  "$GO" version -m "$binary" | grep -F $'\tbuild\tCGO_ENABLED=0' >/dev/null || fail "binary is not statically configured: $binary"
  "$GO" version -m "$binary" | grep -F $'\tbuild\tGOOS=linux' >/dev/null || fail "binary GOOS is not linux: $binary"
  "$GO" version -m "$binary" | grep -F $'\tbuild\tGOARCH=amd64' >/dev/null || fail "binary GOARCH is not amd64: $binary"
  {
    echo "=== $(basename "$binary") ==="
    "$GO" version -m "$binary"
    sha256sum "$binary"
  } >>"$OUTPUT_DIR/SOURCE_BINARY_PROVENANCE.txt"
  "$GO" version -m "$binary" | grep -F $'\tmod\t'"$NLI_MODULE" >/dev/null || fail "wrong module in $binary"
  "$GO" version -m "$binary" | grep -F $'\tbuild\tvcs.revision='"$SOURCE_REVISION" >/dev/null || fail "wrong VCS revision in $binary"
  "$GO" version -m "$binary" | grep -F $'\tbuild\tvcs.modified=false' >/dev/null || fail "dirty VCS provenance in $binary"
done

[[ $("$OUTPUT_DIR/bin/nli" version) == "nli $NLI_VERSION" ]] || fail "CLI version does not match release metadata"
[[ $("$OUTPUT_DIR/bin/nli-agent" --version) == "nli-agent $NLI_VERSION" ]] || fail "agent version does not match release metadata"
[[ $("$OUTPUT_DIR/bin/nli-client-control" --version) == "nli-client-control $NLI_VERSION" ]] || fail "client-control version does not match release metadata"
[[ $("$OUTPUT_DIR/bin/nli-controller-agent" --version) == "nli-control $NLI_VERSION" ]] || fail "controller version does not match release metadata"
(cd "$OUTPUT_DIR/bin" && sha256sum * | LC_ALL=C sort) >"$OUTPUT_DIR/BINARY_SHA256SUMS"
echo "binary build: PASS"
