#!/bin/bash
# RelaxNGo Benchmark Runner
# Usage: ./scripts/run_benchmarks.sh [baseline|compare]
#
# baseline: Run benchmarks and save as baseline
# compare:  Run benchmarks and compare against baseline
# (no args): Run benchmarks once

set -e

BENCHMARK_DIR="${PWD}/benchmarks"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BASELINE_FILE="${BENCHMARK_DIR}/baseline.txt"
CURRENT_FILE="${BENCHMARK_DIR}/benchmark-${TIMESTAMP}.txt"

# Create benchmark directory
mkdir -p "${BENCHMARK_DIR}"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "Running RelaxNGo benchmarks..."
echo "Timestamp: ${TIMESTAMP}"
echo ""

# Run benchmarks
go test -bench=. -benchmem -run=^$ ./... > "${CURRENT_FILE}"

if [ "$1" == "baseline" ]; then
    cp "${CURRENT_FILE}" "${BASELINE_FILE}"
    echo -e "${GREEN}✓ Baseline saved to: ${BASELINE_FILE}${NC}"
    cat "${BASELINE_FILE}"

elif [ "$1" == "compare" ]; then
    if [ ! -f "${BASELINE_FILE}" ]; then
        echo -e "${RED}✗ No baseline found. Run 'scripts/run_benchmarks.sh baseline' first.${NC}"
        exit 1
    fi

    echo "Comparing against baseline..."
    echo ""

    # Install benchstat if needed
    if ! command -v benchstat &> /dev/null; then
        echo "Installing benchstat..."
        go install golang.org/x/perf/cmd/benchstat@latest
    fi

    # Run comparison
    benchstat -alpha 0.05 "${BASELINE_FILE}" "${CURRENT_FILE}"

else
    cat "${CURRENT_FILE}"
    echo ""
    echo "Benchmark results saved to: ${CURRENT_FILE}"
    echo ""
    echo "To set as baseline:  ./scripts/run_benchmarks.sh baseline"
    echo "To compare:          ./scripts/run_benchmarks.sh compare"
fi
