package generator_test

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

func fieldType(t *testing.T, schema, typeName, fieldName string) string {
	t.Helper()
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	types, err := generator.GenerateTypes(g)
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	for _, ty := range types {
		if ty.Name != typeName {
			continue
		}
		for _, f := range ty.Fields {
			if f.Name == fieldName {
				return f.Type
			}
		}
	}
	t.Fatalf("field %s.%s not found in %+v", typeName, fieldName, types)
	return ""
}

// An attribute's Go type must come from the attribute's own content, not from a
// <list> in the enclosing element. A plain text attribute alongside an
// element-level list previously became []int64 and failed to unmarshal.
func TestAttributeTypeIgnoresElementList(t *testing.T) {
	const schema = `<grammar xmlns="http://relaxng.org/ns/structure/1.0" datatypeLibrary="http://www.w3.org/2001/XMLSchema-datatypes">
		<start><element name="e">
			<attribute name="a"><text/></attribute>
			<list><data type="int"/></list>
		</element></start>
	</grammar>`
	if got := fieldType(t, schema, "E", "A"); got != "string" {
		t.Errorf("plain attribute type = %q, want string", got)
	}
}

// An attribute that itself contains a <list> maps to a slice.
func TestAttributeWithOwnListIsSlice(t *testing.T) {
	const schema = `<grammar xmlns="http://relaxng.org/ns/structure/1.0" datatypeLibrary="http://www.w3.org/2001/XMLSchema-datatypes">
		<start><element name="e">
			<attribute name="a"><list><data type="int"/></list></attribute>
		</element></start>
	</grammar>`
	if got := fieldType(t, schema, "E", "A"); got != "[]int64" {
		t.Errorf("list attribute type = %q, want []int64", got)
	}
}
