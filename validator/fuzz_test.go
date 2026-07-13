package validator

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// FuzzValidateXML checks that validating arbitrary documents against arbitrary
// (parseable) schemas never panics.
func FuzzValidateXML(f *testing.F) {
	f.Add(`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><start><element name="a"><text/></element></start></grammar>`, `<a>hi</a>`)
	f.Add(`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><start><element name="r"><attribute name="id"><text/></attribute></element></start></grammar>`, `<r id="1"/>`)
	f.Fuzz(func(t *testing.T, schema, doc string) {
		g, err := rng.ParseSchema(strings.NewReader(schema))
		if err != nil {
			return
		}
		_, _ = NewValidator(g, DefaultOptions()).Validate(strings.NewReader(doc))
	})
}
