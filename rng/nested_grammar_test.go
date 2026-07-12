package rng

import (
	"os"
	"strings"
	"testing"
)

// TestSerializeNestedGrammar_PreservesOuterElement checks that a nested grammar
// wrapped in an element round-trips through SerializeGrammar with the outer
// element intact (previously the outer element was dropped).
func TestSerializeNestedGrammar_PreservesOuterElement(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="foo"/></start>
  <define name="foo">
    <element name="outerFoo">
      <grammar>
        <start><ref name="foo"/></start>
        <define name="foo"><element name="innerFoo"><empty/></element></define>
      </grammar>
    </element>
  </define>
</grammar>`

	g, err := ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	serialized := SerializeGrammar(g)
	if !strings.Contains(serialized, `<element name="outerFoo">`) {
		t.Errorf("serialized schema dropped the outer element:\n%s", serialized)
	}

	// The serialized schema must itself re-parse.
	if _, err := ParseSchema(strings.NewReader(serialized)); err != nil {
		t.Fatalf("re-parse of serialized schema failed: %v", err)
	}
}

// mapResolver is a trivial in-memory ResourceResolver for tests.
type mapResolver map[string]string

func (m mapResolver) ReadResource(path string) ([]byte, error) {
	if s, ok := m[path]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

// TestParse_StartElementWrappingNestedGrammar checks that, via the resolver
// parse path, an element wrapping a nested grammar in the start pattern keeps
// the wrapping element (previously the start was replaced by the inner grammar's
// start, dropping the element).
func TestParse_StartElementWrappingNestedGrammar(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start>
    <element name="foo">
      <grammar><start><text/></start></grammar>
    </element>
  </start>
</grammar>`
	g, err := ParseSchemaWithResolver("main.rng", mapResolver{"main.rng": schema})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.Start.Element == nil {
		t.Fatal("start element was dropped; expected the wrapping <element name=\"foo\">")
	}
	if g.Start.Element.Name != "foo" {
		t.Errorf("start element = %q, want foo", g.Start.Element.Name)
	}
	if g.Start.Element.Text == nil {
		t.Error("expected the nested grammar's <text/> to become foo's content")
	}
}
