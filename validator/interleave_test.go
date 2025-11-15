package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// runInterleaveTest validates an interleave test case
func runInterleaveTest(t *testing.T, schema, xml string, wantValid bool) {
	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	v := NewValidator(grammar, DefaultOptions())
	errors, err := v.Validate(strings.NewReader(xml))

	if err != nil {
		t.Fatalf("validation raised error: %v", err)
	}

	if wantValid && len(errors) > 0 {
		t.Errorf("expected valid but got errors: %v", errors)
	}
	if !wantValid && len(errors) == 0 {
		t.Errorf("expected invalid but validation passed")
	}
}

// interleaveThreeElementsSchema returns a schema with three interleaved elements
func interleaveThreeElementsSchema() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="metadata"/></start>
  <define name="metadata">
    <element name="metadata">
      <interleave>
        <element name="title"><text/></element>
        <element name="author"><text/></element>
        <element name="date"><text/></element>
      </interleave>
    </element>
  </define>
</grammar>`
}

// interleaveTwoElementsSchema returns a schema with two interleaved elements
func interleaveTwoElementsSchema() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="metadata"/></start>
  <define name="metadata">
    <element name="metadata">
      <interleave>
        <element name="title"><text/></element>
        <element name="author"><text/></element>
      </interleave>
    </element>
  </define>
</grammar>`
}

// interleaveOptionalSchema returns a schema with optional element
func interleaveOptionalSchema() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="metadata"/></start>
  <define name="metadata">
    <element name="metadata">
      <interleave>
        <element name="title"><text/></element>
        <element name="author"><text/></element>
        <optional><element name="date"><text/></element></optional>
      </interleave>
    </element>
  </define>
</grammar>`
}

// TestInterleaveValidation tests full interleave validation with any-order acceptance
func TestInterleaveValidation(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		xml       string
		wantValid bool
	}{
		{
			name:      "Interleave: correct order",
			schema:    interleaveThreeElementsSchema(),
			xml:       `<?xml version="1.0"?><metadata><title>My Book</title><author>John Doe</author><date>2023</date></metadata>`,
			wantValid: true,
		},
		{
			name:      "Interleave: reversed order",
			schema:    interleaveThreeElementsSchema(),
			xml:       `<?xml version="1.0"?><metadata><date>2023</date><author>John Doe</author><title>My Book</title></metadata>`,
			wantValid: true,
		},
		{
			name:      "Interleave: mixed order",
			schema:    interleaveThreeElementsSchema(),
			xml:       `<?xml version="1.0"?><metadata><author>John Doe</author><date>2023</date><title>My Book</title></metadata>`,
			wantValid: true,
		},
		{
			name:      "Interleave: duplicate element should fail",
			schema:    interleaveTwoElementsSchema(),
			xml:       `<?xml version="1.0"?><metadata><title>My Book</title><title>Another Title</title><author>John Doe</author></metadata>`,
			wantValid: false,
		},
		{
			name:      "Interleave: missing element should fail",
			schema:    interleaveThreeElementsSchema(),
			xml:       `<?xml version="1.0"?><metadata><title>My Book</title><author>John Doe</author></metadata>`,
			wantValid: false,
		},
		{
			name:      "Interleave with optional elements",
			schema:    interleaveOptionalSchema(),
			xml:       `<?xml version="1.0"?><metadata><author>John Doe</author><title>My Book</title></metadata>`,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runInterleaveTest(t, tt.schema, tt.xml, tt.wantValid)
		})
	}
}

// TestInterleaveWithAttributes tests interleave with attributes
func TestInterleaveWithAttributes(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="person"/>
  </start>
  <define name="person">
    <element name="person">
      <attribute name="id"><text/></attribute>
      <interleave>
        <element name="name"><text/></element>
        <element name="email"><text/></element>
      </interleave>
    </element>
  </define>
</grammar>`

	xml := `<?xml version="1.0"?>
<person id="123">
  <email>john@example.com</email>
  <name>John Doe</name>
</person>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("ParseSchema failed: %v", err)
	}
	v := NewValidator(grammar, DefaultOptions())
	errors, _ := v.Validate(strings.NewReader(xml))

	if len(errors) > 0 {
		t.Errorf("expected valid but got errors: %v", errors)
	}
}

// TestInterleaveNestedInRefs tests interleave referenced elements
func TestInterleaveNestedInRefs(t *testing.T) {
	schema := `<?xml version="1.0" encoding="UTF-8"?>
<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <ref name="document"/>
  </start>
  <define name="document">
    <element name="doc">
      <interleave>
        <ref name="header"/>
        <ref name="body"/>
      </interleave>
    </element>
  </define>
  <define name="header">
    <element name="header"><text/></element>
  </define>
  <define name="body">
    <element name="body"><text/></element>
  </define>
</grammar>`

	tests := []struct {
		name      string
		xml       string
		wantValid bool
	}{
		{
			name: "correct order",
			xml: `<?xml version="1.0"?>
<doc>
  <header>Top</header>
  <body>Content</body>
</doc>`,
			wantValid: true,
		},
		{
			name: "reversed order",
			xml: `<?xml version="1.0"?>
<doc>
  <body>Content</body>
  <header>Top</header>
</doc>`,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grammar, _ := rng.ParseSchema(strings.NewReader(schema))
			v := NewValidator(grammar, DefaultOptions())
			errors, _ := v.Validate(strings.NewReader(tt.xml))

			if tt.wantValid && len(errors) > 0 {
				t.Errorf("expected valid but got errors: %v", errors)
			}
		})
	}
}
