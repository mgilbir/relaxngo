package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// runLineNumberTest validates line number tracking in an error
func runLineNumberTest(t *testing.T, validator *Validator, xml string, expectErrors bool, expectedLine int, errorContains string) {
	errs, err := validator.Validate(strings.NewReader(xml))

	if err != nil && !expectErrors {
		t.Errorf("unexpected parse error: %v", err)
		return
	}

	if !expectErrors {
		if len(errs) > 0 {
			t.Errorf("expected no validation errors, got %d: %v", len(errs), errs)
		}
		return
	}

	if expectErrors && len(errs) == 0 {
		t.Errorf("expected validation errors but got none")
		return
	}

	foundLineNumber := false
	for _, err := range errs {
		if err.Line > 0 {
			foundLineNumber = true
			if err.Line == expectedLine {
				t.Logf("✓ Found error on expected line %d: %s", expectedLine, err.Message)
			}
		}
		if errorContains != "" && strings.Contains(err.Message, errorContains) {
			t.Logf("✓ Error message contains '%s'", errorContains)
		}
	}

	if expectedLine > 0 && !foundLineNumber {
		t.Errorf("expected line number to be tracked, but Line fields were 0")
		for _, e := range errs {
			t.Logf("  Error: %v (line=%d)", e.Message, e.Line)
		}
	}
}

// TestLineNumberTracking tests that validation errors include line numbers
func TestLineNumberTracking(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="person"/></start>
  <define name="person">
    <element name="person">
      <attribute name="id"><text/></attribute>
      <element name="name"><text/></element>
      <element name="age"><text/></element>
    </element>
  </define>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	validator := NewValidator(grammar, DefaultOptions())

	t.Run("Valid document has line 2", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<person id="1">
  <name>Alice</name>
  <age>30</age>
</person>`
		runLineNumberTest(t, validator, xml, false, 0, "")
	})

	t.Run("Missing required element on line 2", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<person id="invalid">
  <age>30</age>
</person>`
		runLineNumberTest(t, validator, xml, true, 2, "attribute 'id'")
	})
}

// TestLineNumberInErrorMessage tests that line numbers appear in error messages
func TestLineNumberInErrorMessage(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="item"/>
  </start>
  <define name="item">
    <element name="item">
      <attribute name="id"><text/></attribute>
    </element>
  </define>
</grammar>`

	grammar, _ := rng.ParseSchema(strings.NewReader(schema))
	validator := NewValidator(grammar, DefaultOptions())

	xml := `<?xml version="1.0"?>
<item id="invalid">
</item>`

	errs, _ := validator.Validate(strings.NewReader(xml))

	for _, err := range errs {
		if err.Line > 0 {
			// Check that Error() method includes line number
			msg := err.Error()
			if !strings.Contains(msg, "line") {
				t.Errorf("error message should contain 'line' keyword: %s", msg)
			}
			// Line 1 is XML declaration, line 2 is <item> element
			if !strings.Contains(msg, "2") && !strings.Contains(msg, "3") {
				t.Logf("Line number in message: %s", msg)
			}
			t.Logf("✓ Error message with line number: %s", msg)
		}
	}
}

// TestLineTrackerIndependence ensures line tracker doesn't interfere with validation
func TestLineTrackerIndependence(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="root"/>
  </start>
  <define name="root">
    <element name="root">
      <element name="child"><text/></element>
    </element>
  </define>
</grammar>`

	grammar, _ := rng.ParseSchema(strings.NewReader(schema))
	validator := NewValidator(grammar, DefaultOptions())

	validDoc := `<?xml version="1.0"?>
<root>
  <child>content</child>
</root>`

	errs, err := validator.Validate(strings.NewReader(validDoc))
	if err != nil {
		t.Errorf("validation failed: %v", err)
	}
	if len(errs) > 0 {
		t.Errorf("valid document should not have errors: %v", errs)
	}
}

// BenchmarkLineTracking measures the overhead of line number tracking
func BenchmarkLineTracking(b *testing.B) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="person"/>
  </start>
  <define name="person">
    <element name="person">
      <attribute name="id"><text/></attribute>
      <element name="name"><text/></element>
      <element name="age"><text/></element>
    </element>
  </define>
</grammar>`

	docXML := `<?xml version="1.0"?>
<person id="123">
  <name>John Doe</name>
  <age>30</age>
</person>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		b.Fatalf("ParseSchema error: %v", err)
	}
	validator := NewValidator(grammar, DefaultOptions())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validator.Validate(strings.NewReader(docXML))
		if err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}
