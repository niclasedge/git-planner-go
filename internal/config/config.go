// Package config loads config.yaml and resolves token secrets from the
// environment. Secrets never appear in the config file itself — it holds only
// the name of the environment variable to read.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server Server  `yaml:"server"`
	Tokens []Token `yaml:"tokens"`
	GitHub GitHub  `yaml:"github"`
	Pages  []Page  `yaml:"pages"`
}

type Server struct {
	Bind string `yaml:"bind"`
	// DBPath is the SQLite cache. Not a secret, so it belongs here rather than
	// in .env.
	DBPath string `yaml:"db-path"`
}

// Token is one GitHub identity. Each gets its own rate-limit budget and its own
// namespace in the HTTP cache, because the same URL yields different visibility
// per token.
type Token struct {
	Name  string `yaml:"name"`
	Env   string `yaml:"env"`
	Label string `yaml:"label"`

	// Secret is resolved from Env at load time and is never serialized.
	Secret string `yaml:"-"`
}

type GitHub struct {
	// Repos to track as "owner/repo". Empty means: discover everything each
	// token can see.
	Repos   []string `yaml:"repos"`
	Refresh Refresh  `yaml:"refresh"`
	Issues  Issues   `yaml:"issues"`
	Actions Actions  `yaml:"actions"`
}

type Refresh struct {
	Notifications Duration `yaml:"notifications"`
	Issues        Duration `yaml:"issues"`
	Actions       Duration `yaml:"actions"`
	Repos         Duration `yaml:"repos"`
	// Full is the reconciliation interval: the sweep that asks every repo directly
	// and the GraphQL totals sweep both run on this clock. Issues above is the
	// incremental interval — how often to ask GitHub only what changed. Keep Full
	// well above it: it is the expensive one, and its job is to catch what an
	// incremental cursor structurally cannot.
	Full Duration `yaml:"full"`
}

type Issues struct {
	PerRepo int    `yaml:"per-repo"`
	State   string `yaml:"state"`
}

type Actions struct {
	RunsPerRepo int `yaml:"runs-per-repo"`
	// JobsPerRepo limits how many of the newest runs get their jobs fetched.
	// Jobs cost one request per run, so this is the main cost knob on page 2.
	// Zero disables the step dots entirely.
	JobsPerRepo int `yaml:"jobs-per-repo"`
}

// Page is a dashboard page. Built-in pages carry a Type ("issues", "actions");
// free-form pages carry Columns of widgets.
type Page struct {
	Title   string   `yaml:"title"`
	Slug    string   `yaml:"slug"`
	Type    string   `yaml:"type"`
	Columns []Column `yaml:"columns"`
}

type Column struct {
	Size    string      `yaml:"size"` // "small" | "full"
	Widgets []yaml.Node `yaml:"widgets"`
}

// Duration wraps time.Duration so YAML can carry "90s" / "2m" strings.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }

// Load reads path, applies defaults and resolves token secrets from the
// environment. A token whose environment variable is unset is skipped with a
// warning rather than failing the whole start — one broken identity should not
// take the dashboard down.
func Load(path string) (*Config, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	c.applyDefaults()

	var warnings []string
	resolved := make([]Token, 0, len(c.Tokens))
	for _, t := range c.Tokens {
		if t.Name == "" {
			return nil, nil, fmt.Errorf("token entry without a name")
		}
		if t.Env == "" {
			t.Env = "GITHUB_PAT"
		}
		secret := strings.TrimSpace(os.Getenv(t.Env))
		if secret == "" {
			warnings = append(warnings, fmt.Sprintf(
				"token %q skipped: environment variable %s is empty", t.Name, t.Env))
			continue
		}
		t.Secret = secret
		if t.Label == "" {
			t.Label = t.Name
		}
		resolved = append(resolved, t)
	}
	c.Tokens = resolved

	if len(c.Tokens) == 0 {
		return nil, warnings, fmt.Errorf("no usable tokens: set at least one PAT in .env")
	}

	if err := c.validatePages(); err != nil {
		return nil, warnings, err
	}

	return &c, warnings, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Bind == "" {
		c.Server.Bind = "127.0.0.1:8090"
	}
	if c.Server.DBPath == "" {
		c.Server.DBPath = "./data/git-planner.db"
	}
	if c.GitHub.Refresh.Notifications == 0 {
		c.GitHub.Refresh.Notifications = Duration(60 * time.Second)
	}
	if c.GitHub.Refresh.Issues == 0 {
		c.GitHub.Refresh.Issues = Duration(2 * time.Minute)
	}
	if c.GitHub.Refresh.Actions == 0 {
		c.GitHub.Refresh.Actions = Duration(1 * time.Minute)
	}
	if c.GitHub.Refresh.Repos == 0 {
		c.GitHub.Refresh.Repos = Duration(30 * time.Minute)
	}
	if c.GitHub.Refresh.Full == 0 {
		c.GitHub.Refresh.Full = Duration(1 * time.Hour)
	}
	if c.GitHub.Issues.PerRepo == 0 {
		c.GitHub.Issues.PerRepo = 50
	}
	if c.GitHub.Issues.State == "" {
		c.GitHub.Issues.State = "open"
	}
	if c.GitHub.Actions.RunsPerRepo == 0 {
		c.GitHub.Actions.RunsPerRepo = 10
	}
	if c.GitHub.Actions.JobsPerRepo == 0 {
		c.GitHub.Actions.JobsPerRepo = 3
	}
	// A config with no pages still gets the two built-in ones, so a minimal
	// config.yaml is just a token and it works.
	if len(c.Pages) == 0 {
		c.Pages = []Page{
			{Title: "Issues", Type: "issues"},
			{Title: "Actions", Type: "actions"},
		}
	}
}

// validatePages assigns slugs and rejects collisions. Built-in pages get fixed
// slugs so their URLs stay stable regardless of title.
func (c *Config) validatePages() error {
	seen := map[string]bool{}
	for i := range c.Pages {
		p := &c.Pages[i]
		switch p.Type {
		case "issues":
			p.Slug = "issues"
		case "actions":
			p.Slug = "actions"
		case "planner":
			p.Slug = "planner"
		case "", "widgets":
			p.Type = "widgets"
			if p.Slug == "" {
				p.Slug = slugify(p.Title)
			}
		default:
			return fmt.Errorf("page %q: unknown type %q (want issues, planner, actions or widgets)", p.Title, p.Type)
		}
		if p.Slug == "" {
			return fmt.Errorf("page %d has neither title nor slug", i)
		}
		if seen[p.Slug] {
			return fmt.Errorf("duplicate page slug %q", p.Slug)
		}
		seen[p.Slug] = true
	}
	return nil
}

func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == 'ä':
			b.WriteString("ae")
			prevDash = false
		case r == 'ö':
			b.WriteString("oe")
			prevDash = false
		case r == 'ü':
			b.WriteString("ue")
			prevDash = false
		case r == 'ß':
			b.WriteString("ss")
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
