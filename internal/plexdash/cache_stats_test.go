package plexdash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	bytes, n, newest, exists, err := statDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected exists")
	}
	if n != 1 || bytes != 5 {
		t.Fatalf("got files=%d bytes=%d", n, bytes)
	}
	if newest.IsZero() {
		t.Fatal("expected mtime")
	}
}

func TestStatDirectoryMissing(t *testing.T) {
	t.Parallel()
	bytes, n, newest, exists, err := statDirectory(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if exists || n != 0 || bytes != 0 || !newest.IsZero() {
		t.Fatalf("exists=%v n=%d bytes=%d newest=%v", exists, n, bytes, newest)
	}
}
