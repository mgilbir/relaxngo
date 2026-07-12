package rng

import (
	"strings"
	"testing"
)

// TestContainerRefValidation checks that an undefined <ref> directly inside a
// content container (choice, group, interleave, optional, oneOrMore,
// zeroOrMore, mixed) is rejected at parse time, and that a ref to a defined
// name is accepted.
func TestContainerRefValidation(t *testing.T) {
	tmpl := func(inner string) string {
		return `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><ref name="root"/></start>
  <define name="root"><element name="r">` + inner + `</element></define>
  <define name="ok"><element name="o"><text/></element></define>
</grammar>`
	}

	undefinedCases := map[string]string{
		"choice":     `<choice><ref name="MISSING"/><text/></choice>`,
		"group":      `<group><ref name="MISSING"/></group>`,
		"interleave": `<interleave><ref name="MISSING"/><text/></interleave>`,
		"optional":   `<optional><ref name="MISSING"/></optional>`,
		"oneOrMore":  `<oneOrMore><ref name="MISSING"/></oneOrMore>`,
		"zeroOrMore": `<zeroOrMore><ref name="MISSING"/></zeroOrMore>`,
		"mixed":      `<mixed><ref name="MISSING"/></mixed>`,
	}
	for name, inner := range undefinedCases {
		t.Run("undefined/"+name, func(t *testing.T) {
			_, err := ParseSchema(strings.NewReader(tmpl(inner)))
			if err == nil {
				t.Errorf("%s: undefined container ref accepted; expected an error", name)
			} else if !strings.Contains(err.Error(), "undefined reference") {
				t.Errorf("%s: unexpected error %q", name, err)
			}
		})
	}

	definedCases := map[string]string{
		"choice":    `<choice><ref name="ok"/><text/></choice>`,
		"oneOrMore": `<oneOrMore><ref name="ok"/></oneOrMore>`,
		"optional":  `<optional><ref name="ok"/></optional>`,
	}
	for name, inner := range definedCases {
		t.Run("defined/"+name, func(t *testing.T) {
			if _, err := ParseSchema(strings.NewReader(tmpl(inner))); err != nil {
				t.Errorf("%s: defined container ref rejected: %v", name, err)
			}
		})
	}
}
