package generator_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
	"github.com/mgilbir/relaxngo/validator"
)

// TestCase represents a single integration test case
type TestCase struct {
	Name        string
	Schema      string // RELAX NG schema content
	Payload     string // XML payload to test
	ShouldPass  bool   // Expected validation result
	Description string
}

// TestGeneratorIntegration validates that the code generator produces
// code that correctly accepts/rejects the same payloads as the validator
//
//nolint:funlen // Comprehensive integration test with many test cases
func TestGeneratorIntegration(t *testing.T) {
	testCases := []TestCase{
		{
			Name: "SimpleElement_Valid",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <element name="title">
        <text/>
      </element>
      <element name="author">
        <text/>
      </element>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<book>
  <title>The Great Gatsby</title>
  <author>F. Scott Fitzgerald</author>
</book>`,
			ShouldPass:  true,
			Description: "Valid simple element with required children",
		},
		{
			Name: "SimpleElement_MissingRequired",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <element name="title">
        <text/>
      </element>
      <element name="author">
        <text/>
      </element>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<book>
  <title>The Great Gatsby</title>
</book>`,
			ShouldPass:  false,
			Description: "Missing required author element",
		},
		{
			Name: "Attributes_Valid",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <attribute name="isbn">
        <text/>
      </attribute>
      <element name="title">
        <text/>
      </element>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<book isbn="978-0743273565">
  <title>The Great Gatsby</title>
</book>`,
			ShouldPass:  true,
			Description: "Valid element with required attribute",
		},
		{
			Name: "Attributes_Missing",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <attribute name="isbn">
        <text/>
      </attribute>
      <element name="title">
        <text/>
      </element>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<book>
  <title>The Great Gatsby</title>
</book>`,
			ShouldPass:  false,
			Description: "Missing required isbn attribute",
		},
		{
			Name: "Optional_Present",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <element name="title">
        <text/>
      </element>
      <optional>
        <element name="subtitle">
          <text/>
        </element>
      </optional>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<book>
  <title>The Great Gatsby</title>
  <subtitle>A Novel</subtitle>
</book>`,
			ShouldPass:  true,
			Description: "Optional element present",
		},
		{
			Name: "Optional_Absent",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <element name="title">
        <text/>
      </element>
      <optional>
        <element name="subtitle">
          <text/>
        </element>
      </optional>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<book>
  <title>The Great Gatsby</title>
</book>`,
			ShouldPass:  true,
			Description: "Optional element absent",
		},
		{
			Name: "ZeroOrMore_Multiple",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="library"/>
  </start>
  <define name="library">
    <element name="library">
      <zeroOrMore>
        <ref name="book"/>
      </zeroOrMore>
    </element>
  </define>
  <define name="book">
    <element name="book">
      <element name="title">
        <text/>
      </element>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<library>
  <book><title>Book 1</title></book>
  <book><title>Book 2</title></book>
  <book><title>Book 3</title></book>
</library>`,
			ShouldPass:  true,
			Description: "ZeroOrMore with multiple elements",
		},
		{
			Name: "ZeroOrMore_Zero",
			Schema: `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="library"/>
  </start>
  <define name="library">
    <element name="library">
      <zeroOrMore>
        <ref name="book"/>
      </zeroOrMore>
    </element>
  </define>
  <define name="book">
    <element name="book">
      <element name="title">
        <text/>
      </element>
    </element>
  </define>
</grammar>`,
			Payload: `<?xml version="1.0"?>
<library>
</library>`,
			ShouldPass:  true,
			Description: "ZeroOrMore with zero elements",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			runIntegrationTest(t, tc)
		})
	}
}

func runIntegrationTest(t *testing.T, tc TestCase) {
	// Step 1: Write schema to temp file
	schemaFile := filepath.Join(t.TempDir(), "schema.rng")
	if err := os.WriteFile(schemaFile, []byte(tc.Schema), 0o600); err != nil {
		t.Fatalf("Failed to write schema: %v", err)
	}

	// Step 2: Run validator
	validatorPassed := runValidator(t, schemaFile, tc.Payload)

	// Step 3: Generate code and run tests
	generatorPassed := runGenerator(t, schemaFile, tc.Payload)

	// Step 4: Compare results
	t.Logf("Test: %s", tc.Description)
	t.Logf("Expected to pass: %v", tc.ShouldPass)
	t.Logf("Validator passed: %v", validatorPassed)
	t.Logf("Generated code passed: %v", generatorPassed)

	// Verify validator expectation
	if validatorPassed != tc.ShouldPass {
		t.Errorf("Validator result mismatch: expected %v, got %v", tc.ShouldPass, validatorPassed)
	}

	// Verify generator matches validator
	if validatorPassed != generatorPassed {
		t.Errorf("Generator/Validator mismatch: validator=%v, generator=%v", validatorPassed, generatorPassed)
		t.Errorf("This indicates the generator produces code that behaves differently from the validator!")
	}
}

func runValidator(t *testing.T, schemaFile, payload string) bool {
	t.Helper()

	// Parse schema
	grammar, err := rng.ParseSchemaFile(schemaFile, filepath.Dir(schemaFile))
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	// Create validator
	val := validator.NewValidator(grammar, validator.DefaultOptions())

	// Validate payload
	reader := bytes.NewReader([]byte(payload))
	errors, err := val.Validate(reader)
	if err != nil {
		t.Logf("Validator error: %v", err)
		return false
	}

	if len(errors) > 0 {
		t.Logf("Validation errors (%d):", len(errors))
		for _, e := range errors {
			t.Logf("  - %s", e.Error())
		}
		return false
	}

	t.Logf("Validator: PASSED")
	return true
}

//nolint:funlen // Helper function with multiple setup and execution steps
func runGenerator(t *testing.T, schemaFile, payload string) bool {
	t.Helper()

	// Create temp directory for generated code
	tempDir := t.TempDir()
	t.Logf("Generated code directory: %s", tempDir)

	// Parse schema
	grammar, err := rng.ParseSchemaFile(schemaFile, filepath.Dir(schemaFile))
	if err != nil {
		t.Fatalf("Failed to parse schema for generation: %v", err)
	}

	// Generate types from grammar
	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		t.Fatalf("Failed to generate types: %v", err)
	}
	if len(types) == 0 {
		t.Fatalf("No types extracted from schema")
	}

	// Read schema content for embedding
	schemaContent, err := os.ReadFile(schemaFile) // #nosec G304
	if err != nil {
		t.Fatalf("Failed to read schema file: %v", err)
	}

	// Generate code
	code, err := generator.GenerateCode(types, "testpkg", string(schemaContent), grammar)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}

	// Find root element name - check both direct element and ref
	rootElementName := ""
	if grammar.Start.Element != nil {
		rootElementName = grammar.Start.Element.Name
	} else if grammar.Start.Ref != nil {
		// Root is a ref, find the element in the define
		for _, def := range grammar.Defines {
			if def.Name == grammar.Start.Ref.Name {
				elem := def.FirstElement()
				if elem != nil {
					rootElementName = elem.Name
				}
				break
			}
		}
	}
	if rootElementName == "" {
		t.Fatalf("Could not determine root element name from schema")
	}
	t.Logf("Root element: %s", rootElementName)

	// Write generated code
	generatedFile := filepath.Join(tempDir, "types.go")
	if err := os.WriteFile(generatedFile, []byte(code), 0o600); err != nil {
		t.Fatalf("Failed to write generated code: %v", err)
	}

	// Create go.mod with replace directive to find relaxngo package
	// Go up one level from where we are to reach the project root
	cwd, _ := os.Getwd()
	moduleRoot := filepath.Join(cwd, "..")
	goModContent := fmt.Sprintf(`module testpkg

go 1.21

require github.com/mgilbir/relaxngo v0.0.0

replace github.com/mgilbir/relaxngo => %s
`, moduleRoot)
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0o600); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	// Run go mod tidy to ensure all dependencies are resolved
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tempDir
	tidyOutput, _ := tidyCmd.CombinedOutput()
	t.Logf("go mod tidy output: %s", tidyOutput)

	// Determine the root type name from the generated code
	rootTypeName := toGoTypeNameInt(rootElementName)

	// Create test file that uses the generated types
	testContent := fmt.Sprintf(`package testpkg

import (
	"encoding/xml"
	"testing"
)

func TestUnmarshal(t *testing.T) {
	payload := %s

	var root %s
	err := xml.Unmarshal([]byte(payload), &root)
	if err != nil {
		t.Fatalf("Failed to unmarshal into generated type: %%v", err)
	}
	t.Logf("Successfully unmarshaled: %%+v", root)
}

func TestRoundtrip(t *testing.T) {
	payload := %s

	// Parse XML into generated type
	var root %s
	err := xml.Unmarshal([]byte(payload), &root)
	if err != nil {
		t.Fatalf("Failed to unmarshal into generated type: %%v", err)
	}

	// Serialize back to XML
	roundtripped, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal back to XML: %%v", err)
	}

	// Parse the roundtripped XML again
	var root2 %s
	err = xml.Unmarshal(roundtripped, &root2)
	if err != nil {
		t.Fatalf("Failed to unmarshal roundtripped XML: %%v", err)
	}

	// Re-marshal both objects and compare the XML output to normalize pointer differences
	reserializedOrig, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatalf("Failed to re-marshal original: %%v", err)
	}
	reserializedRoundtrip, err := xml.MarshalIndent(root2, "", "  ")
	if err != nil {
		t.Fatalf("Failed to re-marshal roundtrip: %%v", err)
	}

	if string(reserializedOrig) != string(reserializedRoundtrip) {
		t.Logf("Original:     %%+v", root)
		t.Logf("Roundtripped: %%+v", root2)
		t.Logf("Original XML:\n%%s", reserializedOrig)
		t.Logf("Roundtripped XML:\n%%s", reserializedRoundtrip)
		t.Errorf("Roundtrip mismatch: data lost or changed during serialization")
	}

	t.Logf("Roundtrip successful, no field loss detected")
	t.Logf("Original XML:\n%%s", payload)
	t.Logf("Roundtripped XML:\n%%s", roundtripped)
}
`, "`"+payload+"`", rootTypeName, "`"+payload+"`", rootTypeName, rootTypeName)

	testFile := filepath.Join(tempDir, "types_test.go")
	if err := os.WriteFile(testFile, []byte(testContent), 0o600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run go test
	cmd := exec.Command("go", "test", "-v")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(output))

	if err != nil {
		t.Logf("Generated code test: FAILED")
		return false
	}

	t.Logf("Generated code test: PASSED")
	return true
}

// TestGeneratorOutput verifies the generator produces valid Go code
//
//nolint:funlen // Comprehensive test validating generated code structure
func TestGeneratorOutput(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0" datatypeLibrary="http://www.w3.org/2001/XMLSchema-datatypes">
  <start>
    <ref name="book"/>
  </start>
  <define name="book">
    <element name="book">
      <attribute name="isbn">
        <text/>
      </attribute>
      <element name="title">
        <text/>
      </element>
      <element name="author">
        <text/>
      </element>
      <optional>
        <element name="year">
          <data type="int"/>
        </element>
      </optional>
    </element>
  </define>
</grammar>`

	// Write schema
	tempDir := t.TempDir()
	schemaFile := filepath.Join(tempDir, "schema.rng")
	if err := os.WriteFile(schemaFile, []byte(schema), 0o600); err != nil {
		t.Fatalf("Failed to write schema: %v", err)
	}

	// Parse and generate
	grammar, err := rng.ParseSchemaFile(schemaFile, tempDir)
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		t.Fatalf("Failed to generate types: %v", err)
	}

	schemaContent, err := os.ReadFile(schemaFile) // #nosec G304
	if err != nil {
		t.Fatalf("Failed to read schema file: %v", err)
	}

	code, err := generator.GenerateCode(types, "testpkg", string(schemaContent), grammar)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}

	// Verify generated code contains expected structures
	expectedPatterns := []string{
		"package testpkg",
		"type Book struct",
		"Isbn",
		"Title",
		"Author",
		"Year",
		"xml:",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(code, pattern) {
			t.Errorf("Generated code missing expected pattern: %q", pattern)
		}
	}

	t.Logf("Generated code:\n%s", code)
}

