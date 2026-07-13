package rng

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A schema rooted at <externalRef> with no href is malformed. The error must be
// a clear message, not a formatting artifact from wrapping a nil error
// ("%!w(<nil>)").
func TestExternalRefMissingHrefClearError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.rng")
	if err := os.WriteFile(path, []byte(`<externalRef xmlns="http://relaxng.org/ns/structure/1.0"/>`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := ParseSchemaFile(path, dir)
	if err == nil {
		t.Fatal("expected an error for <externalRef> without href")
	}
	if strings.Contains(err.Error(), "%!w") || strings.Contains(err.Error(), "<nil>") {
		t.Fatalf("error wraps a nil error: %v", err)
	}
	if !strings.Contains(err.Error(), "href") {
		t.Fatalf("error should mention the missing href, got: %v", err)
	}
}
