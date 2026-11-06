package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("TERMINAL", "")
	t.Setenv("TERM_PROGRAM", "")

	path := writeConfig(t, `
[profiles.work]
config_dir = "~/.claude-work"

[profiles.default]
config_dir = "~/.claude"

[[profiles.default.forges]]
kind = "github"
host = "github.com"
`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ProfileNames(); !slices.Equal(got, []string{"work", "default"}) {
		t.Errorf("ProfileNames() = %v, want file order [work default]", got)
	}
	if cfg.DefaultProfile != "work" {
		t.Errorf("DefaultProfile = %q, want the first profile", cfg.DefaultProfile)
	}
	if got, want := cfg.Profiles["work"].ConfigDir, filepath.Join(home, ".claude-work"); got != want {
		t.Errorf("ConfigDir = %q, want %q", got, want)
	}
	if cfg.ClaudeBin != "claude" || cfg.Shell != "/bin/zsh" {
		t.Errorf("defaults: ClaudeBin = %q, Shell = %q", cfg.ClaudeBin, cfg.Shell)
	}
	if got := cfg.Profiles["default"].Forges[0].Scopes; !slices.Equal(got, []string{"author", "assignee"}) {
		t.Errorf("forge scopes = %v, want the default [author assignee]", got)
	}
	if !cfg.UseWorktrees() {
		t.Error("UseWorktrees() = false, want true by default")
	}
	if cfg.NewTerminalDefault() {
		t.Error("NewTerminalDefault() = true without a terminal configured")
	}
}

func TestLoadConfigOptions(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	path := writeConfig(t, `
default_profile = "b"
terminal = "foot"
worktrees = false
new_terminal = false

[profiles.a]
config_dir = "/a"

[profiles.b]
config_dir = "/b"
`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultProfile != "b" {
		t.Errorf("DefaultProfile = %q, want b", cfg.DefaultProfile)
	}
	if cfg.UseWorktrees() {
		t.Error("UseWorktrees() = true with worktrees = false")
	}
	if cfg.NewTerminalDefault() {
		t.Error("NewTerminalDefault() = true with new_terminal = false")
	}
}

func TestLoadConfigErrors(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"no profiles", `claude_bin = "claude"`, "no profiles defined"},
		{"no config_dir", "[profiles.a]\n", `profile "a" has no config_dir`},
		{"bad forge kind", "[profiles.a]\nconfig_dir = \"/a\"\n[[profiles.a.forges]]\nkind = \"svn\"\nhost = \"x\"\n", `forge kind "svn" is not supported`},
		{"bad toml", "[profiles.a\n", "config.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(writeConfig(t, tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestForgeValidate(t *testing.T) {
	tests := []struct {
		name string
		f    Forge
		ok   bool
	}{
		{"gitlab", Forge{Kind: "gitlab", Host: "gitlab.com"}, true},
		{"gitea with scopes", Forge{Kind: "gitea", Host: "codeberg.org", Scopes: []string{"reviewer"}}, true},
		{"unknown kind", Forge{Kind: "svn", Host: "x"}, false},
		{"no host", Forge{Kind: "github"}, false},
		{"unknown scope", Forge{Kind: "github", Host: "github.com", Scopes: []string{"watcher"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.f.validate(); (err == nil) != tt.ok {
				t.Errorf("validate() = %v, want ok = %v", err, tt.ok)
			}
		})
	}
}