// toGoTypeNameInt converts an XML element name to a Go type name
// Uses the shared helper parseElementNameToGoType from testhelpers.go
func toGoTypeNameInt(name string) string {
	return parseElementNameToGoType(name)
}

// extractStructDef extracts a struct definition from generated code
func extractStructDef(code, typeName string) string {
	lines := strings.Split(code, "\n")
	var result []string
	inStruct := false

	for _, line := range lines {
		if strings.Contains(line, "type "+typeName+" struct") {
			inStruct = true
			result = append(result, line)
		} else if inStruct {
			result = append(result, line)
			if strings.TrimSpace(line) == "}" {
				break
			}
		}
	}

	return strings.Join(result, "\n")
}

// TestValidateMethod tests the generated Validate() method on root types
//
// This test verifies that:
// 1. The Validate() method is generated for root types
// 2. Valid objects pass validation without errors
// 3. The method works when objects are created directly (not from XML parsing)
//
// Note: Invalid object validation is tested in TestGeneratorIntegration
// which validates that UnmarshalXML rejects invalid XML during parsing.
// Here we focus on the positive case of programmatically created valid objects.
//
//nolint:funlen // Comprehensive test with multiple scenarios
func TestValidateMethod(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
	<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<start>
	<ref name="library"/>
	</start>
	<define name="library">
	<element name="library">
	<oneOrMore>
	<ref name="book"/>
	</oneOrMore>
	</element>
	</define>
	<define name="book">
	<element name="book">
	<element name="title">
	<text/>
	</element>
	</element>
	</define>
	</grammar>`

	// Create temp directory for generated code
	tempDir := t.TempDir()

	// Write schema to file
	schemaFile := filepath.Join(tempDir, "schema.rng")
	if err := os.WriteFile(schemaFile, []byte(schema), 0o600); err != nil {
		t.Fatalf("Failed to write schema: %v", err)
	}

	// Parse schema
	grammar, err := rng.ParseSchemaFile(schemaFile, tempDir)
	if err != nil {
		t.Fatalf("Failed to parse schema: %v", err)
	}

	// Generate types and code
	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		t.Fatalf("Failed to generate types: %v", err)
	}

	schemaContent, err := os.ReadFile(schemaFile) // #nosec G304
	if err != nil {
		t.Fatalf("Failed to read schema file: %v", err)
	}

	code, err := generator.GenerateCode(types, "testpkg", string(schemaContent), grammar)
	if err != nil {
		t.Fatalf("Failed to generate code: %v", err)
	}

	// Verify Validate method is in generated code (for Library, the root type)
	if !strings.Contains(code, "func (x *Library) Validate() error {") {
		t.Fatalf("Generated code missing Validate() method for Library")
	}

	// Log the generated struct definitions for debugging
	t.Logf("Generated Library struct:\n%s", extractStructDef(code, "Library"))
	t.Logf("Generated Book struct:\n%s", extractStructDef(code, "Book"))

	// Write generated code
	generatedFile := filepath.Join(tempDir, "types.go")
	if err := os.WriteFile(generatedFile, []byte(code), 0o600); err != nil {
		t.Fatalf("Failed to write generated code: %v", err)
	}

	// Set up go.mod
	cwd, _ := os.Getwd()
	moduleRoot := filepath.Join(cwd, "..")
	goModContent := fmt.Sprintf(`module testpkg

go 1.21

require github.com/mgilbir/relaxngo v0.0.0

replace github.com/mgilbir/relaxngo => %s
`, moduleRoot)
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0o600); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	// Run go mod tidy
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tempDir
	if _, err := tidyCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: go mod tidy failed: %v", err)
	}

	// Create test file that tests Validate() on both valid and invalid objects
	testContent := fmt.Sprintf(`package testpkg

import (
	"testing"
)

func TestValidate_ValidLibrary(t *testing.T) {
	// Create a valid library with multiple books
	lib := &Library{
		Book: []Book{
			{Title: "Book 1"},
			{Title: "Book 2"},
			{Title: "Book 3"},
		},
	}

	// Call Validate() - should not return an error
	err := lib.Validate()
	if err != nil {
		t.Fatalf("Validate() failed for valid library: %%v", err)
	}
	t.Logf("Valid library passed validation")
}

func TestValidate_InvalidLibrary_EmptyBooks(t *testing.T) {
	// Create a library with no books (violates oneOrMore requirement)
	lib := &Library{
		Book: []Book{}, // Empty - schema requires oneOrMore
	}

	// Call Validate() - should fail because schema requires at least one book
	err := lib.Validate()
	if err == nil {
		t.Fatalf("Validate() should have failed for library with no books")
	}
	t.Logf("Validate() correctly failed for empty library: %%v", err)
}

func TestValidate_InvalidLibrary_NoBooks(t *testing.T) {
	// Create a library without initializing Book slice (nil/uninitialized violates oneOrMore)
	lib := &Library{
		// Book field not set - will be nil or empty
	}

	// Call Validate() - should fail because schema requires at least one book
	err := lib.Validate()
	if err == nil {
		t.Fatalf("Validate() should have failed for library with nil books")
	}
	t.Logf("Validate() correctly failed for library without books: %%v", err)
}
`,
	)

	testFile := filepath.Join(tempDir, "types_test.go")
	if err := os.WriteFile(testFile, []byte(testContent), 0o600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run go test
	cmd := exec.Command("go", "test", "-v")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	t.Logf("Test output:\n%s", string(output))

	if err != nil {
		t.Fatalf("Validate() tests failed: %v", err)
	}

	t.Logf("Validate() method tests passed successfully")
}
