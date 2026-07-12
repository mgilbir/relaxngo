// Package main generates benchmark projects from the official RELAX NG test suite.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/internal/conformance"
	"github.com/mgilbir/relaxngo/rng"
)

// Generate benchmark projects from the official RELAX NG test suite.
//
// This script pre-generates complete, buildable Go projects for each valid test case,
// allowing benchmarks to measure only parsing/validation performance without including
// code generation and compilation time.
//
// Output directory structure:
//
//	<output-dir>/
//	  Test049_1.v/
//	    go.mod
//	    types.go                 (generated code)
//	    types_test.go            (benchmark: BenchmarkParseValidate_Test049_1_v)
//	  Test050_1.v/
//	    ...
//	  Test135_1.v/
//	    ...
//
// Usage:
//
//	go run ./cmd/internal/generate-benchmark-projects -output /tmp/benchmark_projects
//
// Then use the generated projects with BenchmarkGeneratorClean:
//
//	go test ./benchmarks -run '^$' -bench 'BenchmarkGeneratorClean' -count=1 -v \
//	  -args -benchmark-projects-dir=/tmp/benchmark_projects
//
// This gives pure parsing/validation performance numbers without generation/build overhead.

func main() {
	outputDir := flag.String("output", "", "Output directory for generated test projects")
	flag.Parse()

	if *outputDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: generate_benchmark_projects -output <dir>\n")
		os.Exit(1)
	}

	// Load all official tests
	testDataPath := "testdata/official-tests"
	tests, err := conformance.LoadTestsFromFolder(testDataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load official tests: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded %d official test cases\n", len(tests))

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	stats := generateProjects(tests, *outputDir)
	printSummary(stats)
}

func generateProjects(tests []conformance.FolderTestCase, outputDir string) struct {
	Total            int
	Generated        int
	Skipped          int
	GenerationFailed int
	BuildFailed      int
} {
	var stats struct {
		Total            int
		Generated        int
		Skipped          int
		GenerationFailed int
		BuildFailed      int
	}

	// Generate test projects
	for _, test := range tests {
		// Skip tests without a correct schema
		if test.Correct == "" {
			continue
		}

		// Only generate projects for valid documents
		for _, validXML := range test.Valid {
			stats.Total++

			xmlName := filepath.Base(validXML)
			projectName := fmt.Sprintf("Test%03d_%s", test.Number, strings.TrimSuffix(xmlName, filepath.Ext(xmlName)))
			projectDir := filepath.Join(outputDir, projectName)

			if err := generateBenchmarkProject(projectDir, test.Correct, validXML, test.Number); err != nil {
				fmt.Printf("SKIP: %s - %v\n", projectName, err)
				stats.Skipped++
				if strings.Contains(err.Error(), "Failed to generate") {
					stats.GenerationFailed++
				}
				if strings.Contains(err.Error(), "build") {
					stats.BuildFailed++
				}
				continue
			}

			fmt.Printf("GENERATED: %s\n", projectName)
			stats.Generated++
		}
	}

	return stats
}

func printSummary(stats struct {
	Total            int
	Generated        int
	Skipped          int
	GenerationFailed int
	BuildFailed      int
}) {
	fmt.Printf("\n=== GENERATION SUMMARY ===\n")
	fmt.Printf("Total valid test cases: %d\n", stats.Total)
	fmt.Printf("Successfully generated: %d\n", stats.Generated)
	fmt.Printf("Skipped: %d\n", stats.Skipped)
	if stats.GenerationFailed > 0 {
		fmt.Printf("  Generation failed: %d\n", stats.GenerationFailed)
	}
	if stats.BuildFailed > 0 {
		fmt.Printf("  Build failed: %d\n", stats.BuildFailed)
	}
}

