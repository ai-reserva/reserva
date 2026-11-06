package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAllTabLocation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := func(allTab string, names ...string) *Config {
		c := &Config{AllTab: allTab, Profiles: map[string]Profile{}, order: names}
		for _, n := range names {
			c.Profiles[n] = Profile{Name: n, ConfigDir: t.TempDir()}
		}
		c.DefaultProfile = names[0]
		return c
	}
	tests := []struct {
		name     string
		cfg      *Config
		start    string
		tabs     []string
		selected string
	}{
		{"first", cfg("first", "work", "home"), "work", []string{"all", "work", "home"}, "work"},
		{"last", cfg("last", "work", "home"), "work", []string{"work", "home", "all"}, "work"},
		{"--profile all", cfg("last", "work", "home"), "all", []string{"work", "home", "all"}, "all"},
		{"last with --profile", cfg("last", "work", "home"), "home", []string{"work", "home", "all"}, "home"},
		{"one profile", cfg("last", "work"), "work", []string{"work"}, "work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(tt.cfg, tt.start)
			if !slices.Equal(m.profiles, tt.tabs) {
				t.Errorf("tabs = %v, want %v", m.profiles, tt.tabs)
			}
			if got := m.profiles[m.profile]; got != tt.selected {
				t.Errorf("selected tab = %q, want %q", got, tt.selected)
			}
			if got := m.realProfiles(); !slices.Equal(got, tt.cfg.order) {
				t.Errorf("realProfiles() = %v, want %v", got, tt.cfg.order)
			}
		})
	}
}

func TestLoadConfigAllTab(t *testing.T) {
	path := writeTestConfig(t, "all_tab = \"middle\"\n[profiles.a]\nconfig_dir = \"/a\"\n")
	if _, err := loadConfig(path); err == nil {
		t.Error("all_tab = middle should be rejected")
	}
	cfg, err := loadConfig(writeTestConfig(t, "[profiles.a]\nconfig_dir = \"/a\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllTab != "first" {
		t.Errorf("AllTab = %q, want first by default", cfg.AllTab)
	}
}

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
