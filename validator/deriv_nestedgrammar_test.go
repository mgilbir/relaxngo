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

// A nested grammar using parentRef (a reference to the enclosing grammar's
// define) must validate on the derivative engine.
func TestDeriv_ParentRef(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start>
  <grammar>
    <start><ref name="foo"/></start>
    <define name="foo"><element name="innerFoo"><parentRef name="foo"/></element></define>
  </grammar>
</start>
<define name="foo"><element name="outerFoo"><empty/></element></define>
</grammar>`)
	valid(t, v, `<innerFoo><outerFoo/></innerFoo>`)
	invalid(t, v, `<outerFoo/>`)
	invalid(t, v, `<innerFoo/>`)
}

// A <div ns="..."> applies its namespace to the element names it contains; the
// derivative engine must honor it.
func TestDeriv_DivNamespace(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<div ns="urn:x">
  <start><ref name="foo"/></start>
  <define name="foo"><element name="foo"><empty/></element></define>
</div>
</grammar>`)
	valid(t, v, `<foo xmlns="urn:x"/>`)
	invalid(t, v, `<foo/>`) // wrong namespace
}
