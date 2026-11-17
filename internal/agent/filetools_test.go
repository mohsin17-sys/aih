package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafePathInside(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.txt", "./a.txt", "sub/../a.txt", "new/nested/file.txt", ""} {
		if _, err := safePath(dir, p); err != nil {
			t.Errorf("safePath(%q) = %v, want nil", p, err)
		}
	}
}

func TestSafePathRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"../escape.txt", "../../etc/passwd", "a/../../b", "/etc/passwd"} {
		if _, err := safePath(dir, p); err == nil {
			t.Errorf("safePath(%q) = nil, want error", p)
		}
	}
}

func TestSafePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir() // another directory the path must not reach
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	// direct symlink traversal and a symlinked subdirectory escape
	for _, p := range []string{"link/secret.txt", "link"} {
		if _, err := safePath(dir, p); err == nil {
			t.Errorf("safePath(%q) = nil, want error (symlink escape)", p)
		}
	}
	// a symlink string component that Join collapses lexically maps onto an
	// in-repo path; the caller operates on that joined path, which is contained.
	if _, err := safePath(dir, "link/../secret.txt"); err != nil {
		t.Errorf("safePath(\"link/../secret.txt\") = %v, want nil (lexically contained)", err)
	}
}
