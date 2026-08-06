package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

var (
	// ErrAuthentication is retained as the common authentication failure class.
	ErrAuthentication = errors.New("authentication failed")
	// ErrUnauthorized indicates that the service did not accept the token or did not receive the authentication header.
	ErrUnauthorized = fmt.Errorf("%w: token was not accepted or authorization header was not received", ErrAuthentication)
	// ErrForbidden indicates that the service recognized the token but rejected it according to the instance or permission policy.
	ErrForbidden = fmt.Errorf("%w: token was rejected by instance or permission policy", ErrAuthentication)
	// ErrNotFound indicates that the requested API resource does not exist.
	ErrNotFound = errors.New("resource not found")
)

func responseError(response *http.Response, operation string) error {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= 300 {
		return fmt.Errorf("%s failed with HTTP status %d", operation, response.StatusCode)
	}
	return nil
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "Vikunja endpoint"
	}
	u.User = nil
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}
