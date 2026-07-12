package rng

import (
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
