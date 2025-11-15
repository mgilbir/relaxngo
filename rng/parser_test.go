package rng

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPersonName = "person"

func TestParseSchema_Simple(t *testing.T) {
	schemaXML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="person"/>
  </start>
  <define name="person">
    <element name="person">
      <attribute name="id"/>
    </element>
  </define>
</grammar>`

	grammar, err := ParseSchema(strings.NewReader(schemaXML))
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}

	if grammar.Start.Ref == nil {
		t.Error("Expected start ref to be non-nil")
	}

	if grammar.Start.Ref.Name != testPersonName {
		t.Errorf("Expected start ref name to be %q, got %q", testPersonName, grammar.Start.Ref.Name)
	}

	if len(grammar.Defines) != 1 {
		t.Errorf("Expected 1 define, got %d", len(grammar.Defines))
	}

	if grammar.Defines[0].Name != testPersonName {
		t.Errorf("Expected define name to be %q, got %q", testPersonName, grammar.Defines[0].Name)
	}
}

func TestParseSchemaFile_Simple(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	schemaPath := filepath.Join(cwd, "..", "testdata", "simple.rng")
	grammar, err := ParseSchemaFile(schemaPath, filepath.Dir(schemaPath))
	if err != nil {
		t.Fatalf("ParseSchemaFile failed: %v", err)
	}

	if len(grammar.Defines) == 0 {
		t.Error("Expected at least one define")
	}

	if grammar.Defines[0].Name != "person" {
		t.Errorf("Expected define name to be 'person', got '%s'", grammar.Defines[0].Name)
	}
}

func TestParseSchemaFile_WithIncludes(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	schemaPath := filepath.Join(cwd, "..", "testdata", "parent.rng")
	grammar, err := ParseSchemaFile(schemaPath, filepath.Dir(schemaPath))
	if err != nil {
		t.Fatalf("ParseSchemaFile failed: %v", err)
	}

	if len(grammar.Defines) < 2 {
		t.Errorf("Expected at least 2 defines (root + child), got %d", len(grammar.Defines))
	}

	defineNames := make(map[string]bool)
	for _, def := range grammar.Defines {
		defineNames[def.Name] = true
	}

	if !defineNames["root"] {
		t.Error("Expected 'root' define from parent schema")
	}

	if !defineNames["child"] {
		t.Error("Expected 'child' define from included schema")
	}
}

func TestParseSchemaFile_IncludeNotFound(t *testing.T) {
	schemaXML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <include href="nonexistent.rng"/>
</grammar>`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.rng")

	if err := os.WriteFile(tmpFile, []byte(schemaXML), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := ParseSchemaFile(tmpFile, tmpDir)
	if err == nil {
		t.Error("Expected error for nonexistent include file")
	}
}

func TestParseSchemaFile_WithExternalRef(t *testing.T) {
	tmpDir := t.TempDir()

	// Create external schema
	externalXML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<define name="external-elem">
		<element name="external"><text/></element>
	</define>
</grammar>`

	externalFile := filepath.Join(tmpDir, "external.rng")
	if err := os.WriteFile(externalFile, []byte(externalXML), 0600); err != nil {
		t.Fatal(err)
	}

	// Create main schema with externalRef
	mainXML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<start><ref name="external-elem"/></start>
	<externalRef href="external.rng"/>
</grammar>`

	mainFile := filepath.Join(tmpDir, "main.rng")
	if err := os.WriteFile(mainFile, []byte(mainXML), 0600); err != nil {
		t.Fatal(err)
	}

	grammar, err := ParseSchemaFile(mainFile, tmpDir)
	if err != nil {
		t.Fatalf("ParseSchemaFile failed: %v", err)
	}

	// Check that external-elem define was merged
	defineNames := make(map[string]bool)
	for _, def := range grammar.Defines {
		defineNames[def.Name] = true
	}

	if !defineNames["external-elem"] {
		t.Error("Expected 'external-elem' define from externalRef")
	}
}

func TestParseSchemaFile_ExternalRefNotFound(t *testing.T) {
	schemaXML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <externalRef href="nonexistent.rng"/>
</grammar>`

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.rng")

	if err := os.WriteFile(tmpFile, []byte(schemaXML), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := ParseSchemaFile(tmpFile, tmpDir)
	if err == nil {
		t.Error("Expected error for nonexistent externalRef file")
	}
}

func TestParseSchemaFile_MultipleExternalRefs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first external schema
	external1XML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<define name="elem1">
		<element name="first"><text/></element>
	</define>
</grammar>`

	ext1File := filepath.Join(tmpDir, "external1.rng")
	if err := os.WriteFile(ext1File, []byte(external1XML), 0600); err != nil {
		t.Fatal(err)
	}

	// Create second external schema
	external2XML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<define name="elem2">
		<element name="second"><text/></element>
	</define>
</grammar>`

	ext2File := filepath.Join(tmpDir, "external2.rng")
	if err := os.WriteFile(ext2File, []byte(external2XML), 0600); err != nil {
		t.Fatal(err)
	}

	// Create main schema with multiple externalRefs
	mainXML := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
	<start><ref name="elem1"/></start>
	<externalRef href="external1.rng"/>
	<externalRef href="external2.rng"/>
</grammar>`

	mainFile := filepath.Join(tmpDir, "main.rng")
	if err := os.WriteFile(mainFile, []byte(mainXML), 0600); err != nil {
		t.Fatal(err)
	}

	grammar, err := ParseSchemaFile(mainFile, tmpDir)
	if err != nil {
		t.Fatalf("ParseSchemaFile failed: %v", err)
	}

	// Check that both external defines were merged
	defineNames := make(map[string]bool)
	for _, def := range grammar.Defines {
		defineNames[def.Name] = true
	}

	if !defineNames["elem1"] {
		t.Error("Expected 'elem1' define from first externalRef")
	}
	if !defineNames["elem2"] {
		t.Error("Expected 'elem2' define from second externalRef")
	}
}

// TestIsValidNCName tests the NCName validation function
func TestIsValidNCName(t *testing.T) {
	tests := []struct {
		name    string
		ncName  string
		valid   bool
		comment string
	}{
		// Valid NCNames
		{"validSimple", "foo", true, "Simple letter-only name"},
		{"validWithUnderscore", "_foo", true, "Starting with underscore"},
		{"validWithHyphen", "foo-bar", true, "Contains hyphen"},
		{"validWithDot", "foo.bar", true, "Contains dot"},
		{"validWithDigit", "foo123", true, "Contains digits"},
		{"validQName", "foo:bar", true, "Valid qualified name"},
		{"validUnicode", "café", true, "Unicode letters"},
		{"nameWithCombining", "e\u0301", true, "Unicode combining mark after letter (valid per XML spec)"},

		// Invalid NCNames
		{"emptyString", "", false, "Empty string"},
		{"startsWithDigit", "1foo", false, "Starts with digit"},
		{"startsWithHyphen", "-foo", false, "Starts with hyphen"},
		{"startsWithDot", ".foo", false, "Starts with dot"},
		{"containsSpace", "foo bar", false, "Contains space"},
		{"multipleColons", "foo:bar:baz", false, "Multiple colons"},
		{"colonAtEnd", "foo:", false, "Colon at end"},
		{"colonAtStart", ":foo", false, "Colon at start"},
		{"unicodeCombining", "☃", false, "Unicode combining mark as first char"},
		{"combiningMarkOnly", "\u0301", false, "Combining mark as only character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidNCName(tt.ncName)
			if result != tt.valid {
				t.Errorf("isValidNCName(%q) = %v, expected %v (%s)", tt.ncName, result, tt.valid, tt.comment)
			}
		})
	}
}

