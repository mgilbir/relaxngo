package generator_test

import (
	"strings"
	"testing"
)

// G4: two <element name="item"> with different content must merge into one type
// that carries both variants' fields, rather than the second being dropped.
func TestGenerate_SameNameDifferentContentMerges(t *testing.T) {
	code := generate(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="root"/></start>
  <define name="root"><element name="root">
    <choice>
      <element name="item"><attribute name="id"><text/></attribute></element>
      <element name="item"><element name="name"><text/></element></element>
    </choice>
  </element></define>
</grammar>`)

	// The single Item type must carry fields from both variants.
	if !strings.Contains(code, "type Item struct") {
		t.Fatalf("expected a merged Item type:\n%s", code)
	}
	if !strings.Contains(code, `xml:"id,attr,omitempty"`) {
		t.Errorf("merged type lost the 'id' attribute variant:\n%s", code)
	}
	if !strings.Contains(code, `xml:"name,omitempty"`) {
		t.Errorf("merged type lost the 'name' element variant:\n%s", code)
	}
	// The choice field must reference the struct, not string (which would drop
	// the attribute/child content).
	if !strings.Contains(code, `*Item`) {
		t.Errorf("choice field should reference the Item struct, not string:\n%s", code)
	}
}
