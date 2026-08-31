package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunTopLevelHelpAndNoCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"--pretty", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%v) exit code = %d, want 0", args, code)
		}
		if !strings.Contains(stdout.String(), "用法:") {
			t.Fatalf("run(%v) stdout does not contain usage: %q", args, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("run(%v) stderr = %q, want empty", args, stderr.String())
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") || !strings.Contains(stderr.String(), "用法:") {
		t.Fatalf("stderr lacks error or usage: %q", stderr.String())
	}
}

func TestRunMalformedOrUnknownTopLevelFlag(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"--pretty=invalid"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%v) exit code = %d, want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "用法:") || strings.HasPrefix(stderr.String(), "vikunja-ops: Vikunja CLI") {
			t.Fatalf("run(%v) stderr lacks error or usage: %q", args, stderr.String())
		}
	}
}

func TestRunVersion(t *testing.T) {
	oldVersion := version
	version = "test-version"
	t.Cleanup(func() { version = oldVersion })

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got, want := stdout.String(), "vikunja-ops test-version\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunVersionRejectsCommandOrArguments(t *testing.T) {
	for _, args := range [][]string{{"--version", "doctor"}, {"--version", "extra"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%v) exit code = %d, want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "--version") || !strings.Contains(stderr.String(), "用法:") {
			t.Fatalf("run(%v) stderr lacks error or usage: %q", args, stderr.String())
		}
	}
}

func TestRunPrettyVersionIsAllowedAndPlain(t *testing.T) {
	oldVersion := version
	version = "test-version"
	t.Cleanup(func() { version = oldVersion })

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--pretty", "--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got, want := stdout.String(), "vikunja-ops test-version\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunPrettyBeforeCommandAccepted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--pretty", "doctor", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "vikunja-ops doctor") {
		t.Fatalf("stdout does not contain doctor usage: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
