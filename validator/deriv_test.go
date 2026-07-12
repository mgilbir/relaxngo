package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// mustValidator parses a schema and returns a validator that uses the
// derivative engine (fails the test if the grammar was not translated).
func mustValidator(t *testing.T, schema string) *Validator {
	t.Helper()
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	v := NewValidator(g, DefaultOptions())
	if v.deriv == nil {
		t.Fatalf("derivative engine was not used for this schema")
	}
	return v
}

func valid(t *testing.T, v *Validator, doc string) {
	t.Helper()
	errs, err := v.Validate(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Validate(%q) error: %v", doc, err)
	}
	if len(errs) != 0 {
		t.Errorf("Validate(%q): expected valid, got errors: %v", doc, errs)
	}
}

func invalid(t *testing.T, v *Validator, doc string) {
	t.Helper()
	errs, err := v.Validate(strings.NewReader(doc))
	if err == nil && len(errs) == 0 {
		t.Errorf("Validate(%q): expected invalid, but it passed", doc)
	}
}

// V1: attributes below the root element are validated.
func TestDeriv_NonRootAttributes(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="r"/></start>
<define name="r"><element name="r">
  <element name="name"><attribute name="lang"><text/></attribute><text/></element>
</element></define></grammar>`)
	valid(t, v, `<r><name lang="en">x</name></r>`)
	invalid(t, v, `<r><name>x</name></r>`)                     // missing required attribute
	invalid(t, v, `<r><name lang="en" bogus="z">x</name></r>`) // unknown attribute
}

// V2: schema element order is enforced; optional/zeroOrMore before a sibling works.
func TestDeriv_ElementOrder(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="r"/></start>
<define name="r"><element name="root">
  <optional><element name="o"><text/></element></optional>
  <element name="m"><text/></element>
</element></define></grammar>`)
	valid(t, v, `<root><o>a</o><m>b</m></root>`)
	valid(t, v, `<root><m>b</m></root>`)           // optional absent
	invalid(t, v, `<root><o>a</o></root>`)         // required m missing
	invalid(t, v, `<root><m>b</m><o>a</o></root>`) // wrong order
}

// V3: a sibling <choice> is part of the content model.
func TestDeriv_SiblingChoice(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="r"/></start>
<define name="r"><element name="root">
  <choice><element name="a"><text/></element><element name="b"><text/></element></choice>
  <element name="c"><text/></element>
</element></define></grammar>`)
	valid(t, v, `<root><a>x</a><c>y</c></root>`)
	valid(t, v, `<root><b>x</b><c>y</c></root>`)
	invalid(t, v, `<root><c>y</c></root>`)                 // required choice missing
	invalid(t, v, `<root><a>x</a><b>x</b><c>y</c></root>`) // both branches
}

// V5: interleave allows arbitrary interleaving of repeatable particles.
func TestDeriv_Interleave(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="r"/></start>
<define name="r"><element name="root">
  <interleave>
    <oneOrMore><element name="a"><text/></element></oneOrMore>
    <element name="b"><text/></element>
  </interleave>
</element></define></grammar>`)
	valid(t, v, `<root><a>1</a><b>2</b><a>3</a></root>`) // b between a's
	valid(t, v, `<root><b>2</b><a>1</a></root>`)
	valid(t, v, `<root><a>1</a><a>3</a><b>2</b></root>`)
	invalid(t, v, `<root><a>1</a><a>3</a></root>`) // b missing
	invalid(t, v, `<root><b>1</b><b>2</b></root>`) // a missing, extra b
}

// V8: namespaces are checked below the root.
func TestDeriv_NamespaceBelowRoot(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="r"/></start>
<define name="r"><element name="root">
  <element name="c" ns="urn:a"><text/></element>
</element></define></grammar>`)
	valid(t, v, `<root><c xmlns="urn:a">x</c></root>`)
	invalid(t, v, `<root><c xmlns="urn:b">x</c></root>`) // wrong namespace
	invalid(t, v, `<root><c>x</c></root>`)               // no namespace
}
