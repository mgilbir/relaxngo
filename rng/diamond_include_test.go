package rng

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// A diamond include (main includes a and b, both of which include a common
// third file) is legal: the shared file is reached via two distinct paths, not
// a cycle. It must parse without a false "include cycle detected".
func TestDiamondIncludeAllowed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "common.rng",
		`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><define name="c"><text/></define></grammar>`)
	writeFile(t, dir, "a.rng",
		`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><include href="common.rng"/><define name="a"><element name="a"><ref name="c"/></element></define></grammar>`)
	writeFile(t, dir, "b.rng",
		`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><include href="common.rng"/><define name="b"><element name="b"><ref name="c"/></element></define></grammar>`)
	writeFile(t, dir, "main.rng",
		`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><include href="a.rng"/><include href="b.rng"/><start><ref name="a"/></start></grammar>`)

	if _, err := ParseSchemaFile(filepath.Join(dir, "main.rng"), dir); err != nil {
		t.Fatalf("diamond include should parse, got: %v", err)
	}
}

// A genuine include cycle (two files including each other) must still be
// rejected.
func TestTrueIncludeCycleRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cyc1.rng",
		`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><include href="cyc2.rng"/><define name="x"><text/></define></grammar>`)
	writeFile(t, dir, "cyc2.rng",
		`<grammar xmlns="http://relaxng.org/ns/structure/1.0"><include href="cyc1.rng"/><define name="y"><text/></define></grammar>`)

	if _, err := ParseSchemaFile(filepath.Join(dir, "cyc1.rng"), dir); err == nil {
		t.Fatal("expected an include-cycle error, got nil")
	}
}
