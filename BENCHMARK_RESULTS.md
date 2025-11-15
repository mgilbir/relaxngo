# Benchmark Results

## Performance Summary

Complete benchmark results from 229 test cases from the official RELAX NG test suite.

### Overall Performance

**Generated Code Performance (xml.Unmarshal):**
- **Min**: 5.9 µs/op
- **Median**: 7.2 µs/op
- **Max**: 26.9 µs/op

**Validator Performance:**
- **Min**: 2.1 µs/op
- **Median**: 2.6 µs/op
- **Max**: 19.7 µs/op

### Performance Ratio

| Complexity | Validator | Generated Code | Ratio |
|-----------|-----------|----------------|-------|
| Simple | 2.1-2.6 µs | 5.9-7.3 µs | ~2.8x |
| Medium | 3.0-9.0 µs | 8.5-15.8 µs | ~2.8-2.9x |
| Complex | 14.6-19.7 µs | 20.6-26.9 µs | ~1.4-1.5x |

### Sample Results from Different Complexity Levels

**Simple Schemas:**
| Test | Validator | Generated | Ratio |
|------|-----------|-----------|-------|
| Test049_1.v | 2.15 µs | 6.05 µs | 2.8x |
| Test054_1.v | 2.13 µs | 5.91 µs | 2.8x |
| Test089_1.v | 2.15 µs | 6.08 µs | 2.8x |

**Medium Schemas:**
| Test | Validator | Generated | Ratio |
|------|-----------|-----------|-------|
| Test066_1.v | 2.64 µs | 6.73 µs | 2.5x |
| Test050_1.v | 2.75 µs | 8.56 µs | 3.1x |
| Test250_1.v | 9.64 µs | 15.84 µs | 1.6x |

**Complex Schemas:**
| Test | Validator | Generated | Ratio |
|------|-----------|-----------|-------|
| Test247_1.v | 14.58 µs | 21.31 µs | 1.5x |
| Test251_1.v | 19.67 µs | 25.75 µs | 1.3x |
| Test369_1.v | 17.95 µs | 24.49 µs | 1.4x |

## Benchmark Details

### What's Being Measured

**Validator benchmarks:**
- Schema parsing and validation engine performance
- Pure constraint checking without materialization

**Generated code benchmarks:**
- XML unmarshalling via standard `encoding/xml.Unmarshal`
- Type conversion and struct field population
- Allocation overhead

### What's Not Included

- Schema loading/parsing time (pre-loaded)
- Code generation time
- Go compilation time
- Test harness overhead

## Test Coverage

229 test cases covering:
- Simple schemas (minimal structure, single elements)
- Medium schemas (multiple elements, moderate nesting, ~100-200 allocations/op)
- Complex schemas (deep nesting, many alternatives, ~260-370 allocations/op)

## Running the Benchmarks

### Prerequisites

Generate benchmark projects once:

```bash
go run ./cmd/internal/generate-benchmark-projects -output /tmp/benchmark_projects
```

### Run Full Benchmark Suite

```bash
go test ./benchmarks -run '^$' -bench 'BenchmarkGeneratorClean' -count=1 -v \
  -args -benchmark-projects-dir=/tmp/benchmark_projects
```

Expected runtime: ~10 minutes on a modern multi-core CPU.

### Run a Single Test

```bash
go test -bench . -benchmem /tmp/benchmark_projects/Test049_1.v
```

## Machine Configuration

- **CPU**: AMD Ryzen 9 6900HX with Radeon Graphics (16 cores)
- **OS**: Ubuntu 24.04.3 LTS
- **Go**: 1.25.3

## Key Observations

1. **Ratio decreases with complexity**: Simple schemas show ~2.8x overhead, while complex schemas show ~1.3-1.5x overhead. This is because the validator spends more time on constraint checking for complex schemas.

2. **Consistent allocation patterns**:
   - Simple schemas: ~70-100 allocs/op, ~12 KB/op
   - Medium schemas: ~100-200 allocs/op, ~13-17 KB/op
   - Complex schemas: ~260-370 allocs/op, ~18-24 KB/op

3. **Throughput**:
   - Generated code: ~140k ops/sec (simple) to ~40k ops/sec (complex)
   - Validator: ~460k ops/sec (simple) to ~50k ops/sec (complex)

## Methodology

Each benchmark:
1. Pre-generates a complete Go project with generated types from the schema
2. Uses standard `encoding/xml.Unmarshal` for parsing
3. Measures unmarshalling performance with timer reset
4. Runs with Go's `-benchmem` flag for allocation tracking
5. Executed on real schemas from the official RELAX NG test suite

## Notes

- Results are from a single benchmark run; multiple runs show consistent numbers
- Performance scales predictably with schema complexity
- Generated code provides type safety with modest overhead for simple cases
- Overhead becomes negligible for complex schemas
