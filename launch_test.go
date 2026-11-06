package main

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"claude", "claude"},
		{"--resume", "--resume"},
		{"", "''"},
		{"two words", "'two words'"},
		{"it's", `'it'\''s'`},
		{"$HOME", "'$HOME'"},
		{"CLAUDE_CONFIG_DIR=/x", "'CLAUDE_CONFIG_DIR=/x'"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Quoted words must come back unchanged when a real shell parses them.
func TestShellQuoteRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for _, in := range []string{"", "plain", "it's", `a "b" $c`, "`x` ; rm -rf / & | <>", "tab\tnew\nline", "~/*?[]"} {
		out, err := exec.Command(sh, "-c", "printf %s "+shellQuote(in)).Output()
		if err != nil {
			t.Fatalf("sh failed for %q: %v", in, err)
		}
		if string(out) != in {
			t.Errorf("round trip of %q gave %q", in, out)
		}
	}
}

func TestConfigDirEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := configDirEnv(Profile{ConfigDir: filepath.Join(home, ".claude") + "/"}); got != "" {
		t.Errorf("default profile: got %q, want CLAUDE_CONFIG_DIR left unset", got)
	}
	dir := filepath.Join(home, ".claude-work")
	if got, want := configDirEnv(Profile{ConfigDir: dir}), "CLAUDE_CONFIG_DIR="+dir; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNewSessionArgv(t *testing.T) {
	c := &Config{ClaudeBin: "claude", ClaudeArgs: []string{"--model", "opus"}}
	tests := []struct {
		name     string
		worktree bool
		want     []string
	}{
		{"fix", true, []string{"claude", "--model", "opus", "--worktree", "fix", "--name", "fix"}},
		{"fix", false, []string{"claude", "--model", "opus", "--name", "fix"}},
		{"", true, []string{"claude", "--model", "opus", "--worktree"}},
		{"", false, []string{"claude", "--model", "opus"}},
	}
	for _, tt := range tests {
		if got := newSessionArgv(c, tt.name, tt.worktree); !slices.Equal(got, tt.want) {
			t.Errorf("newSessionArgv(%q, %v) = %v, want %v", tt.name, tt.worktree, got, tt.want)
		}
	}
	if got, want := resumeArgv(c, &Session{ID: "abc"}), []string{"claude", "--model", "opus", "--resume", "abc"}; !slices.Equal(got, want) {
		t.Errorf("resumeArgv = %v, want %v", got, want)
	}
}

func TestTailWriter(t *testing.T) {
	w := &tailWriter{max: 32}
	w.Write([]byte("first line\n"))
	w.Write([]byte("\x1b[31merror: boom\x1b[0m\n\n"))
	if got := w.LastLine(); got != "error: boom" {
		t.Errorf("LastLine() = %q, want the last non-empty line without styling", got)
	}
	if len(w.buf) > w.max {
		t.Errorf("buffer holds %d bytes, want at most %d", len(w.buf), w.max)
	}
}
