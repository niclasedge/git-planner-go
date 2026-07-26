package config

import (
	"bufio"
	"os"
	"strings"
)

// LoadDotEnv reads KEY=VALUE lines into the process environment. Existing
// variables win, so a shell export or a systemd Environment= still overrides the
// file — the usual dotenv convention, and what lets you run one-off with a
// different token.
//
// It returns the names of keys where the file was overridden by a *different*
// value. That is worth a warning: a stale token exported in a shell profile
// silently shadows the .env you just edited, and the only symptom is 401 Bad
// credentials — which looks like a bad token rather than the wrong token.
// Only names are returned, never values.
//
// A missing file is fine — tokens may well come from the environment directly.
// This is deliberately minimal: no interpolation, no multi-line values. Anything
// more belongs in a real secret store, not a dotfile.
func LoadDotEnv(path string) (shadowed []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if key == "" {
			continue
		}
		want := unquote(strings.TrimSpace(value))
		if have, exists := os.LookupEnv(key); exists {
			if have != want {
				shadowed = append(shadowed, key)
			}
			continue
		}
		os.Setenv(key, want)
	}
	return shadowed, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	// An unquoted trailing comment is a common mistake; strip it.
	if i := strings.Index(v, " #"); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}
