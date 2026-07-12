package validator

import (
	"testing"
)

// Foreign-namespace annotation elements must be ignored, not treated as
// patterns (which previously forced a fallback).
func TestDeriv_ForeignAnnotationsIgnored(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0" xmlns:a="urn:annot">
<a:doc>schema annotation</a:doc>
<start>
  <a:note/>
  <element name="r"><a:info/><text/></element>
</start>
</grammar>`)
	valid(t, v, `<r>hi</r>`)
	invalid(t, v, `<other>hi</other>`)
}

// A structured (combine-built) element using an <anyName> name class with an
// <except> must validate via the derivative engine.
func TestDeriv_AnyNameExcept(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="x"/></start>
<define name="x" combine="choice">
  <element><anyName><except><name>bad</name></except></anyName><empty/></element>
</define>
<define name="x" combine="choice">
  <element name="keep"><empty/></element>
</define>
</grammar>`)
	valid(t, v, `<good/>`)  // matches anyName (not "bad")
	valid(t, v, `<keep/>`)  // matches the other alternative
	invalid(t, v, `<bad/>`) // excluded by the except
}

// A structured element using an <nsName> name class validates by namespace.
func TestDeriv_NsNameClass(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="x"/></start>
<define name="x" combine="choice">
  <element><nsName ns="urn:x"/><empty/></element>
</define>
<define name="x" combine="choice">
  <element name="plain"><empty/></element>
</define>
</grammar>`)
	valid(t, v, `<anything xmlns="urn:x"/>`) // any name in urn:x
	valid(t, v, `<plain/>`)
	invalid(t, v, `<anything xmlns="urn:other"/>`) // wrong namespace
}
