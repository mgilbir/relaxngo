package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgilbir/relaxngo/rng"
)

// TestDeriv_IncludeWithoutNamespace checks that a schema pulling in an <include>
// with no namespace is handled by the derivative engine and validates
// correctly, while an <include ns="..."> still falls back to the legacy engine.
func TestDeriv_IncludeWithoutNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "inc.rng", `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <start><element name="foo"><text/></element></start>
</grammar>`)

	t.Run("no namespace uses deriv", func(t *testing.T) {
		writeFile(t, dir, "main.rng", `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <include href="inc.rng"/>
</grammar>`)
		g, err := rng.ParseSchemaFile(filepath.Join(dir, "main.rng"), dir)
		if err != nil {
			t.Fatal(err)
		}
		v := NewValidator(g, DefaultOptions())
		if v.deriv == nil {
			t.Error("include without namespace should use the derivative engine")
		}
		valid(t, v, `<foo>hi</foo>`)
		invalid(t, v, `<bar>hi</bar>`)
	})

	t.Run("with namespace applies the include namespace", func(t *testing.T) {
		writeFile(t, dir, "mainns.rng", `<grammar xmlns="http://relaxng.org/ns/structure/1.0">
  <include href="inc.rng" ns="urn:x"/>
</grammar>`)
		g, err := rng.ParseSchemaFile(filepath.Join(dir, "mainns.rng"), dir)
		if err != nil {
			t.Fatal(err)
		}
		v := NewValidator(g, DefaultOptions())
		if v.deriv == nil {
			t.Fatal("include with namespace should build from structured fields on the derivative engine")
		}
		valid(t, v, `<foo xmlns="urn:x">hi</foo>`)
		invalid(t, v, `<foo>hi</foo>`) // element must be in urn:x
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.TrimSpace(content)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDeriv_ElementRootNestedGrammar checks that an element-root schema whose
// content is a nested grammar (unpacked by the parser into structured fields
// with empty RawContent) is built from those structured fields and validates.
func TestDeriv_ElementRootNestedGrammar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "s.rng", `<element xmlns="http://relaxng.org/ns/structure/1.0" name="foo">
  <grammar><start><text/></start></grammar>
</element>`)
	g, err := rng.ParseSchemaFile(filepath.Join(dir, "s.rng"), dir)
	if err != nil {
		t.Fatal(err)
	}
	v := NewValidator(g, DefaultOptions())
	if v.deriv == nil {
		t.Fatal("element-root nested grammar should build on the derivative engine")
	}
	valid(t, v, `<foo>hi</foo>`)
	invalid(t, v, `<foo><bar/></foo>`)
}
