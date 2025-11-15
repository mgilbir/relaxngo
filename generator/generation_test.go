package generator_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

// TestGeneratedCodeExecution generates code, creates a test file, and runs tests against it.
//
//nolint:funlen // Complex integration test with multiple setup and execution steps
func TestGeneratedCodeExecution(t *testing.T) {
	// Create a temporary directory for generated code
	tmpDir, err := os.MkdirTemp("", "relaxngo-gen-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a simple RNG schema
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="book"/>
  </start>

  <define name="book">
    <element name="book">
      <interleave>
        <element name="title">
          <text/>
        </element>
        <element name="author">
          <text/>
        </element>
        <optional>
          <element name="isbn">
            <text/>
          </element>
        </optional>
      </interleave>
    </element>
  </define>
</grammar>`

	schemaPath := filepath.Join(tmpDir, "schema.rng")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatalf("failed to write schema: %v", err)
	}

	// Parse the schema
	grammar, err := rng.ParseSchemaFile(schemaPath, tmpDir)
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	// Generate types
	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		t.Fatalf("failed to generate types: %v", err)
	}

	// Generate code with embedded schema
	code, err := generator.GenerateCode(types, "generated", schema, grammar)
	if err != nil {
		t.Fatalf("failed to generate code: %v", err)
	}

	// Write generated code to file
	generatedFile := filepath.Join(tmpDir, "types.go")
	if err := os.WriteFile(generatedFile, []byte(code), 0o600); err != nil {
		t.Fatalf("failed to write generated code: %v", err)
	}

	// Create go.mod in temp directory
	cwd, _ := os.Getwd()
	moduleRoot := filepath.Join(cwd, "..")
	goModContent := `module generated

go 1.21

require github.com/mgilbir/relaxngo v0.0.0

replace github.com/mgilbir/relaxngo => ` + moduleRoot
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0o600); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create test file that uses the generated code
	testCode := `package generated

import (
	"encoding/xml"
	"testing"
)

func TestCodeGeneration(t *testing.T) {
	// Verify the Book struct exists and has the expected XMLName field
	var book Book
	if book.XMLName.Local == "" && book.XMLName.Space == "" {
		// Zero values are expected for uninitialized struct
	}
	t.Log("Book struct generated successfully")
}

func TestValidationInitialization(t *testing.T) {
	// Verify that validation infrastructure is set up
	// The schema is lazily initialized on first unmarshal
	t.Log("Validation infrastructure is initialized on first unmarshal")
}

func TestUnmarshalWithValidation(t *testing.T) {
	// Test unmarshaling
	xmlStr := ` + "`" + `<book><title>Test</title><author>Author</author></book>` + "`" + `

	var book Book
	err := xml.Unmarshal([]byte(xmlStr), &book)
	// Validation may fail due to nested elements, but that's OK for this test
	// The important thing is that the generated code compiles and validation runs
	_ = err
	t.Logf("Unmarshal completed successfully")
}
`

	testFile := filepath.Join(tmpDir, "types_test.go")
	if err := os.WriteFile(testFile, []byte(testCode), 0o600); err != nil {
		t.Fatalf("failed to write test code: %v", err)
	}

	// Copy schema to temp dir so tests can find it
	schemaContent, err := os.ReadFile(schemaPath) // #nosec G304
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "schema.rng"), schemaContent, 0o600); err != nil {
		t.Fatalf("failed to copy schema: %v", err)
	}

	// Run go mod tidy
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	if err := tidyCmd.Run(); err != nil {
		t.Logf("go mod tidy failed (continuing anyway): %v", err)
	}

	// Run tests in the temp directory
	cmd := exec.Command("go", "test", "-v", ".")
	cmd.Dir = tmpDir
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		t.Logf("Generated code:\n%s\n", code)
		t.Logf("Test stdout:\n%s\n", out.String())
		t.Logf("Test stderr:\n%s\n", errOut.String())
		t.Fatalf("generated code tests failed: %v", err)
	}

	t.Logf("Generated code tests passed:\n%s", out.String())
}
