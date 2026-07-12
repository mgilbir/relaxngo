package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// assertNameClass parses schema, validates xml, and asserts the outcome.
// wantValid=true means the document must validate with no errors; false means it
// must be rejected. A schema parse error or a hard validate error always fails
// the test — these cases exercise supported constructs, so neither should occur.
func assertNameClass(t *testing.T, schema, xml string, wantValid bool) {
	t.Helper()
	grammar, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	errs, err := NewValidator(grammar, DefaultOptions()).Validate(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("validate returned a hard error: %v", err)
	}
	switch {
	case wantValid && len(errs) > 0:
		t.Errorf("expected valid document, got errors: %v", errs)
	case !wantValid && len(errs) == 0:
		t.Error("expected invalid document, but validation passed")
	}
}

// TestAnyNameValidation covers <anyName/> as an element and attribute name class.
func TestAnyNameValidation(t *testing.T) {
	t.Run("anyName accepts any element", func(t *testing.T) {
		schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><element name="container">
    <zeroOrMore><element><anyName/><empty/></element></zeroOrMore>
  </element></start>
</grammar>`
		assertNameClass(t, schema, `<container><foo/><bar/><baz/></container>`, true)
	})

	t.Run("anyName accepts any attribute", func(t *testing.T) {
		schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><element name="container">
    <zeroOrMore><attribute><anyName/></attribute></zeroOrMore>
  </element></start>
</grammar>`
		assertNameClass(t, schema, `<container foo="bar" baz="qux"/>`, true)
	})
}

// TestNsNameValidation covers <nsName/> matching only elements in a namespace.
func TestNsNameValidation(t *testing.T) {
	const ns = "http://www.w3.org/1999/xhtml"
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><element name="html" ns="` + ns + `">
    <zeroOrMore><element><nsName ns="` + ns + `"/><empty/></element></zeroOrMore>
  </element></start>
</grammar>`

	t.Run("accepts elements in the namespace", func(t *testing.T) {
		assertNameClass(t, schema, `<html xmlns="`+ns+`"><head/><body/></html>`, true)
	})

	t.Run("rejects a child in a different namespace", func(t *testing.T) {
		xml := `<html xmlns="` + ns + `"><child xmlns="http://example.com/other"/></html>`
		assertNameClass(t, schema, xml, false)
	})
}

// TestNameClassExcept covers <anyName><except>…</except></anyName>.
func TestNameClassExcept(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><element name="root">
    <zeroOrMore>
      <element><anyName><except><name>forbidden</name></except></anyName><empty/></element>
    </zeroOrMore>
  </element></start>
</grammar>`

	t.Run("accepts names outside the exception", func(t *testing.T) {
		assertNameClass(t, schema, `<root><allowed/><alsogood/></root>`, true)
	})

	t.Run("rejects the excepted name", func(t *testing.T) {
		assertNameClass(t, schema, `<root><allowed/><forbidden/></root>`, false)
	})
}

// TestAttributeWildcards covers a fixed attribute plus a wildcard attribute set,
// e.g. an id attribute alongside arbitrary data-* attributes.
func TestAttributeWildcards(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><element name="div">
    <attribute name="id"><text/></attribute>
    <zeroOrMore><attribute><anyName/></attribute></zeroOrMore>
  </element></start>
</grammar>`

	t.Run("accepts fixed plus wildcard attributes", func(t *testing.T) {
		assertNameClass(t, schema, `<div id="main" data-toggle="modal" data-target="#myModal"/>`, true)
	})

	t.Run("rejects when the required attribute is missing", func(t *testing.T) {
		assertNameClass(t, schema, `<div data-toggle="modal"/>`, false)
	})
}
