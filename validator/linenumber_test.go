package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

const personSchema = `<?xml version="1.0" encoding="UTF-8"?>
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

func mustParseValidator(t *testing.T, schema string) *Validator {
	t.Helper()
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return NewValidator(g, DefaultOptions())
}

func TestLineNumberTracking(t *testing.T) {
	v := mustParseValidator(t, personSchema)

	t.Run("valid document has no errors", func(t *testing.T) {
		xml := "<?xml version=\"1.0\"?>\n" +
			"<person id=\"1\">\n" +
			"  <name>Alice</name>\n" +
			"  <age>30</age>\n" +
			"</person>"
		errs, err := v.Validate(strings.NewReader(xml))
		if err != nil {
			t.Fatalf("unexpected hard error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected no errors, got: %v", errs)
		}
	})

	t.Run("error is located at the offending line", func(t *testing.T) {
		// <age> appears on line 3 where <name> is required, so the error must be
		// reported against the <age> element on line 3.
		xml := "<?xml version=\"1.0\"?>\n" +
			"<person id=\"invalid\">\n" +
			"  <age>30</age>\n" +
			"</person>"
		errs, err := v.Validate(strings.NewReader(xml))
		if err != nil {
			t.Fatalf("unexpected hard error: %v", err)
		}
		if len(errs) == 0 {
			t.Fatal("expected a validation error, got none")
		}
		e := errs[0]
		if e.Line != 3 {
			t.Errorf("Line = %d, want 3", e.Line)
		}
		if !strings.Contains(e.Message, "age") {
			t.Errorf("Message = %q, want it to mention the offending element 'age'", e.Message)
		}
	})
}

func TestLineNumberInErrorMessage(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="item"/></start>
  <define name="item">
    <element name="item">
      <attribute name="id"><text/></attribute>
    </element>
  </define>
</grammar>`
	v := mustParseValidator(t, schema)

	// Missing the required 'id' attribute produces an error carrying a line.
	xml := "<?xml version=\"1.0\"?>\n<item/>"
	errs, err := v.Validate(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected a validation error, got none")
	}
	e := errs[0]
	if e.Line <= 0 {
		t.Fatalf("expected a positive line number, got %d", e.Line)
	}
	if msg := e.Error(); !strings.Contains(msg, "line") {
		t.Errorf("Error() = %q, want it to contain the 'line' keyword", msg)
	}
}

func TestLineTrackerIndependence(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="root"/></start>
  <define name="root">
    <element name="root">
      <element name="child"><text/></element>
    </element>
  </define>
</grammar>`
	v := mustParseValidator(t, schema)

	validDoc := "<?xml version=\"1.0\"?>\n<root>\n  <child>content</child>\n</root>"
	errs, err := v.Validate(strings.NewReader(validDoc))
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("valid document should not have errors: %v", errs)
	}
}

// BenchmarkLineTracking measures validation throughput on a small document.
func BenchmarkLineTracking(b *testing.B) {
	docXML := "<?xml version=\"1.0\"?>\n" +
		"<person id=\"123\">\n" +
		"  <name>John Doe</name>\n" +
		"  <age>30</age>\n" +
		"</person>"

	g, err := rng.ParseSchema(strings.NewReader(personSchema))
	if err != nil {
		b.Fatalf("ParseSchema error: %v", err)
	}
	v := NewValidator(g, DefaultOptions())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.Validate(strings.NewReader(docXML)); err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}
