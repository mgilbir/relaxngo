package generator_test

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

// TestGenerate_RefDefineNameDiffersFromElement ensures a ref whose define name
// differs from the wrapped element name generates a field typed after the
// element (with the element's XML tag), not the define name — otherwise the
// generated code references an undefined type.
func TestGenerate_RefDefineNameDiffersFromElement(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="root"/></start>
  <define name="root"><element name="root"><ref name="itemdef"/></element></define>
  <define name="itemdef"><element name="item"><text/></element></define>
</grammar>`

	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	types, err := generator.GenerateTypes(g)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	code, err := generator.GenerateCode(types, "testpkg", schema, g)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	// The referenced type is named after the element (Item), and the ref field
	// must use that type and the element's XML tag — not the define name.
	if !strings.Contains(code, "type Item struct") {
		t.Errorf("expected a type named after the element 'item':\n%s", code)
	}
	if strings.Contains(code, "type Itemdef struct") {
		t.Errorf("generated a bogus type from the define name 'itemdef':\n%s", code)
	}
	if !strings.Contains(code, `Itemdef Item`) {
		t.Errorf("ref field should be typed Item (the element), not Itemdef (the define):\n%s", code)
	}
	if !strings.Contains(code, `xml:"item"`) {
		t.Errorf("ref field should carry the element XML tag 'item':\n%s", code)
	}
}
