package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"

	"vikunja-opencode-skill/internal/client"
)

func TestSharedCLIHelpers(t *testing.T) {
	t.Run("pagination defaults and range", func(t *testing.T) {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		page, perPage := bindPaginationFlags(flags)
		if *page != 1 || *perPage != 50 {
			t.Fatalf("defaults = page %d, per-page %d", *page, *perPage)
		}
		for _, pagination := range [][2]int{{1, 1}, {1, 50}, {2, 1000}} {
			if !validPagination(pagination[0], pagination[1]) {
				t.Errorf("validPagination(%d, %d) = false", pagination[0], pagination[1])
			}
		}
		for _, pagination := range [][2]int{{0, 50}, {1, 0}, {1, 1001}} {
			if validPagination(pagination[0], pagination[1]) {
				t.Errorf("validPagination(%d, %d) = true", pagination[0], pagination[1])
			}
		}
	})

	t.Run("JSON preserves bytes and appends newline", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		value := json.RawMessage(`{"unknown_field":{"preserved":true}}`)
		if code := writeJSON("resource get", value, &stdout, &stderr); code != 0 {
			t.Fatalf("writeJSON() = %d, stderr %q", code, stderr.String())
		}
		if got, want := stdout.String(), "{\"unknown_field\":{\"preserved\":true}}\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("resource errors retain specific messages without error details", func(t *testing.T) {
		var stderr bytes.Buffer
		secret := errors.New("request failed with test-secret-token")
		if code := printResourceError("tasks get", errors.Join(client.ErrNotFound, secret), "任务读取被拒绝", "任务或项目不存在", &stderr); code != 1 {
			t.Fatalf("printResourceError() = %d", code)
		}
		if got, want := stderr.String(), "tasks get: 任务或项目不存在\n"; got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})
}

func jsonEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func assertPrettyJSON(t *testing.T, output []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatalf("output is not JSON: %v; output %q", err, output)
	}
	text := string(output)
	if !strings.HasSuffix(text, "\n") {
		t.Errorf("pretty JSON does not end with a newline: %q", text)
	}
	if !strings.Contains(text, "\n  \"") {
		t.Errorf("pretty JSON does not use a two-space top-level indent: %q", text)
	}
}
