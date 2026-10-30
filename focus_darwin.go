package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const terminalAppBin = "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal"

// Selects the Terminal.app tab whose tty is the first argument and brings
// its window to the front. Prints "ok", or "none" when no tab has that tty.
const terminalAppFocusScript = `
on run argv
	set target to item 1 of argv
	tell application "Terminal"
		repeat with w in windows
			repeat with t in tabs of w
				if tty of t is target then
					set selected of t to true
					set frontmost of w to true
					activate
					return "ok"
				end if
			end repeat
		end repeat
	end tell
	return "none"
end run
`

// focusWindowOf brings the Terminal.app tab hosting process pid to the
// front. The tab is found by the tty of the process, so shells and tmux
// between the terminal and Claude do not matter. Other terminals are not
// supported on macOS.
func focusWindowOf(pid int) error {
	if !processRunning(terminalAppBin) {
		return errors.New("window focusing on macOS supports Terminal.app only")
	}
	tty, err := ttyOf(pid)
	if err != nil {
		return err
	}
	cmd := exec.Command("osascript", "-", tty)
	cmd.Stdin = strings.NewReader(terminalAppFocusScript)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("osascript: %w", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("no Terminal.app tab owns %s", tty)
	}
	return nil
}

// ttyOf returns the controlling terminal of pid as a device path, such as
// /dev/ttys003.
func ttyOf(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "tty=", "-p", fmt.Sprint(pid)).Output()
	if err != nil {
		return "", fmt.Errorf("ps: %w", err)
	}
	tty := strings.TrimSpace(string(out))
	if tty == "" || tty == "??" {
		return "", fmt.Errorf("pid %d has no controlling terminal", pid)
	}
	return "/dev/" + tty, nil
}

// processRunning reports whether a process with the given command path is
// running. Asking a macOS app over AppleScript launches it when it is not
// running, so callers check first.
func processRunning(command string) bool {
	out, err := exec.Command("ps", "-axo", "comm=").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == command {
			return true
		}
	}
	return false
}
