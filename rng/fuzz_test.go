package rng

import (
	"strings"
	"testing"
)

// FuzzParseSchema checks that parsing arbitrary input never panics; a malformed
// schema must return an error, not crash.
func FuzzParseSchema(f *testing.F) {
	f.Add(`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><start><element name="a"><text/></element></start></grammar>`)
	f.Add(`<element name="x" xmlns="http://relaxng.org/ns/structure/1.0"><empty/></element>`)
	f.Add(`<grammar><define name="d"><notAllowed/></define></grammar>`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, schema string) {
		_, _ = ParseSchema(strings.NewReader(schema))
	})
}
