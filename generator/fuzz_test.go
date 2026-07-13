package generator_test

import (
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/generator"
	"github.com/mgilbir/relaxngo/rng"
)

// FuzzGenerateCode checks that type/code generation never panics on any
// parseable schema.
func FuzzGenerateCode(f *testing.F) {
	f.Add(`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><start><element name="a"><text/></element></start></grammar>`)
	f.Add(`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><start><element name="r"><attribute name="id"><text/></attribute><element name="c"><text/></element></element></start></grammar>`)
	f.Fuzz(func(t *testing.T, schema string) {
		g, err := rng.ParseSchema(strings.NewReader(schema))
		if err != nil {
			return
		}
		types, err := generator.GenerateTypes(g)
		if err != nil {
			return
		}
		_, _ = generator.GenerateCode(types, "p", schema, g)
	})
}
