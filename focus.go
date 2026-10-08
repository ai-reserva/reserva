package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// focusWindowOf brings the window hosting process pid to the front. It finds
// the window by walking up pid's ancestors until one of them owns a window,
// which works for terminals that run one process per window (alacritty -e,
// kitty, foot, ...). Only niri on Linux is supported.
func focusWindowOf(pid int) error {
	if os.Getenv("NIRI_SOCKET") == "" {
		return errors.New("window focusing needs niri")
	}
	out, err := exec.Command("niri", "msg", "--json", "windows").Output()
	if err != nil {
		return fmt.Errorf("niri msg windows: %w", err)
	}
	var windows []struct {
		ID  int `json:"id"`
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(out, &windows); err != nil {
		return fmt.Errorf("niri msg windows: %w", err)
	}
	byPID := map[int]int{}
	for _, w := range windows {
		byPID[w.PID] = w.ID
	}
	for p := pid; p > 1; {
		if id, ok := byPID[p]; ok {
			out, err := exec.Command("niri", "msg", "action", "focus-window", "--id", strconv.Itoa(id)).CombinedOutput()
			if err != nil {
				return fmt.Errorf("niri focus-window: %s", strings.TrimSpace(string(out)))
			}
			return nil
		}
		if p, err = parentPID(p); err != nil {
			return err
		}
	}
	return fmt.Errorf("no window found for pid %d", pid)
}

// parentPID reads the parent of pid from /proc (Linux only).
func parentPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The command name in field 2 may contain spaces and parentheses, so
	// parse from the last ')': state, then ppid.
	s := string(data)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected /proc/%d/stat", pid)
	}
	return strconv.Atoi(fields[1])
}
