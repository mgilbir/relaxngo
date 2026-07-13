#!/bin/bash
# Run Go fuzzing against relaxngo.
# Each fuzz target lives in the package it exercises, so it must be run against
# that package's directory (go test -fuzz runs one target in one package).

set -euo pipefail

FUZZ_TIME=${1:-30s} # Duration per target (default 30s); pass e.g. 2m for longer runs.

cd "$(dirname "$0")/.."

echo "=== relaxngo fuzzing suite (${FUZZ_TIME} per target) ==="

run() {
	local pkg=$1 target=$2
	echo ""
	echo ">>> ${target} (${pkg})"
	go test "${pkg}" -run '^$' -fuzz="^${target}\$" -fuzztime="${FUZZ_TIME}"
}

run ./rng FuzzParseSchema
run ./validator FuzzValidateXML
run ./generator FuzzGenerateCode

echo ""
echo "=== fuzzing complete ==="
echo "Any crash inputs are written under the package's testdata/fuzz/ directory."
