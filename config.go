package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Profile is one Claude Code configuration directory (what CLAUDE_CONFIG_DIR
// points at). Sessions are discovered from <config_dir>/projects.
type Profile struct {
	Name      string  `toml:"-"`
	ConfigDir string  `toml:"config_dir"`
	Forges    []Forge `toml:"forges"` // where this profile's open MRs are fetched from
}

// Config is ~/.config/reserva/config.toml.
type Config struct {
	DefaultProfile string             `toml:"default_profile"`
	ClaudeBin      string             `toml:"claude_bin"`
	ClaudeArgs     []string           `toml:"claude_args"`
	Terminal       string             `toml:"terminal"`
	Shell          string             `toml:"shell"`
	Worktrees      *bool              `toml:"worktrees"`
	NewTerminal    *bool              `toml:"new_terminal"`
	AllTab         string             `toml:"all_tab"` // where the all tab goes: first or last
	Profiles       map[string]Profile `toml:"profiles"`

	// ordered profile names, in config file order
	order []string
	path  string
}

func defaultConfigPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "reserva", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "reserva", "config.toml")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}

// loadConfig reads the config file, creating a default one on first run.
func loadConfig(path string) (*Config, error) {
	if path == "" {
		path = defaultConfigPath()
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeDefaultConfig(path); err != nil {
			return nil, err
		}
	}
	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.path = path
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	// Preserve the order profiles are written in the file.
	seen := map[string]bool{}
	for _, k := range meta.Keys() {
		if len(k) == 2 && k[0] == "profiles" && !seen[k[1]] {
			if _, ok := cfg.Profiles[k[1]]; ok {
				seen[k[1]] = true
				cfg.order = append(cfg.order, k[1])
			}
		}
	}
	for name := range cfg.Profiles {
		if !seen[name] {
			cfg.order = append(cfg.order, name)
		}
	}
	for name, p := range cfg.Profiles {
		p.Name = name
		p.ConfigDir = expandHome(p.ConfigDir)
		if p.ConfigDir == "" {
			return nil, fmt.Errorf("%s: profile %q has no config_dir", path, name)
		}
		for i := range p.Forges {
			if err := p.Forges[i].validate(); err != nil {
				return nil, fmt.Errorf("%s: profile %q: %w", path, name, err)
			}
		}
		cfg.Profiles[name] = p
	}
	if len(cfg.order) == 0 {
		return nil, fmt.Errorf("%s: no profiles defined", path)
	}
	if cfg.ClaudeBin == "" {
		cfg.ClaudeBin = "claude"
	}
	switch cfg.AllTab {
	case "":
		cfg.AllTab = "first"
	case "first", "last":
	default:
		return nil, fmt.Errorf("%s: all_tab must be first or last, not %q", path, cfg.AllTab)
	}
	if cfg.Shell == "" {
		cfg.Shell = os.Getenv("SHELL")
		if cfg.Shell == "" {
			cfg.Shell = "/bin/sh"
		}
	}
	if cfg.Terminal == "" {
		cfg.Terminal = os.Getenv("TERMINAL")
	}
	if cfg.Terminal == "" && runtime.GOOS == "darwin" && os.Getenv("TERM_PROGRAM") == "Apple_Terminal" {
		cfg.Terminal = terminalApp
	}
	if cfg.DefaultProfile == "" || cfg.Profiles[cfg.DefaultProfile].ConfigDir == "" {
		cfg.DefaultProfile = cfg.order[0]
	}
	return &cfg, nil
}

func (c *Config) UseWorktrees() bool {
	return c.Worktrees == nil || *c.Worktrees
}

// NewTerminalDefault reports whether enter opens a new terminal window
// (shift+enter then runs in this one). It is off when no terminal is
// configured, since spawning one would only fail.
func (c *Config) NewTerminalDefault() bool {
	if strings.TrimSpace(c.Terminal) == "" {
		return false
	}
	return c.NewTerminal == nil || *c.NewTerminal
}

// ProfileNames returns profiles in config-file order.
func (c *Config) ProfileNames() []string { return c.order }

