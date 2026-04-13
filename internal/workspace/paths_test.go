package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathValid(t *testing.T) {
	dir := t.TempDir()

	// Create a file to resolve.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolvePath(dir, "file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "file.txt")
	// Compare via EvalSymlinks since TempDir may itself be a symlink (e.g., macOS /tmp).
	wantResolved, _ := filepath.EvalSymlinks(want)
	if resolved != wantResolved {
		t.Errorf("got %q, want %q", resolved, wantResolved)
	}
}

func TestResolvePathNewFile(t *testing.T) {
	dir := t.TempDir()

	// File doesn't exist yet — should still resolve via parent.
	resolved, err := ResolvePath(dir, "newfile.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dirResolved, _ := filepath.EvalSymlinks(dir)
	want := filepath.Join(dirResolved, "newfile.txt")
	if resolved != want {
		t.Errorf("got %q, want %q", resolved, want)
	}
}

func TestResolvePathRejectsAbsolute(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolvePath(dir, "/etc/passwd")
	if err == nil {
		t.Error("expected error for absolute path")
	}
}

func TestResolvePathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		"../outside",
		"subdir/../../outside",
		"foo/../../../etc/passwd",
	}
	for _, p := range paths {
		_, err := ResolvePath(dir, p)
		if err == nil {
			t.Errorf("expected error for traversal path %q", p)
		}
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside workspace pointing outside.
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	_, err := ResolvePath(dir, "escape/secret.txt")
	if err == nil {
		t.Error("expected error for symlink escape")
	}
}

func TestResolvePathAllowsSymlinkWithin(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "data.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(sub, link); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := ResolvePath(dir, "link/data.txt")
	if err != nil {
		t.Errorf("symlink within workspace should be allowed: %v", err)
	}
}

func TestResolvePathSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolvePath(dir, "a/b/f.txt")
	if err != nil {
		t.Errorf("subdirectory path should be allowed: %v", err)
	}
}

func TestResolvePathDeepNewDirectories(t *testing.T) {
	dir := t.TempDir()

	// Multiple non-existent levels: a/b/c.txt where neither a/ nor a/b/ exist.
	resolved, err := ResolvePath(dir, "a/b/c.txt")
	if err != nil {
		t.Fatalf("deep new path should resolve: %v", err)
	}
	dirResolved, _ := filepath.EvalSymlinks(dir)
	want := filepath.Join(dirResolved, "a", "b", "c.txt")
	if resolved != want {
		t.Errorf("got %q, want %q", resolved, want)
	}
}
