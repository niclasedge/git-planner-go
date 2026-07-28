package config

import "testing"

// The container has to bind 0.0.0.0 while the same config file started natively
// must not — so the override has to win over the file, and its absence must
// leave the file's value alone.
func TestApplyDefaults_BindOverride(t *testing.T) {
	t.Setenv("GITPLANNER_BIND", "0.0.0.0:8092")
	c := &Config{Server: Server{Bind: "127.0.0.1:8092"}}
	c.applyDefaults()
	if c.Server.Bind != "0.0.0.0:8092" {
		t.Fatalf("override ignored: %q", c.Server.Bind)
	}
}

func TestApplyDefaults_BindWithoutOverride(t *testing.T) {
	t.Setenv("GITPLANNER_BIND", "")
	c := &Config{Server: Server{Bind: "127.0.0.1:8092"}}
	c.applyDefaults()
	if c.Server.Bind != "127.0.0.1:8092" {
		t.Fatalf("an empty override must not change the file's value: %q", c.Server.Bind)
	}

	// And an unset bind still falls back to loopback, never a wildcard.
	empty := &Config{}
	empty.applyDefaults()
	if empty.Server.Bind != "127.0.0.1:8090" {
		t.Fatalf("default bind changed: %q", empty.Server.Bind)
	}
}
