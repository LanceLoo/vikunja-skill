// Package client provides read-only Vikunja health checks.
package client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client performs Vikunja API checks.
type Client struct{ httpClient *http.Client }

// New constructs a client. A nil client uses a 10-second total request timeout.
func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	// Copy the supplied client so disabling redirects cannot alter the caller's
	// client. Redirects can otherwise forward the bearer token to another URL.
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: &clientCopy}
}

// CheckOpenAPI verifies that the OpenAPI document endpoint is reachable.
func (c *Client) CheckOpenAPI(baseURL string) error {
	response, err := c.get(endpoint(baseURL, "openapi.json"), "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenAPI request failed with HTTP status %d", response.StatusCode)
	}
	return nil
}

// VerifyTokenV1 verifies a token against the v1 API without exposing its value.
func (c *Client) VerifyTokenV1(baseURL, token string) error {
	return c.checkTokenEndpoint(tokenTestEndpoint(baseURL), token, "token validation")
}

// CheckProjectsRead verifies that the token can read projects through the v2 API.
func (c *Client) CheckProjectsRead(baseURL, token string) error {
	return c.checkTokenEndpoint(projectsEndpoint(baseURL), token, "projects read request")
}

func (c *Client) checkTokenEndpoint(target, token, operation string) error {
	response, err := c.get(target, token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s failed with HTTP status %d", operation, response.StatusCode)
	}
	return nil
}

func (c *Client) get(target, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed", safeURL(target))
	}
	return response, nil
}

func endpoint(baseURL, suffix string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v2/" + suffix
}
func tokenTestEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v1/token/test"
}
func projectsEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v2/projects?page=1&per_page=1"
}
