package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// TestValidate_EmptyDocument checks that input with no document element is
// reported as invalid rather than validating clean.
func TestValidate_EmptyDocument(t *testing.T) {
	schema := `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
<start><element name="r"><text/></element></start></grammar>`
	g, err := rng.ParseSchema(strings.NewReader(schema))
	if err != nil {
		t.Fatal(err)
	}
	v := NewValidator(g, DefaultOptions())

	for _, doc := range []string{
		"",
		"   \n\t ",
		`<?xml version="1.0"?>`,
		`<?xml version="1.0"?><!-- only a comment -->`,
	} {
		errs, err := v.Validate(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("Validate(%q) returned error: %v", doc, err)
		}
		if len(errs) == 0 {
			t.Errorf("Validate(%q) accepted an element-less document; expected an error", doc)
		}
	}

	// Sanity: a real document with the root element still validates.
	if errs, _ := v.Validate(strings.NewReader(`<r>hi</r>`)); len(errs) != 0 {
		t.Errorf("valid document reported errors: %v", errs)
	}
}
