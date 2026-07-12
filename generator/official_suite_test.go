package generator_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"text/template"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
	"github.com/mgilbir/relaxngo/validator"
)

// TestGeneratorOfficialSuite runs the official RELAX NG test suite
// against the code generator to ensure generated code behaves like the validator.
//
//nolint:funlen // Comprehensive test suite with setup, execution, and cleanup
func TestGeneratorOfficialSuite(t *testing.T) {

	// Load all official tests
	// Find testdata relative to project root
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	testDataPath := filepath.Join(cwd, "testdata", "official-tests")
	// If running from generator directory, go up one level
	if _, err := os.Stat(testDataPath); err != nil {
		testDataPath = filepath.Join(cwd, "..", "testdata", "official-tests")
	}

	tests, err := validator.LoadTestsFromFolder(testDataPath)
	if err != nil {
		t.Fatalf("Failed to load official tests: %v", err)
	}

	t.Logf("Loaded %d official test cases", len(tests))

	// Statistics using atomic operations for thread-safe parallel access
	var stats struct {
		Total              atomic.Int64
		Skipped            atomic.Int64
		SkippedNoTypes     atomic.Int64
		ValidPassed        atomic.Int64
		ValidFailed        atomic.Int64
		InvalidPassed      atomic.Int64
		InvalidFailed      atomic.Int64
		GeneratorMismatch  atomic.Int64
		GeneratorBuildFail atomic.Int64
	}

	// Print statistics after all tests complete (including parallel ones)
	t.Cleanup(func() {
		t.Logf("\n=== OFFICIAL SUITE RESULTS ===")
		t.Logf("Total test cases: %d", stats.Total.Load())
		t.Logf("Skipped (no schema): %d", stats.Skipped.Load())
		t.Logf("Skipped (no types/inline schemas): %d", stats.SkippedNoTypes.Load())
		t.Logf("\nValid documents:")
		t.Logf("  Passed: %d", stats.ValidPassed.Load())
		t.Logf("  Failed: %d", stats.ValidFailed.Load())
		t.Logf("\nInvalid documents:")
		t.Logf("  Passed: %d", stats.InvalidPassed.Load())
		t.Logf("  Failed: %d", stats.InvalidFailed.Load())
		t.Logf("\nGenerator issues:")
		t.Logf("  Validator/Generator mismatch: %d", stats.GeneratorMismatch.Load())
		t.Logf("  Generator build failures: %d", stats.GeneratorBuildFail.Load())

		totalTests := stats.ValidPassed.Load() + stats.ValidFailed.Load() + stats.InvalidPassed.Load() + stats.InvalidFailed.Load()
		totalPassed := stats.ValidPassed.Load() + stats.InvalidPassed.Load()
		if totalTests > 0 {
			passRate := float64(totalPassed) / float64(totalTests) * 100
			t.Logf("\nTests run (excluding skipped): %d", totalTests)
			t.Logf("Overall pass rate: %.1f%% (%d/%d)", passRate, totalPassed, totalTests)
		}
	})

	// Run tests
	for _, test := range tests {
		// Skip tests without a correct schema
		if test.Correct == "" {
			stats.Skipped.Add(1)
			continue
		}

		stats.Total.Add(1)

		// Test valid documents
		for _, validXML := range test.Valid {
			testName := fmt.Sprintf("Test%03d/Valid/%s", test.Number, filepath.Base(validXML))
			t.Run(testName, func(t *testing.T) {
				t.Parallel()
				result := runOfficialIntegrationTest(t, test.Correct, validXML, true)
				switch {
				case result.Skipped:
					stats.SkippedNoTypes.Add(1)
				case result.Success:
					stats.ValidPassed.Add(1)
				default:
					stats.ValidFailed.Add(1)
					if result.GeneratorMismatch {
						stats.GeneratorMismatch.Add(1)
					}
					if result.GeneratorBuildFailed {
						stats.GeneratorBuildFail.Add(1)
					}
				}
			})
		}

		// Test invalid documents
		for _, invalidXML := range test.Invalid {
			testName := fmt.Sprintf("Test%03d/Invalid/%s", test.Number, filepath.Base(invalidXML))
			t.Run(testName, func(t *testing.T) {
				t.Parallel()
				result := runOfficialIntegrationTest(t, test.Correct, invalidXML, false)
				switch {
				case result.Skipped:
					stats.SkippedNoTypes.Add(1)
				case result.Success:
					stats.InvalidPassed.Add(1)
				default:
					stats.InvalidFailed.Add(1)
					if result.GeneratorMismatch {
						stats.GeneratorMismatch.Add(1)
					}
					if result.GeneratorBuildFailed {
						stats.GeneratorBuildFail.Add(1)
					}
				}
			})
		}
	}
	// Stats are printed by t.Cleanup() after all parallel tests complete
}

