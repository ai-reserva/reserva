// reserva is a small TUI for managing Claude Code sessions across
// several Claude configuration profiles, using git worktrees by default.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to config.toml (default ~/.config/reserva/config.toml)")
		profile    = flag.String("profile", "", "profile to open first")
		list       = flag.Bool("list", false, "print sessions and exit")
	)
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reserva:", err)
		os.Exit(1)
	}
	// Without --profile the TUI starts on the all tab (or the only profile)
	// and --list prints the default profile.
	start := *profile
	if start != "" && start != allProfile {
		if _, ok := cfg.Profiles[start]; !ok {
			fmt.Fprintf(os.Stderr, "reserva: unknown profile %q\n", start)
			os.Exit(1)
		}
	}

	if *list {
		if start == "" {
			start = cfg.DefaultProfile
		}
		printSessions(cfg, start)
		return
	}

	p := tea.NewProgram(newModel(cfg, start))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "reserva:", err)
		os.Exit(1)
	}
}

func printSessions(cfg *Config, profile string) {
	names := []string{profile}
	if profile == allProfile {
		names = cfg.ProfileNames()
	}
	var groups []*Group
	for _, name := range names {
		g, err := loadProfile(cfg.Profiles[name])
		if err != nil {
			fmt.Fprintln(os.Stderr, "reserva:", err)
			os.Exit(1)
		}
		groups = append(groups, g...)
	}
	for _, g := range groups {
		fmt.Println(shortPath(g.Repo))
		for _, s := range g.Sessions {
			mark := " "
			if s.Running {
				mark = "●"
			}
			branch := s.Branch
			if s.Worktree != "" {
				branch = "⎇ " + s.Worktree
			}
			fmt.Printf("  %s %-8s %-50s %-20s %s\n", mark, s.ID[:8], truncate(s.Title, 50), truncate(branch, 20), relTime(s.Modified))
		}
	}
}