// TestInvalidElementName tests that invalid element names are rejected
func TestInvalidElementName(t *testing.T) {
	tests := []struct {
		schemaXML  string
		shouldFail bool
		errorMatch string
	}{
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name="validName"><empty/></element></start>
			</grammar>`,
			shouldFail: false,
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name=""><empty/></element></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name="123invalid"><empty/></element></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name="foo bar"><empty/></element></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
	}

	for i, tt := range tests {
		_, err := ParseSchema(strings.NewReader(tt.schemaXML))
		switch {
		case tt.shouldFail && err == nil:
			t.Errorf("Test %d: Expected error but got none", i)
		case !tt.shouldFail && err != nil:
			t.Errorf("Test %d: Expected success but got error: %v", i, err)
		case tt.shouldFail && err != nil:
			if !strings.Contains(err.Error(), tt.errorMatch) {
				t.Errorf("Test %d: Expected error containing %q, got: %v", i, tt.errorMatch, err)
			}
		}
	}
}

// TestInvalidAttributeName tests that invalid attribute names are rejected
func TestInvalidAttributeName(t *testing.T) {
	tests := []struct {
		schemaXML  string
		shouldFail bool
		errorMatch string
	}{
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name="test">
					<attribute name="validName"><text/></attribute>
				</element></start>
			</grammar>`,
			shouldFail: false,
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name="test">
					<attribute name=""><text/></attribute>
				</element></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<start><element name="test">
					<attribute name="123attr"><text/></attribute>
				</element></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
	}

	for i, tt := range tests {
		_, err := ParseSchema(strings.NewReader(tt.schemaXML))
		switch {
		case tt.shouldFail && err == nil:
			t.Errorf("Test %d: Expected error but got none", i)
		case !tt.shouldFail && err != nil:
			t.Errorf("Test %d: Expected success but got error: %v", i, err)
		case tt.shouldFail && err != nil:
			if !strings.Contains(err.Error(), tt.errorMatch) {
				t.Errorf("Test %d: Expected error containing %q, got: %v", i, tt.errorMatch, err)
			}
		}
	}
}

