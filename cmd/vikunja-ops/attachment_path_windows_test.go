//go:build windows

package main

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsafeWindowsPath(t *testing.T) {
	parent := t.TempDir()
	safe := filepath.Join(parent, "out.bin")
	unsafe := map[string]string{
		`\\server\share\out.bin`: "UNC backslash",
		"//server/share/out.bin": "UNC forward slash",
		`\\?\C:\out.bin`:         "device namespace prefix",
		`\\.\PhysicalDrive0`:     "device namespace dot prefix",
		"//?/C:/out.bin":         "device namespace forward slash",
		"out.bin:stream":         "alternate data stream",
		`C:out.bin`:              "drive-relative path",
		`C:dir\out.bin`:          "drive-relative child path",
		filepath.Join(parent, "dir:name", "out.bin"): "colon in directory component",
		"NUL":         "reserved name",
		"nul.txt":     "reserved name with extension",
		"con":         "reserved name case-insensitive",
		"LPT1":        "reserved name",
		"COM9":        "reserved name",
		"name.":       "trailing dot",
		"name ":       "trailing space",
		"out.bin.":    "trailing dot after extension",
		"bad\x01name": "control character",
	}
	for path, why := range unsafe {
		if !unsafeWindowsPath(path) {
			t.Errorf("unsafeWindowsPath(%q) = false, want true (%s)", path, why)
		}
	}
	if unsafeWindowsPath(safe) {
		t.Errorf("unsafeWindowsPath(%q) = true, want false", safe)
	}
}

// A directory junction in the parent chain must be rejected even though
// Lstat does not report junctions as symlinks.
func TestWindowsJunctionInParentChainRejected(t *testing.T) {
	realDir := t.TempDir()
	linkParent := t.TempDir()
	junction := filepath.Join(linkParent, "junction")
	if err := exec.Command("cmd", "/c", "mklink", "/J", junction, realDir).Run(); err != nil {
		t.Skipf("cannot create junction in this environment: %v", err)
	}
	if !isReparsePoint(junction) {
		t.Skip("junction reparse attribute not visible in this environment")
	}
	if !pathComponentIsLink(junction) {
		t.Fatal("pathComponentIsLink(junction) = false, want true")
	}
	if pathComponentIsLink(realDir) {
		t.Fatal("pathComponentIsLink(realDir) = true, want false for an ordinary directory")
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()

	destination := filepath.Join(junction, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "重解析点") {
		t.Fatalf("run = %d, stdout %q, stderr %q, want pre-network rejection", code, stdout, stderr)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0", requests)
	}
	// The centralized revalidation rejects the same path at every stage.
	if err := checkDestinationAbsentAndParentSafe(destination); err == nil {
		t.Error("checkDestinationAbsentAndParentSafe accepted a junction parent")
	}
}
