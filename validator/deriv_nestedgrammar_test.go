package validator

import "testing"

// A grammar-root nested grammar (an element whose content is a <grammar>) must
// validate on the derivative engine.
func TestDeriv_GrammarRootNestedGrammar(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="foo"/></start>
<define name="foo">
  <element name="outerFoo">
    <grammar>
      <start><ref name="foo"/></start>
      <define name="foo"><element name="innerFoo"><empty/></element></define>
    </grammar>
  </element>
</define>
</grammar>`)
	valid(t, v, `<outerFoo><innerFoo/></outerFoo>`)
	invalid(t, v, `<innerFoo/>`)
	invalid(t, v, `<outerFoo/>`)
}

// A recursive nested grammar (208-style: element foo defined via a nested
// grammar referring to itself) must validate on the derivative engine.
func TestDeriv_RecursiveNestedGrammar(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="foo"/></start>
<define name="foo">
  <grammar>
    <start><ref name="foo"/></start>
    <define name="foo"><element name="foo"><empty/></element></define>
  </grammar>
</define>
</grammar>`)
	valid(t, v, `<foo/>`)
	invalid(t, v, `<bar/>`)
}