// TestInvalidDefineName tests that invalid define names are rejected
func TestInvalidDefineName(t *testing.T) {
	tests := []struct {
		schemaXML  string
		shouldFail bool
		errorMatch string
	}{
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name="validName"><element name="test"><empty/></element></define>
				<start><ref name="validName"/></start>
			</grammar>`,
			shouldFail: false,
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name=""><element name="test"><empty/></element></define>
				<start><ref name=""/></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name="123def"><element name="test"><empty/></element></define>
				<start><ref name="123def"/></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
	}

	for i, tt := range tests {
		_, err := ParseSchema(strings.NewReader(tt.schemaXML))
		switch {
		case tt.shouldFail && err == nil:
			t.Errorf("Test %d: Expected error but got none", i)
		case !tt.shouldFail && err != nil:
			t.Errorf("Test %d: Expected success but got error: %v", i, err)
		case tt.shouldFail && err != nil:
			if !strings.Contains(err.Error(), tt.errorMatch) {
				t.Errorf("Test %d: Expected error containing %q, got: %v", i, tt.errorMatch, err)
			}
		}
	}
}

// TestInvalidRefName tests that invalid ref names are rejected
func TestInvalidRefName(t *testing.T) {
	tests := []struct {
		schemaXML  string
		shouldFail bool
		errorMatch string
	}{
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name="validName"><element name="test"><empty/></element></define>
				<start><ref name="validName"/></start>
			</grammar>`,
			shouldFail: false,
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name="validName"><element name="test"><empty/></element></define>
				<start><ref name=""/></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "must have a name",
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name="validName"><element name="test"><empty/></element></define>
				<start><ref name="123invalid"/></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
		{
			schemaXML: `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
				<define name="validName"><element name="test"><empty/></element></define>
				<start><ref name="foo bar"/></start>
			</grammar>`,
			shouldFail: true,
			errorMatch: "invalid name",
		},
	}

	for i, tt := range tests {
		_, err := ParseSchema(strings.NewReader(tt.schemaXML))
		switch {
		case tt.shouldFail && err == nil:
			t.Errorf("Test %d: Expected error but got none", i)
		case !tt.shouldFail && err != nil:
			t.Errorf("Test %d: Expected success but got error: %v", i, err)
		case tt.shouldFail && err != nil:
			if !strings.Contains(err.Error(), tt.errorMatch) {
				t.Errorf("Test %d: Expected error containing %q, got: %v", i, tt.errorMatch, err)
			}
		}
	}
}
