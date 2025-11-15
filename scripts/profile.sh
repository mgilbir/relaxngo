#!/bin/bash
# Performance profiling script for RelaxNGo
# Generates CPU, memory, and trace profiles for analysis

set -e

PROFILE_DIR=${1:-.profile}
mkdir -p "$PROFILE_DIR"

echo "=== RelaxNGo Performance Profiling ==="
echo ""
echo "Output directory: $PROFILE_DIR"
echo ""

# Run benchmarks with profiling
echo "Running benchmarks with CPU profiling..."
go test -bench=. -cpuprofile="$PROFILE_DIR/cpu.prof" -benchmem -benchtime=10s ./... 2>&1 | tee "$PROFILE_DIR/bench_output.txt"

echo ""
echo "Running benchmarks with memory profiling..."
go test -bench=. -memprofile="$PROFILE_DIR/mem.prof" -benchmem -benchtime=10s ./... > /dev/null 2>&1

echo ""
echo "Running benchmarks with trace..."
go test -bench=. -trace="$PROFILE_DIR/trace.out" ./... > /dev/null 2>&1

echo ""
echo "=== Profiling Complete ==="
echo ""
echo "To analyze profiles, use:"
echo "  # CPU profile (shows where time is spent)"
echo "  go tool pprof $PROFILE_DIR/cpu.prof"
echo "  > top20      # Show top 20 functions"
echo "  > list ParseXML  # Show source for specific function"
echo ""
echo "  # Memory profile (shows allocations)"
echo "  go tool pprof $PROFILE_DIR/mem.prof"
echo "  > top20 -cum  # Top 20 by cumulative memory"
echo ""
echo "  # Trace (timeline view of execution)"
echo "  go tool trace $PROFILE_DIR/trace.out"
echo ""
echo "Benchmark results saved to: $PROFILE_DIR/bench_output.txt"
