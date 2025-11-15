package benchmarks

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
	"github.com/mgilbir/relaxngo/validator"
)

var (
	// Flag to specify directory of pre-generated benchmark projects
	benchmarkProjectsDir = flag.String("benchmark-projects-dir", "", "Directory containing pre-generated benchmark projects")
)

// fileExists checks if a file or directory exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// BenchmarkGeneratorClean benchmarks the code generator output without including
// code generation or compilation time in the results.
//
// This test assumes pre-generated benchmark projects exist in a directory.
// To generate them, run:
//
//	go run ./cmd/generate-benchmark-projects -output /tmp/benchmark_projects
//
// Then run the benchmark:
//
//	go test ./benchmarks -run '^$' -bench 'BenchmarkGeneratorClean' -count=1 -v \
//	  -args -benchmark-projects-dir=/tmp/benchmark_projects
//
// Results include:
// - Validator/Test<N>: Validator performance on that test case
// - Generator/Test<N>: Generated code performance (from pre-built projects)
//
// The Generator results are pure parsing/validation time without generation/build overhead.
//
//nolint:funlen // Comprehensive benchmark with setup, execution, and cleanup
func BenchmarkGeneratorClean(b *testing.B) {
	// Check if projects directory was provided
	if *benchmarkProjectsDir == "" {
		b.Skip("Use -args -benchmark-projects-dir=<dir> to run this benchmark")
	}

	// Verify projects directory exists
	if _, err := os.Stat(*benchmarkProjectsDir); err != nil {
		b.Fatalf("Benchmark projects directory not found: %v", err)
	}

	// Load all official tests for validator benchmarks
	// Find testdata relative to project root
	cwd, err := os.Getwd()
	if err != nil {
		b.Fatalf("Failed to get current directory: %v", err)
	}
	testDataPath := filepath.Join(cwd, "testdata", "official-tests")
	// If running from benchmarks directory, go up one level
	if !fileExists(testDataPath) {
		testDataPath = filepath.Join(cwd, "..", "testdata", "official-tests")
	}
	tests, err := validator.LoadTestsFromFolder(testDataPath)
	if err != nil {
		b.Fatalf("Failed to load official tests: %v", err)
	}

	b.Logf("Loaded %d official test cases", len(tests))

	// Collect all valid test cases
	type validTestCase struct {
		schemaPath string
		xmlPath    string
		testNum    int
		xmlName    string
		projectDir string
	}

	var validTestCases []validTestCase

	for _, test := range tests {
		if test.Correct == "" {
			continue
		}

		for _, validXML := range test.Valid {
			xmlName := filepath.Base(validXML)
			projectName := fmt.Sprintf("Test%03d_%s", test.Number, strings.TrimSuffix(xmlName, filepath.Ext(xmlName)))
			projectDir := filepath.Join(*benchmarkProjectsDir, projectName)

			// Only include test cases where a pre-generated project exists
			if _, err := os.Stat(projectDir); err != nil {
				continue
			}

			validTestCases = append(validTestCases, validTestCase{
				schemaPath: test.Correct,
				xmlPath:    validXML,
				testNum:    test.Number,
				xmlName:    xmlName,
				projectDir: projectDir,
			})
		}
	}

	b.Logf("Found %d pre-generated benchmark projects", len(validTestCases))

	// Statistics
	stats := struct {
		ValidatorBenchmarked int
		GeneratorBenchmarked int
		BenchmarksFailed     int
	}{}

	// Print summary after benchmark completes
	b.Cleanup(func() {
		b.Logf("\n=== CLEAN BENCHMARK RESULTS ===")
		b.Logf("Pre-generated projects benchmarked:")
		b.Logf("  Validator: %d test cases", stats.ValidatorBenchmarked)
		b.Logf("  Generator: %d test cases", stats.GeneratorBenchmarked)
		if stats.BenchmarksFailed > 0 {
			b.Logf("  Failed: %d", stats.BenchmarksFailed)
		}
		b.Logf("\nNote: Benchmark results shown as subtests above")
		b.Logf("Results exclude code generation and compilation time")
	})

	// Run benchmarks
	for i, testCase := range validTestCases {
		// Benchmark validator
		b.Run(fmt.Sprintf("Validator/Test%03d/%s", testCase.testNum, testCase.xmlName), func(b *testing.B) {
			xmlContent, _ := os.ReadFile(testCase.xmlPath)
			absSchemaPath := testCase.schemaPath
			if !filepath.IsAbs(absSchemaPath) {
				cwd, _ := os.Getwd()
				absSchemaPath = filepath.Join(cwd, absSchemaPath)
			}
			grammarBench, _ := rng.ParseSchemaFile(absSchemaPath, filepath.Dir(absSchemaPath))

			b.ResetTimer()
			for j := 0; j < b.N; j++ {
				val := validator.NewValidator(grammarBench, validator.DefaultOptions())
				reader := bytes.NewReader(xmlContent)
				_, _ = val.Validate(reader)
			}
		})

		stats.ValidatorBenchmarked++

		// Benchmark generated code (not using b.Run to avoid measuring subprocess overhead)
		// Instead, we run the benchmark directly and log results
		{
			xmlBaseName := filepath.Base(testCase.xmlPath)
			xmlNameClean := strings.TrimSuffix(xmlBaseName, filepath.Ext(xmlBaseName))
			xmlNameClean = strings.NewReplacer(".", "_", "-", "_").Replace(xmlNameClean)
			benchmarkName := fmt.Sprintf("BenchmarkParseValidate_Test%03d_%s", testCase.testNum, xmlNameClean)

			// Run go test benchmark on the pre-generated project
			// #nosec G204 - benchmarkName is derived from test case name
			benchCmd := exec.Command("go", "test", "-bench", benchmarkName, "-count=1", "-benchmem")
			benchCmd.Dir = testCase.projectDir
			output, err := benchCmd.CombinedOutput()

			if err != nil {
				b.Logf("Generator/Test%03d/%s: Benchmark failed: %v", testCase.testNum, testCase.xmlName, err)
				stats.BenchmarksFailed++
			} else {
				// Log the benchmark output which contains the timing information
				b.Logf("Generator/Test%03d/%s: %s", testCase.testNum, testCase.xmlName, string(output))
				stats.GeneratorBenchmarked++
			}
		}

		if i%20 == 0 {
			b.Logf("Progress: %d/%d test cases completed", i, len(validTestCases))
		}
	}
}
