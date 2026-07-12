package validator

import "testing"

// A prefixed attribute name (eg:a) must resolve via the namespace declared on
// the element, and validate on the derivative engine.
func TestDeriv_PrefixedAttributeName(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="x"/></start>
<define name="x" combine="choice">
  <element name="r" xmlns:eg="urn:eg"><attribute name="eg:a"><text/></attribute></element>
</define>
<define name="x" combine="choice"><element name="other"><empty/></element></define>
</grammar>`)
	valid(t, v, `<r xmlns:eg="urn:eg" eg:a="1"/>`)
	invalid(t, v, `<r a="1"/>`) // attribute not in the urn:eg namespace
}

// The always-bound xml: prefix resolves to the XML namespace.
func TestDeriv_XmlPrefixAttribute(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="x"/></start>
<define name="x" combine="choice">
  <element name="r"><attribute name="xml:lang"><text/></attribute></element>
</define>
<define name="x" combine="choice"><element name="other"><empty/></element></define>
</grammar>`)
	valid(t, v, `<r xml:lang="en"/>`)
	invalid(t, v, `<r lang="en"/>`) // "lang" without the xml namespace
}

// A choice-of-names name class validates on the derivative engine.
func TestDeriv_ChoiceOfNames(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="x"/></start>
<define name="x" combine="choice">
  <element><choice><name>foo</name><name>bar</name></choice><empty/></element>
</define>
<define name="x" combine="choice"><element name="other"><empty/></element></define>
</grammar>`)
	valid(t, v, `<foo/>`)
	valid(t, v, `<bar/>`)
	valid(t, v, `<other/>`)
	invalid(t, v, `<baz/>`)
}
