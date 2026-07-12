package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

func recursiveSchema(t *testing.T) *rng.Grammar {
	// <a> may contain zero or more <a> children — permits arbitrary nesting.
	const schema = `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><ref name="a"/></start>
		<define name="a"><element name="a"><zeroOrMore><ref name="a"/></zeroOrMore></element></define>
	</grammar>`
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return g
}

func nested(depth int) string {
	return strings.Repeat("<a>", depth) + strings.Repeat("</a>", depth)
}

func TestMaxDepthRejectsDeepNesting(t *testing.T) {
	g := recursiveSchema(t)
	opts := DefaultOptions()
	opts.MaxDepth = 50
	v := NewValidator(g, opts)

	if _, err := v.Validate(strings.NewReader(nested(50))); err != nil {
		t.Fatalf("depth 50 within limit should not error: %v", err)
	}
	_, err := v.Validate(strings.NewReader(nested(51)))
	if err == nil {
		t.Fatal("depth 51 over limit should be rejected")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Fatalf("expected a depth error, got: %v", err)
	}
}

func TestMaxDepthUnlimited(t *testing.T) {
	g := recursiveSchema(t)
	opts := DefaultOptions()
	opts.MaxDepth = 0 // unlimited
	v := NewValidator(g, opts)
	if _, err := v.Validate(strings.NewReader(nested(2000))); err != nil {
		t.Fatalf("unlimited depth should accept deep nesting: %v", err)
	}
}

func TestMaxDocumentBytes(t *testing.T) {
	g := recursiveSchema(t)
	opts := DefaultOptions()
	opts.MaxDocumentBytes = 32
	v := NewValidator(g, opts)

	small := `<a></a>` // 7 bytes
	if _, err := v.Validate(strings.NewReader(small)); err != nil {
		t.Fatalf("small doc within limit should not error: %v", err)
	}
	big := nested(100) // far more than 32 bytes
	_, err := v.Validate(strings.NewReader(big))
	if err == nil {
		t.Fatal("document over MaxDocumentBytes should be rejected")
	}
	if !strings.Contains(err.Error(), "MaxDocumentBytes") {
		t.Fatalf("expected a size-limit error, got: %v", err)
	}
}
