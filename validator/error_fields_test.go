package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

func mkValidatorEF(t *testing.T, schema string) *Validator {
	t.Helper()
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return NewValidator(g, DefaultOptions())
}

func firstErrorEF(t *testing.T, v *Validator, doc string) ValidationError {
	t.Helper()
	errs, err := v.Validate(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatalf("expected a validation error, got none")
	}
	return errs[0]
}

func TestErrorFieldsUnexpectedElement(t *testing.T) {
	v := mkValidatorEF(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="root">
			<element name="a"><empty/></element>
		</element></start>
	</grammar>`)
	// <b/> where <a/> is required.
	e := firstErrorEF(t, v, `<root><b/></root>`)
	if e.Path != "/root/b" {
		t.Errorf("Path = %q, want /root/b", e.Path)
	}
	if e.Found != "b" {
		t.Errorf("Found = %q, want b", e.Found)
	}
	if len(e.Expected) != 1 || e.Expected[0] != "a" {
		t.Errorf("Expected = %v, want [a]", e.Expected)
	}
}

func TestErrorFieldsMissingAttribute(t *testing.T) {
	v := mkValidatorEF(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="root">
			<attribute name="id"><text/></attribute>
		</element></start>
	</grammar>`)
	e := firstErrorEF(t, v, `<root/>`)
	if len(e.Expected) == 0 || e.Expected[0] != "id" {
		t.Errorf("Expected = %v, want it to contain id", e.Expected)
	}
}

func TestErrorFieldsRepeatedSiblingIndex(t *testing.T) {
	v := mkValidatorEF(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="root">
			<oneOrMore><element name="item"><text/></element></oneOrMore>
		</element></start>
	</grammar>`)
	// Second <item> has a disallowed child element instead of text.
	e := firstErrorEF(t, v, `<root><item>ok</item><item><bad/></item></root>`)
	if e.Path != "/root/item[2]/bad" {
		t.Errorf("Path = %q, want /root/item[2]/bad", e.Path)
	}
}
