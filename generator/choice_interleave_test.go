package generator_test

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

func generate(t *testing.T, schema string) string {
	t.Helper()
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
	return code
}

// G2: an element-level <choice> must produce fields for its branches instead of
// silently dropping them.
func TestGenerate_ChoiceProducesFields(t *testing.T) {
	code := generate(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="root"/></start>
  <define name="root"><element name="root">
    <choice>
      <element name="a"><text/></element>
      <element name="b"><text/></element>
    </choice>
  </element></define>
</grammar>`)

	for _, want := range []string{`xml:"a,omitempty"`, `xml:"b,omitempty"`} {
		if !strings.Contains(code, want) {
			t.Errorf("choice branch missing (%s) — data would be dropped:\n%s", want, code)
		}
	}
}

// G2: an element-level <interleave> must produce a field per child.
func TestGenerate_InterleaveProducesFields(t *testing.T) {
	code := generate(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="root"/></start>
  <define name="root"><element name="book">
    <interleave>
      <element name="title"><text/></element>
      <element name="author"><text/></element>
    </interleave>
  </element></define>
</grammar>`)

	for _, want := range []string{`xml:"title"`, `xml:"author"`} {
		if !strings.Contains(code, want) {
			t.Errorf("interleave child missing (%s) — data would be dropped:\n%s", want, code)
		}
	}
}
