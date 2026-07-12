package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// TestDeriv_CombineChoiceDefine checks that a define split across several
// combine="choice" definitions is handled by the derivative engine (not the
// legacy fallback) and validates correctly.
func TestDeriv_CombineChoiceDefine(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><ref name="x"/></start>
<define name="x" combine="choice"><element name="foo1"><empty/></element></define>
<define name="x" combine="choice"><element name="foo2"><empty/></element></define>
<define name="x"><element name="foo3"><empty/></element></define>
</grammar>`)
	valid(t, v, `<foo1/>`)
	valid(t, v, `<foo2/>`)
	valid(t, v, `<foo3/>`)
	invalid(t, v, `<foo4/>`)
}

// TestDeriv_CombineInterleaveDefine checks combine="interleave".
func TestDeriv_CombineInterleaveDefine(t *testing.T) {
	v := mustValidator(t, `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><element name="foo"><ref name="x"/></element></start>
<define name="x" combine="interleave"><element name="bar1"><empty/></element></define>
<define name="x" combine="interleave"><element name="bar2"><empty/></element></define>
</grammar>`)
	valid(t, v, `<foo><bar1/><bar2/></foo>`)
	valid(t, v, `<foo><bar2/><bar1/></foo>`) // interleave: any order
	invalid(t, v, `<foo><bar1/></foo>`)      // bar2 missing
}

// TestDeriv_CombineChoiceStart checks combine on <start>.
func TestDeriv_CombineChoiceStart(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start combine="choice"><element name="foo1"><empty/></element></start>
<start combine="choice"><element name="foo2"><empty/></element></start>
</grammar>`
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatal(err)
	}
	v := NewValidator(g, DefaultOptions())
	if v.deriv == nil {
		t.Fatal("combine start should be handled by the derivative engine")
	}
	valid(t, v, `<foo1/>`)
	valid(t, v, `<foo2/>`)
	invalid(t, v, `<foo3/>`)
}
