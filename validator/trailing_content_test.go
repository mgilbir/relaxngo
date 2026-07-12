package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// A well-formed XML document has exactly one root element. Content after the
// root (a second root element, or non-whitespace text) must be rejected.
func TestTrailingContentAfterRoot(t *testing.T) {
	const schema = `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
		<start><element name="foo"><empty/></element></start>
	</grammar>`
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	v := NewValidator(g, DefaultOptions())

	cases := []struct {
		name    string
		doc     string
		wantErr bool // wantErr true => Validate returns a non-nil error (malformed doc)
	}{
		{"single root", `<foo/>`, false},
		{"trailing whitespace ok", "<foo/>\n  \t", false},
		{"trailing comment ok", `<foo/><!-- bye -->`, false},
		{"second root element", `<foo/><bar/>`, true},
		{"second matching root", `<foo/><foo/>`, true},
		{"trailing text", `<foo/>garbage`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Validate(strings.NewReader(tc.doc))
			if tc.wantErr && err == nil {
				t.Fatalf("expected a parse error for %q, got nil", tc.doc)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.doc, err)
			}
		})
	}
}