// generateBenchmarkProject generates a single benchmark test project
func generateBenchmarkProject(projectDir, schemaPath, xmlPath string, testNum int) error {
	// Read XML payload
	// #nosec G304 - xmlPath comes from official test data
	xmlContent, err := os.ReadFile(xmlPath)
	if err != nil {
		return fmt.Errorf("failed to read XML: %w", err)
	}

	// Parse schema and generate types
	grammar, types, err := parseSchemaAndGenerateTypes(schemaPath)
	if err != nil {
		return err
	}

	if len(types) == 0 {
		return fmt.Errorf("no types generated from schema")
	}

	// Find root element name
	rootElementName := findRootElementName(grammar)
	if rootElementName == "" {
		return fmt.Errorf("could not determine root element name")
	}

	// Read schema content
	// #nosec G304 - schemaPath comes from official test data
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("failed to read schema: %w", err)
	}

	// Generate code
	code, err := generator.GenerateCode(types, "testpkg", string(schemaContent), grammar)
	if err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	// Create project directory
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	// Write project files
	if err := writeProjectFiles(projectDir, code, xmlContent, rootElementName, testNum); err != nil {
		return err
	}

	// Run go mod tidy
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = projectDir
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	return nil
}

func parseSchemaAndGenerateTypes(schemaPath string) (*rng.Grammar, []generator.TypeInfo, error) {
	// Parse schema
	absSchemaPath := schemaPath
	if !filepath.IsAbs(absSchemaPath) {
		cwd, _ := os.Getwd()
		absSchemaPath = filepath.Join(cwd, absSchemaPath)
	}

	grammar, err := rng.ParseSchemaFile(absSchemaPath, filepath.Dir(absSchemaPath))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Generate types
	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate types: %w", err)
	}

	return grammar, types, nil
}

func writeProjectFiles(projectDir string, code string, xmlContent []byte, rootElementName string, testNum int) error {
	// Write generated code
	if err := os.WriteFile(filepath.Join(projectDir, "types.go"), []byte(code), 0o600); err != nil {
		return fmt.Errorf("failed to write generated code: %w", err)
	}

	// Write go.mod
	cwd, _ := os.Getwd()
	goModContent := fmt.Sprintf(`module testpkg

go 1.21

require github.com/mgilbir/relaxngo v0.0.0

replace github.com/mgilbir/relaxngo => %s
`, cwd)
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte(goModContent), 0o600); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	// Create benchmark test file
	rootTypeName := toGoTypeName(rootElementName)
	xmlBaseName := filepath.Base(string(xmlContent))
	xmlNameClean := strings.TrimSuffix(xmlBaseName, filepath.Ext(xmlBaseName))
	xmlNameClean = strings.NewReplacer(".", "_", "-", "_").Replace(xmlNameClean)
	benchFuncName := fmt.Sprintf("BenchmarkParseValidate_Test%03d_%s", testNum, xmlNameClean)

	benchContent := fmt.Sprintf(`package testpkg

import (
	"encoding/xml"
	"testing"
)

func %s(b *testing.B) {
	payload := %s

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var root %s
		err := xml.Unmarshal([]byte(payload), &root)
		if err != nil {
			b.Fatalf("Failed to unmarshal: %%v", err)
		}
	}
}

func TestUnmarshal(t *testing.T) {
	payload := %s

	var root %s
	err := xml.Unmarshal([]byte(payload), &root)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %%v", err)
	}
	_ = root // Use the root variable
}
`, benchFuncName, "`"+string(xmlContent)+"`", rootTypeName, "`"+string(xmlContent)+"`", rootTypeName)

	if err := os.WriteFile(filepath.Join(projectDir, "types_test.go"), []byte(benchContent), 0o600); err != nil {
		return fmt.Errorf("failed to write benchmark: %w", err)
	}

	return nil
}

// findRootElementName finds the root element name from a grammar
func findRootElementName(grammar *rng.Grammar) string {
	if grammar.Start.Element != nil {
		return grammar.Start.Element.Name
	}
	if grammar.Start.Ref != nil {
		for _, def := range grammar.Defines {
			if def.Name == grammar.Start.Ref.Name {
				elem := def.FirstElement()
				if elem != nil {
					return elem.Name
				}
				break
			}
		}
	}
	return ""
}

// toGoTypeName converts an element name to a Go type name
func toGoTypeName(name string) string {
	if name == "" {
		return "Element"
	}
	// Capitalize first letter
	return strings.ToUpper(name[:1]) + name[1:]
}
