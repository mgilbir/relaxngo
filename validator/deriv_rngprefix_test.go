package validator

import "testing"

// A schema that binds the RELAX NG namespace to a prefix (<rng:grammar>,
// <rng:element>, …) must validate on the derivative engine.
func TestDeriv_PrefixedRelaxNGNamespace(t *testing.T) {
	v := mustValidator(t, `<rng:grammar xmlns:rng="http://relaxng.org/ns/structure/1.0">
  <rng:start>
    <rng:element name="foo"><rng:text/></rng:element>
  </rng:start>
</rng:grammar>`)
	valid(t, v, `<foo>hi</foo>`)
	invalid(t, v, `<bar>hi</bar>`)
}

// The RELAX NG prefix plus a default namespace for foreign annotations: the
// annotations are ignored and the target element is in no namespace.
func TestDeriv_PrefixedRelaxNGWithForeignDefault(t *testing.T) {
	v := mustValidator(t, `<rng:grammar xmlns:rng="http://relaxng.org/ns/structure/1.0" xmlns="urn:annot">
  <note/>
  <rng:start>
    <note/>
    <rng:element name="foo"><rng:text/></rng:element>
  </rng:start>
</rng:grammar>`)
	valid(t, v, `<foo>hi</foo>`)
	invalid(t, v, `<bar>hi</bar>`)
}
