// Package config loads the minimal configuration required by vikunja-ops.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config contains connection settings. Token must never be included in output.
type Config struct {
	BaseURL string
	Token   string
}

// Load reads configuration exclusively from .env in the current working directory.
func Load() (Config, error) {
	values, err := readDotEnv(filepath.Join(".", ".env"))
	if err != nil {
		return Config{}, fmt.Errorf("cannot load .env: %w", err)
	}
	if values["VIKUNJA_URL"] == "" {
		return Config{}, errors.New("missing required configuration: VIKUNJA_URL")
	}
	if values["VIKUNJA_TOKEN"] == "" {
		return Config{}, errors.New("missing required configuration: VIKUNJA_TOKEN")
	}
	baseURL, err := normalizeURL(values["VIKUNJA_URL"])
	if err != nil {
		return Config{}, err
	}
	return Config{BaseURL: baseURL, Token: values["VIKUNJA_TOKEN"]}, nil
}

func normalizeURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("VIKUNJA_URL must be an absolute http or https URL")
	}
	if u.User != nil {
		return "", errors.New("VIKUNJA_URL must not contain user credentials")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "export ") {
			text = strings.TrimSpace(strings.TrimPrefix(text, "export "))
		}
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || !validKey(key) {
			return nil, fmt.Errorf("cannot parse .env (line %d)", line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		} else if strings.HasPrefix(value, "'") || strings.HasPrefix(value, "\"") {
			return nil, fmt.Errorf("cannot parse .env (line %d)", line)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("cannot read .env")
	}
	return values, nil
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
