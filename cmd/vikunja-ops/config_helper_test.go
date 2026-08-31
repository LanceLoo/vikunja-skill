package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func configureTest(t *testing.T, baseURL, token string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := fmt.Sprintf("VIKUNJA_URL=%s\nVIKUNJA_TOKEN=%s\n", baseURL, token)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}
