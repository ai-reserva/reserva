package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveSession(t *testing.T) {
	root := t.TempDir()
	from := Profile{Name: "a", ConfigDir: filepath.Join(root, "a")}
	to := Profile{Name: "b", ConfigDir: filepath.Join(root, "b")}
	proj := filepath.Join(from.ConfigDir, "projects", "-home-me-repo")
	if err := os.MkdirAll(filepath.Join(proj, "id1"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, "id1.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Session{ID: "id1", Path: path, Profile: "a"}

	if err := moveSession(s, to); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(to.ConfigDir, "projects", "-home-me-repo", "id1.jsonl")
	if s.Path != want || s.Profile != "b" {
		t.Errorf("session now %q in %q, want %q in b", s.Path, s.Profile, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("file not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to.ConfigDir, "projects", "-home-me-repo", "id1")); err != nil {
		t.Errorf("side directory not moved: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("source still there: %v", err)
	}

	// moving back onto an existing file is refused
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveSession(s, from); err == nil {
		t.Error("overwrote an existing session file")
	}
}

func TestMovePicker(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := &Config{AllTab: "first", Profiles: map[string]Profile{}, order: []string{"work", "work-cswap", "personal"}, DefaultProfile: "work"}
	for _, n := range cfg.order {
		cfg.Profiles[n] = Profile{Name: n, ConfigDir: t.TempDir()}
	}
	m := newModel(cfg, "work")
	m.mode = modeMove
	press := func(keys ...string) {
		for _, k := range keys {
			next, _ := m.moveKey(k)
			m = next.(model)
		}
	}
	if press("j"); m.movePick != 1 {
		t.Errorf("j: pick = %d, want 1", m.movePick)
	}
	if press("k", "k"); m.movePick != 2 {
		t.Errorf("k wraps: pick = %d, want 2", m.movePick)
	}
	if press("9"); m.movePick != 2 || m.mode != modeMove {
		t.Errorf("digit past the targets: pick = %d, mode = %v", m.movePick, m.mode)
	}
	if press("esc"); m.mode != modeList {
		t.Errorf("esc: mode = %v, want the list", m.mode)
	}
}
