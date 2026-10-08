package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/charmbracelet/x/ansi"
)

// claudeEnv returns the environment for running claude under a profile.
func claudeEnv(p Profile) []string {
	return append(os.Environ(), "CLAUDE_CONFIG_DIR="+p.ConfigDir)
}

// claudeArgv builds the claude command line.
func (c *Config) claudeArgv(extra ...string) []string {
	argv := []string{c.ClaudeBin}
	argv = append(argv, c.ClaudeArgs...)
	return append(argv, extra...)
}

func resumeArgv(c *Config, s *Session) []string {
	return c.claudeArgv("--resume", s.ID)
}

// newSessionArgv builds the command for a brand-new session. With worktree
// set, claude creates <repo>/.claude/worktrees/<name> and runs inside it.
func newSessionArgv(c *Config, name string, worktree bool) []string {
	var extra []string
	if worktree {
		extra = append(extra, "--worktree")
		if name != "" {
			extra = append(extra, name)
		}
	}
	if name != "" {
		extra = append(extra, "--name", name)
	}
	return c.claudeArgv(extra...)
}

// createWorktree adds <repo>/.claude/worktrees/<name> on the given branch,
// creating the branch from HEAD when it does not exist yet. This mirrors what
// `claude --worktree` does, but with a branch name of your choosing.
func createWorktree(repo, name, branch string) (string, error) {
	path := filepath.Join(repo, worktreeMarker[1:], name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("worktree already exists: %s", path)
	}
	args := []string{"-C", repo, "worktree", "add"}
	exists := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
	if exists {
		args = append(args, path, branch)
	} else {
		args = append(args, "-b", branch, path)
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git worktree add: %s", msg)
	}
	return path, nil
}

func shellArgv(c *Config) []string {
	return []string{c.Shell}
}

// inlineCmd runs argv in the current terminal (the TUI is paused meanwhile).
// The real terminal descriptors are passed explicitly: Bubble Tea would
// otherwise pipe stdout through its own writer, and claude then no longer
// sees a tty and falls back to non-interactive print mode.
//
// stderr is additionally captured in the returned tail so that an error
// printed just before exit can be shown once the TUI redraws.
func inlineCmd(p Profile, dir string, argv []string) (*exec.Cmd, *tailWriter) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = claudeEnv(p)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	tail := &tailWriter{max: 4096}
	cmd.Stderr = io.MultiWriter(os.Stderr, tail)
	return cmd, tail
}

// tailWriter keeps the last max bytes written to it.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

// LastLine returns the last non-empty line written, without ANSI styling.
func (t *tailWriter) LastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(ansi.Strip(string(t.buf)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// terminalArgv returns the terminal emulator command prefix, including the
// flag that introduces the command to run.
func terminalArgv(c *Config) ([]string, error) {
	spec := strings.TrimSpace(c.Terminal)
	if spec == "" {
		return nil, fmt.Errorf("no terminal configured: set $TERMINAL or `terminal` in %s", c.path)
	}
	parts := strings.Fields(spec)
	if len(parts) > 1 {
		return parts, nil // user spelled out the flags
	}
	switch filepath.Base(parts[0]) {
	case "wezterm":
		return []string{parts[0], "start", "--"}, nil
	case "gnome-terminal", "tilix", "ptyxis":
		return []string{parts[0], "--"}, nil
	case "foot":
		return []string{parts[0]}, nil
	default: // alacritty, kitty, ghostty, xterm, urxvt, konsole, st, ...
		return []string{parts[0], "-e"}, nil
	}
}

// spawnTerminal opens argv in a new terminal window, detached from the TUI.
func spawnTerminal(c *Config, p Profile, dir string, argv []string) error {
	term, err := terminalArgv(c)
	if err != nil {
		return err
	}
	full := append(append([]string{}, term...), argv...)
	cmd := exec.Command(full[0], full[1:]...)
	cmd.Dir = dir
	cmd.Env = claudeEnv(p)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // reap the child
	return nil
}
