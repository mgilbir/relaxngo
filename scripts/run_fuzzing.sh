#!/bin/bash
# Run Go fuzzing against RelaxNGo
# This script runs comprehensive fuzzing to find edge cases and crashes

set -e

echo "=== RelaxNGo Fuzzing Suite ==="
echo ""
echo "Starting extended fuzzing (this may take a while)..."
echo ""

FUZZ_TIME=${1:-30s}  # Default 30 seconds per target, or pass custom time

echo "Target 1: FuzzParseSchema (RELAX NG schema parsing)"
echo "  Duration: $FUZZ_TIME per run"
echo "  Running..."
timeout 120 go test -fuzz=FuzzParseSchema -fuzztime="$FUZZ_TIME" . || true
echo "  ✓ Completed"
echo ""

echo "Target 2: FuzzValidateXML (XML validation against schemas)"
echo "  Duration: $FUZZ_TIME per run"
echo "  Running..."
timeout 120 go test -fuzz=FuzzValidateXML -fuzztime="$FUZZ_TIME" . || true
echo "  ✓ Completed"
echo ""

echo "Target 3: FuzzGenerateCode (Type generation from schemas)"
echo "  Duration: $FUZZ_TIME per run"
echo "  Running..."
timeout 120 go test -fuzz=FuzzGenerateCode -fuzztime="$FUZZ_TIME" . || true
echo "  ✓ Completed"
echo ""

echo "=== Fuzzing Complete ==="
echo ""
echo "Results:"
echo "  - Check for any new testdata/fuzz/ directories created"
echo "  - These contain failing inputs that triggered crashes"
echo ""
echo "To replay a failing input:"
echo "  go test -fuzz=FuzzParseSchema -run FuzzParseSchema/<name>"
