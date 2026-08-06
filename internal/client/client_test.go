package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestChecksUseExpectedEndpointsHeadersAndPathPrefix(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		switch r.URL.Path {
		case "/vikunja/api/v2/openapi.json":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("OpenAPI Authorization = %q", got)
			}
		case "/vikunja/api/v1/token/test":
			assertBearer(t, r)
			if r.URL.RawQuery != "" {
				t.Errorf("token query = %q", r.URL.RawQuery)
			}
		case "/vikunja/api/v2/projects":
			assertBearer(t, r)
			if r.URL.RawQuery != "page=1&per_page=1" {
				t.Errorf("projects query = %q", r.URL.RawQuery)
			}
		case "/vikunja/api/v2/token/test":
			t.Error("v2 token/test must not be requested")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := New(server.Client())
	base := server.URL + "/vikunja"
	if err := c.CheckOpenAPI(base); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyTokenV1(base, "fake-token"); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckProjectsRead(base, "fake-token"); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestVerifyTokenV1AuthenticationStatuses(t *testing.T) {
	testAuthenticationStatuses(t, func(c *Client, base string) error { return c.VerifyTokenV1(base, "fake-token") })
}

func TestCheckProjectsReadStatuses(t *testing.T) {
	testAuthenticationStatuses(t, func(c *Client, base string) error { return c.CheckProjectsRead(base, "fake-token") })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	err := New(server.Client()).CheckProjectsRead(server.URL, "fake-token")
	if err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("unexpected error %v", err)
	}
}

func testAuthenticationStatuses(t *testing.T, check func(*Client, string) error) {
	t.Helper()
	for _, test := range []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		err := check(New(server.Client()), server.URL)
		server.Close()
		if !errors.Is(err, test.want) || !errors.Is(err, ErrAuthentication) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
}

func TestTransportErrorIsSafe(t *testing.T) {
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	err := c.CheckProjectsRead("https://host/base?query", "fake-token")
	if err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestRedirectsAreNotFollowedAndTokenIsSafe(t *testing.T) {
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/projects":
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		case "/redirect-target":
			targetRequested = true
			t.Error("redirect target must not be requested")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := New(server.Client()).ListProjects(server.URL, "fake-token", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("unsafe redirect error: %v", err)
	}
	if targetRequested {
		t.Fatal("redirect target was requested")
	}
}

func TestNewDoesNotModifySuppliedHTTPClient(t *testing.T) {
	originalRedirectError := errors.New("original redirect policy")
	supplied := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return originalRedirectError
		},
	}
	_ = New(supplied)
	if err := supplied.CheckRedirect(nil, nil); !errors.Is(err, originalRedirectError) {
		t.Fatalf("supplied client redirect policy was changed: %v", err)
	}
}

func assertJSONEquivalent(t *testing.T, want string, got any) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var wantValue, gotValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &gotValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON = %s, want semantic equivalent of %s", encoded, want)
	}
}

func assertBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer fake-token" {
		t.Errorf("Authorization = %q", got)
	}
}

type roundTripError struct{ err error }

func (r roundTripError) RoundTrip(*http.Request) (*http.Response, error) { return nil, r.err }
