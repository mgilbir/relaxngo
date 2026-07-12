package rng

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskResolver_ReadsWithinBase(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "ok.rng"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(base, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.rng"), []byte("subfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A filename that merely contains ".." as a substring must be allowed.
	if err := os.WriteFile(filepath.Join(base, "a..b.rng"), []byte("dots"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &DiskResolver{BaseDir: base}
	for _, tc := range []struct {
		path string
		want string
	}{
		{"ok.rng", "hello"},
		{"sub/c.rng", "subfile"},
		{"a..b.rng", "dots"},
	} {
		got, err := r.ReadResource(tc.path)
		if err != nil {
			t.Errorf("ReadResource(%q) unexpected error: %v", tc.path, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("ReadResource(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestDiskResolver_RejectsTraversal(t *testing.T) {
	base := t.TempDir()
	// A secret file one level above the base directory.
	secret := filepath.Join(filepath.Dir(base), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	r := &DiskResolver{BaseDir: base}
	cases := []string{
		"../secret.txt",
		"../../etc/passwd",
		secret,          // absolute path outside base
		"/etc/passwd",   // absolute path outside base
		"sub/../../secret.txt",
	}
	for _, p := range cases {
		if _, err := r.ReadResource(p); err == nil {
			t.Errorf("ReadResource(%q) succeeded; expected containment rejection", p)
		}
	}
}

func TestDiskResolver_RejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(filepath.Dir(base), "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(target) })

	link := filepath.Join(base, "link.rng")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	r := &DiskResolver{BaseDir: base}
	if _, err := r.ReadResource("link.rng"); err == nil {
		t.Error("ReadResource followed a symlink escaping base; expected rejection")
	}
}

func TestDiskResolver_RejectsOversizeResource(t *testing.T) {
	base := t.TempDir()
	big := make([]byte, maxResourceBytes+10)
	if err := os.WriteFile(filepath.Join(base, "big.rng"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	r := &DiskResolver{BaseDir: base}
	_, err := r.ReadResource("big.rng")
	if err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Errorf("ReadResource(big) error = %v, want size-limit error", err)
	}
}
