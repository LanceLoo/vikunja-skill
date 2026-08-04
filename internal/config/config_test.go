package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvironmentPrecedenceAndDotEnvFallback(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "VIKUNJA_URL=http://from-file/vikunja/\nVIKUNJA_TOKEN=file-token\n")
	withConfigEnvironment(t, dir, "https://from-environment/base/", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://from-environment/base" || cfg.Token != "file-token" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadMissingConfiguration(t *testing.T) {
	dir := t.TempDir()
	withConfigEnvironment(t, dir, "", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without configuration")
	}
}

func TestNormalizeURL(t *testing.T) {
	got, err := normalizeURL("https://example.test/vikunja/?q=secret#fragment")
	if err != nil || got != "https://example.test/vikunja" {
		t.Fatalf("normalizeURL() = %q, %v", got, err)
	}
	for _, raw := range []string{"example.test", "ftp://example.test", "/relative", "https:///missing-host"} {
		if _, err := normalizeURL(raw); err == nil {
			t.Errorf("normalizeURL(%q) accepted invalid URL", raw)
		}
	}
}

func TestReadDotEnvSyntax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("# comment\nexport VIKUNJA_URL='http://host/path'\nVIKUNJA_TOKEN=\"test-token\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := readDotEnv(path)
	if err != nil || values["VIKUNJA_URL"] != "http://host/path" || values["VIKUNJA_TOKEN"] != "test-token" {
		t.Fatalf("readDotEnv() = %#v, %v", values, err)
	}
	if err := os.WriteFile(path, []byte("VIKUNJA_TOKEN='unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDotEnv(path); err == nil {
		t.Fatal("readDotEnv accepted invalid syntax")
	}
}

func writeEnv(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func withConfigEnvironment(t *testing.T, dir, baseURL, token string) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIKUNJA_URL", baseURL)
	t.Setenv("VIKUNJA_TOKEN", token)
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
}
