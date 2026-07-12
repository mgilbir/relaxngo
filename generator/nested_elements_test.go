package generator_test

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

// TestGenerate_DirectlyNestedElements ensures that a directly nested element
// with its own complex content gets a generated type. Previously
// collectNestedElements did not recurse into direct child elements, so the
// output referenced an undefined type and did not compile.
func TestGenerate_DirectlyNestedElements(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="a"/></start>
  <define name="a">
    <element name="a">
      <element name="b">
        <element name="c"><text/></element>
      </element>
    </element>
  </define>
</grammar>`

	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	types, err := generator.GenerateTypes(grammar)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	code, err := generator.GenerateCode(types, "testpkg", schema, grammar)
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	// Every element referenced by a field must have a corresponding type.
	for _, want := range []string{"type A struct", "type B struct", "type C struct"} {
		if !strings.Contains(code, want) {
			t.Errorf("generated code missing %q; nested element type was not generated:\n%s", want, code)
		}
	}
}
