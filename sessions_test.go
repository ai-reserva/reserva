package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPromptText(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"empty", ``, ""},
		{"string", `"fix the login test"`, "fix the login test"},
		{"text parts", `[{"type":"text","text":"one"},{"type":"image"},{"type":"text","text":"two"}]`, "one\ntwo"},
		{"tool result only", `[{"type":"tool_result","content":"ok"}]`, ""},
		{"object", `{"type":"text"}`, ""},
		{"invalid", `[{"type":`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := promptText(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("promptText(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"  hello   world  ", "hello world"},
		{"\n\nsecond line\nthird", "second line"},
		{"<command-name>/clear</command-name>\nactual prompt", "actual prompt"},
		{"<system-reminder>only tags</system-reminder>", ""},
	}
	for _, tt := range tests {
		if got := firstLine(tt.in); got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitWorktree(t *testing.T) {
	tests := []struct{ cwd, repo, name string }{
		{"/src/app", "/src/app", ""},
		{"/src/app/.claude/worktrees/fix-login", "/src/app", "fix-login"},
		{"/src/app/.claude/worktrees/fix-login/internal/db", "/src/app", "fix-login"},
	}
	for _, tt := range tests {
		repo, name := splitWorktree(tt.cwd)
		if repo != tt.repo || name != tt.name {
			t.Errorf("splitWorktree(%q) = (%q, %q), want (%q, %q)", tt.cwd, repo, name, tt.repo, tt.name)
		}
	}
}

// writeSession writes a session file made of the given JSON lines.
func writeSession(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseSession(t *testing.T) {
	repo := t.TempDir()
	wt := filepath.Join(repo, ".claude", "worktrees", "fix-login")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "abc123.jsonl")
	writeSession(t, path,
		`{"type":"user","cwd":"`+wt+`","gitBranch":"fix/login","message":{"role":"user","content":"first prompt"}}`,
		`{"type":"user","cwd":"`+wt+`","isMeta":true,"message":{"role":"user","content":"meta, ignored"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":"reply"}}`,
		`{"type":"user","cwd":"`+wt+`","message":{"role":"user","content":[{"type":"text","text":"second prompt"}]}}`,
		`{"type":"ai-title","aiTitle":"Fix login"}`,
		`{"type":"pr-link","prUrl":"https://example.com/pr/1"}`,
		`{"type":"pr-link","prUrl":"https://example.com/pr/1"}`,
		`not json`,
	)

	s := parseSession("work", path)
	if s == nil {
		t.Fatal("parseSession returned nil")
	}
	checks := []struct{ field, got, want string }{
		{"ID", s.ID, "abc123"},
		{"Profile", s.Profile, "work"},
		{"CWD", s.CWD, wt},
		{"Repo", s.Repo, repo},
		{"Worktree", s.Worktree, "fix-login"},
		{"Branch", s.Branch, "fix/login"},
		{"Title", s.Title, "Fix login"},
		{"LastPrompt", s.LastPrompt, "second prompt"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if s.Missing {
		t.Error("Missing = true for an existing directory")
	}
	if len(s.PRLinks) != 1 {
		t.Errorf("PRLinks = %v, want one deduplicated link", s.PRLinks)
	}
}

func TestParseSessionTitles(t *testing.T) {
	dir := t.TempDir()
	user := `{"type":"user","cwd":"/does/not/exist","message":{"role":"user","content":"the prompt"}}`

	path := filepath.Join(dir, "a.jsonl")
	writeSession(t, path, user, `{"type":"ai-title","aiTitle":"AI"}`, `{"type":"custom-title","customTitle":"Mine"}`)
	if s := parseSession("p", path); s == nil || s.Title != "Mine" {
		t.Errorf("custom title should win over the AI title, got %+v", s)
	}

	path = filepath.Join(dir, "b.jsonl")
	writeSession(t, path, user, `{"type":"last-prompt","lastPrompt":"recorded prompt"}`)
	s := parseSession("p", path)
	if s == nil || s.Title != "the prompt" || s.LastPrompt != "recorded prompt" {
		t.Errorf("want title from the first prompt and the recorded last prompt, got %+v", s)
	}
	if !s.Missing {
		t.Error("Missing = false for a directory that does not exist")
	}

	path = filepath.Join(dir, "c.jsonl")
	writeSession(t, path, `{"type":"ai-title","aiTitle":"No cwd"}`)
	if s := parseSession("p", path); s != nil {
		t.Errorf("a session without a cwd should be skipped, got %+v", s)
	}
}

func TestParseSessionLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	filler := `{"type":"assistant","message":{"role":"assistant","content":"` + strings.Repeat("x", 1000) + `"}}`
	lines := []string{`{"type":"user","cwd":"/src/app","message":{"role":"user","content":"first"}}`}
	for range 3 * chunkSize / len(filler) {
		lines = append(lines, filler)
	}
	lines = append(lines, `{"type":"user","cwd":"/src/app","message":{"role":"user","content":"last"}}`)
	writeSession(t, path, lines...)

	s := parseSession("p", path)
	if s == nil || s.Title != "first" || s.LastPrompt != "last" {
		t.Errorf("want prompts from both the head and the tail, got %+v", s)
	}
}

func TestRenameSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abc.jsonl")
	writeSession(t, path,
		`{"type":"user","cwd":"/src/app","message":{"role":"user","content":"hi"}}`,
		`{"type":"ai-title","aiTitle":"Old"}`,
	)
	s := parseSession("p", path)
	if err := renameSession(s, "New name"); err != nil {
		t.Fatal(err)
	}
	if s := parseSession("p", path); s == nil || s.Title != "New name" {
		t.Errorf("after rename, title = %+v, want %q", s, "New name")
	}
}

func TestLoadProfile(t *testing.T) {
	cfgDir := t.TempDir()
	repo := t.TempDir()
	now := time.Now()
	write := func(name, cwd, prompt string, age time.Duration) {
		path := filepath.Join(cfgDir, "projects", "p", name+".jsonl")
		lines := []string{`{"type":"user","cwd":"` + cwd + `"}`}
		if prompt != "" {
			lines = append(lines, `{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"`+prompt+`"}}`)
		}
		writeSession(t, path, lines...)
		mt := now.Add(-age)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	write("old", repo, "old prompt", 2*time.Hour)
	write("new", filepath.Join(repo, ".claude", "worktrees", "wt"), "new prompt", time.Hour)
	write("other", "/somewhere/else", "elsewhere", 3*time.Hour)
	write("empty", repo, "", 0)

	groups, err := loadProfile(Profile{Name: "p", ConfigDir: cfgDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	g := groups[0]
	if g.Repo != repo {
		t.Errorf("first group = %q, want the most recently active repo %q", g.Repo, repo)
	}
	var ids []string
	for _, s := range g.Sessions {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "new,old" {
		t.Errorf("sessions = %v, want [new old] (newest first, prompt-less session dropped)", ids)
	}
}
