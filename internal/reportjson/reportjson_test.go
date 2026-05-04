package reportjson

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_createsFile(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	path, err := Write(map[string]any{"k": 1}, "unittest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != "reports" {
		t.Fatalf("dir: %s", filepath.Dir(path))
	}
}
