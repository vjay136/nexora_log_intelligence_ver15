SHELL := /usr/bin/env bash
OUTPUT_DIR ?= $(NLI_BUILD_SCRATCH)/nli-release-output
PREVIOUS_DIR ?=

.PHONY: release-static release-package-test release-lifecycle-test release-preflight release-build release-rocky release-verify release-sign-rpm release-evidence release-sign release-sign-verify

release-static:
	bash tests/release/static.sh infra-only

release-package-test:
	bash tests/release/package-infra.sh

release-lifecycle-test:
	RUN_ROCKY_FIXTURE=1 bash tests/release/package-infra.sh

release-preflight:
	bash tests/release/static.sh full

release-build:
	bash scripts/release/build.sh "$(OUTPUT_DIR)"

release-rocky:
	@test -n "$(PREVIOUS_DIR)" || (echo 'PREVIOUS_DIR is required' >&2; exit 1)
	bash tests/rocky/run-matrix.sh "$(OUTPUT_DIR)" "$(PREVIOUS_DIR)"

release-verify:
	@test -d "$(OUTPUT_DIR)" || (echo 'OUTPUT_DIR does not exist' >&2; exit 1)
	bash scripts/release/verify-artifacts.sh "$(OUTPUT_DIR)"


release-sign-rpm:
	bash scripts/release/sign-artifacts.sh "$(OUTPUT_DIR)" rpm

release-evidence:
	bash scripts/release/regenerate-evidence.sh "$(OUTPUT_DIR)"

release-sign:
	bash scripts/release/sign-artifacts.sh "$(OUTPUT_DIR)" attest

release-sign-verify:
	bash scripts/release/verify-signatures.sh "$(OUTPUT_DIR)"