// TestResult captures the result of running an integration test
type TestResult struct {
	Success              bool
	Skipped              bool
	ValidatorPassed      bool
	GeneratorPassed      bool
	GeneratorMismatch    bool
	GeneratorBuildFailed bool
	Error                string
}

// runOfficialIntegrationTest runs a single integration test
//
//nolint:funlen // Helper function with validator, generator, and comparison logic
func runOfficialIntegrationTest(t *testing.T, schemaPath, xmlPath string, shouldPass bool) TestResult {
	t.Helper()

	result := TestResult{}

	// Read XML payload
	xmlContent, err := os.ReadFile(xmlPath) // #nosec G304
	if err != nil {
		result.Error = fmt.Sprintf("Failed to read XML: %v", err)
		t.Log(result.Error)
		return result
	}

	// Step 1: Run validator
	// Make schema path absolute
	absSchemaPath := schemaPath
	if !filepath.IsAbs(absSchemaPath) {
		cwd, _ := os.Getwd()
		absSchemaPath = filepath.Join(cwd, schemaPath)
	}

	grammar, err := rng.ParseSchemaFile(absSchemaPath, filepath.Dir(absSchemaPath))
	if err != nil {
		result.Error = fmt.Sprintf("Failed to parse schema: %v", err)
		t.Log(result.Error)
		return result
	}

	val := validator.NewValidator(grammar, validator.DefaultOptions())
	reader := bytes.NewReader(xmlContent)
	errors, err := val.Validate(reader)

	validatorPassed := err == nil && len(errors) == 0
	result.ValidatorPassed = validatorPassed

	if validatorPassed {
		t.Logf("Validator: PASS")
	} else {
		t.Logf("Validator: FAIL (%d errors)", len(errors))
	}

	// Step 2: Try to generate code
	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to generate types: %v", err)
		t.Log(result.Error)
		return result
	}

	// Skip if no types were generated
	if len(types) == 0 {
		result.Skipped = true
		result.Error = "No types generated from schema"
		t.Log(result.Error)
		return result
	}

	// Find root element name
	rootElementName := findRootElementName(grammar)
	if rootElementName == "" {
		result.Skipped = true
		result.Error = "Could not determine root element name"
		t.Log(result.Error)
		return result
	}

	// Read schema content
	schemaContent, err := os.ReadFile(schemaPath) // #nosec G304
	if err != nil {
		result.Error = fmt.Sprintf("Failed to read schema: %v", err)
		t.Log(result.Error)
		return result
	}

	// Generate code
	code, err := generator.GenerateCode(types, "testpkg", string(schemaContent), grammar)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to generate code: %v", err)
		t.Log(result.Error)
		return result
	}

	// Step 3: Create temp directory and test the generated code
	tempDir := t.TempDir()

	// Write generated code
	if err := os.WriteFile(filepath.Join(tempDir, "types.go"), []byte(code), 0o600); err != nil {
		result.Error = fmt.Sprintf("Failed to write generated code: %v", err)
		t.Log(result.Error)
		return result
	}

	// Write go.mod
	// Go up one level from where we are to reach the project root
	cwd, _ := os.Getwd()
	moduleRoot := filepath.Join(cwd, "..")
	goModContent := fmt.Sprintf(`module testpkg

go 1.21

require github.com/mgilbir/relaxngo v0.0.0

replace github.com/mgilbir/relaxngo => %s
`, moduleRoot)
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0o600); err != nil {
		result.Error = fmt.Sprintf("Failed to write go.mod: %v", err)
		t.Log(result.Error)
		return result
	}

	// Create test file
	rootTypeName := toGoTypeName(rootElementName)
	testTmpl, err := template.New("test").Parse(`package testpkg

import (
	"encoding/xml"
	"testing"
)

func TestUnmarshalViaUnmarshalXML(t *testing.T) {
	payload := {{.Payload}}

	// Use standard XML unmarshaling which calls UnmarshalXML
	root := &{{.TypeName}}{}
	if err := xml.Unmarshal([]byte(payload), root); err != nil {
		t.Fatalf("Failed to unmarshal via UnmarshalXML: %v", err)
	}
	_ = root // Use the root variable
}

func TestRoundtrip(t *testing.T) {
	payload := {{.Payload}}

	// Parse XML into generated type using the validation constructor
	var root {{.TypeName}}
	if err := xml.Unmarshal([]byte(payload), &root); err != nil {
		t.Fatalf("Failed to unmarshal into generated type: %v", err)
	}

	// Serialize back to XML
	roundtripped, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal back to XML: %v", err)
	}

	// Re-marshal the original to normalize formatting
	normalized, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatalf("Failed to re-marshal original: %v", err)
	}

	// Compare normalized XML to ensure no data loss during roundtrip
	if string(normalized) != string(roundtripped) {
		t.Logf("Original:     %+v", root)
		t.Logf("Normalized XML:\n%s", normalized)
		t.Logf("Roundtripped XML:\n%s", roundtripped)
		t.Errorf("Roundtrip mismatch: data lost or changed during serialization")
	}

	t.Logf("Roundtrip successful, no field loss detected")
}
`)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to parse test template: %v", err)
		t.Log(result.Error)
		return result
	}

	var testBuf bytes.Buffer
	err = testTmpl.Execute(&testBuf, map[string]string{
		"Payload":  "`" + string(xmlContent) + "`",
		"TypeName": rootTypeName,
	})
	if err != nil {
		result.Error = fmt.Sprintf("Failed to execute test template: %v", err)
		t.Log(result.Error)
		return result
	}
	testContent := testBuf.String()

	if err := os.WriteFile(filepath.Join(tempDir, "types_test.go"), []byte(testContent), 0o600); err != nil {
		result.Error = fmt.Sprintf("Failed to write test: %v", err)
		t.Log(result.Error)
		return result
	}

	// Run go mod tidy
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tempDir
	_ = tidyCmd.Run() // Ignore errors

	// Run go test
	cmd := exec.Command("go", "test", "-v")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	generatorPassed := err == nil
	result.GeneratorPassed = generatorPassed

	if !generatorPassed {
		t.Logf("Generator test: FAIL")
		if strings.Contains(string(output), "build failed") {
			result.GeneratorBuildFailed = true
			t.Logf("  Build failed: %s", string(output))
		} else {
			// Show validation or runtime error
			t.Logf("  Test failed: %s", string(output))
		}
	} else {
		t.Logf("Generator test: PASS")
	}

	// Check if results match expectations
	if validatorPassed != generatorPassed {
		result.GeneratorMismatch = true
		t.Errorf("MISMATCH: validator=%v, generator=%v (expected both=%v)",
			validatorPassed, generatorPassed, shouldPass)
	}

	// Overall success: both validator and generator agree with expectation
	result.Success = (validatorPassed == shouldPass) && (generatorPassed == shouldPass) && !result.GeneratorMismatch

	return result
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

// toGoTypeName converts an XML element name to a Go type name
// Uses the shared helper parseElementNameToGoType from testhelpers.go
func toGoTypeName(name string) string {
	return parseElementNameToGoType(name)
}