// discoverConfigDirs finds ~/.claude and ~/.claude-* directories that look
// like Claude Code config dirs.
func discoverConfigDirs() []Profile {
	home, _ := os.UserHomeDir()
	entries, _ := filepath.Glob(filepath.Join(home, ".claude*"))
	var out []Profile
	for _, e := range entries {
		st, err := os.Stat(e)
		if err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(e, "projects")); err != nil {
			continue
		}
		base := filepath.Base(e)
		name := "default"
		if strings.HasPrefix(base, ".claude-") {
			name = strings.TrimPrefix(base, ".claude-")
		} else if base != ".claude" {
			continue
		}
		out = append(out, Profile{Name: name, ConfigDir: "~/" + base})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == "default" {
			return true
		}
		if out[j].Name == "default" {
			return false
		}
		return out[i].Name < out[j].Name
	})
	if len(out) == 0 {
		out = append(out, Profile{Name: "default", ConfigDir: "~/.claude"})
	}
	taken := map[string]bool{}
	for _, p := range out {
		taken[p.Name] = true
	}
	return append(out, discoverClaudeSwapDirs(taken)...)
}

// discoverClaudeSwapDirs finds the per-account profiles of claude-swap
// (cswap), which live in $XDG_DATA_HOME/claude-swap/sessions/<n>-<email>
// with "@" written as "_". The profile is named after the email's local
// part, with the account number appended when that is taken (by another
// account or by one of the profiles in taken, which is updated).
func discoverClaudeSwapDirs(taken map[string]bool) []Profile {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	entries, _ := filepath.Glob(filepath.Join(data, "claude-swap", "sessions", "*-*"))
	sort.Strings(entries)
	var out []Profile
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(e, "projects")); err != nil {
			continue
		}
		base := filepath.Base(e)
		num, email, _ := strings.Cut(base, "-")
		// The domain has no "_", the local part may: the last one is the "@".
		name := email
		if i := strings.LastIndexByte(email, '_'); i > 0 {
			name = email[:i]
		}
		if taken[name] {
			name += "-" + num
		}
		taken[name] = true
		dir := e
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(e, home+"/") {
			dir = "~" + e[len(home):]
		}
		out = append(out, Profile{Name: name, ConfigDir: dir})
	}
	return out
}

// tomlKey quotes a table key unless it is a bare key (A-Za-z0-9_-).
func tomlKey(name string) string {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Sprintf("%q", name)
		}
	}
	return name
}

func writeDefaultConfig(path string) error {
	profiles := discoverConfigDirs()
	def := profiles[0].Name
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		for _, p := range profiles {
			if expandHome(p.ConfigDir) == filepath.Clean(expandHome(env)) {
				def = p.Name
			}
		}
	}
	var b strings.Builder
	b.WriteString("# reserva configuration\n")
	b.WriteString("# Generated on first run; edit freely.\n\n")
	fmt.Fprintf(&b, "default_profile = %q\n", def)
	b.WriteString("\n# Binary used to start Claude Code.\n")
	b.WriteString("claude_bin = \"claude\"\n")
	b.WriteString("\n# Extra arguments appended to every claude invocation.\n")
	b.WriteString("claude_args = []\n")
	b.WriteString("\n# Terminal emulator used by shift+enter / ctrl+shift+enter.\n")
	b.WriteString("# Defaults to $TERMINAL, or to \"Terminal.app\" when running in\n")
	b.WriteString("# Terminal.app on macOS. Flags for running a command are added\n")
	b.WriteString("# automatically for common terminals, or spell them out, e.g.\n")
	b.WriteString("# terminal = \"wezterm start --\"\n")
	b.WriteString("terminal = \"\"\n")
	b.WriteString("\n# enter opens sessions and shells in a new terminal window, and\n")
	b.WriteString("# shift+enter / ctrl+shift+enter in this one. false swaps them.\n")
	b.WriteString("new_terminal = true\n")
	b.WriteString("\n# Shell opened by ctrl+enter (defaults to $SHELL).\n")
	b.WriteString("shell = \"\"\n")
	b.WriteString("\n# New sessions are started in a fresh git worktree (claude --worktree).\n")
	b.WriteString("worktrees = true\n")
	b.WriteString("\n# With several profiles, the all tab lists every profile's sessions.\n")
	b.WriteString("# It goes before (first) or after (last) the profiles' own tabs.\n")
	b.WriteString("all_tab = \"first\"\n")
	b.WriteString("\n# One profile per Claude Code config directory (CLAUDE_CONFIG_DIR).\n")
	b.WriteString("# ~/.claude, ~/.claude-* and claude-swap (cswap) accounts are found\n")
	b.WriteString("# automatically on first run.\n")
	b.WriteString("# Add forges to a profile to see its open merge requests (m), e.g.\n")
	b.WriteString("# forges = [{ kind = \"gitlab\", host = \"gitlab.com\" }]\n")
	for _, p := range profiles {
		fmt.Fprintf(&b, "\n[profiles.%s]\nconfig_dir = %q\n", tomlKey(p.Name), p.ConfigDir)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
