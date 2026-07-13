#!/bin/bash
# Performance profiling script for relaxngo.
# Generates CPU, memory, and trace profiles from one package's benchmarks.
#
# Usage: scripts/profile.sh [package] [output-dir]
#   package    Go package to benchmark (default ./validator). Profile flags
#              (-cpuprofile/-memprofile/-trace) only work with a single package,
#              so this must not be a multi-package pattern like ./...

set -euo pipefail

PKG=${1:-./validator}
PROFILE_DIR=${2:-.profile}
mkdir -p "$PROFILE_DIR"

cd "$(dirname "$0")/.."

echo "=== relaxngo profiling: $PKG -> $PROFILE_DIR ==="

echo "CPU profile..."
go test "$PKG" -bench=. -benchmem -benchtime=10s -cpuprofile="$PROFILE_DIR/cpu.prof" 2>&1 | tee "$PROFILE_DIR/bench_output.txt"

echo "Memory profile..."
go test "$PKG" -bench=. -benchmem -benchtime=10s -memprofile="$PROFILE_DIR/mem.prof" >/dev/null

echo "Trace..."
go test "$PKG" -bench=. -benchtime=10s -trace="$PROFILE_DIR/trace.out" >/dev/null

cat <<EOF

=== profiling complete ===
Analyze with:
  go tool pprof $PROFILE_DIR/cpu.prof     # 'top20', 'list <func>'
  go tool pprof $PROFILE_DIR/mem.prof     # 'top20 -cum'
  go tool trace $PROFILE_DIR/trace.out
Benchmark output: $PROFILE_DIR/bench_output.txt
EOF
