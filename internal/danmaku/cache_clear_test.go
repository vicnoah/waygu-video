package danmaku

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearCachedComments(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "dandanplay")
	if err := ClearCachedComments(cache); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	comments := filepath.Join(dir, strings.Repeat("a", 64)+".json.gz")
	unrelated := filepath.Join(dir, "unrelated.json.gz")
	for _, path := range []string{comments, unrelated} {
		if err := os.WriteFile(path, []byte("fixtures"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearCachedComments(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(comments); !os.IsNotExist(err) {
		t.Fatalf("cache entry still present: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file changed: %v", err)
	}
}
