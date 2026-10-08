# reserva

A small terminal UI for managing [Claude Code](https://claude.com/claude-code)
sessions across several Claude profiles. It lists every conversation Claude
Code has stored, grouped by repository, and lets you jump back into one with a
single key, either resuming Claude or dropping into a shell in that session's
worktree.

In Brazilian football the *reservas* are the substitutes on the bench, waiting
to be sent on. Here they are your Claude sessions.

```
 reserva   1:default   2:work   3:personal
──────────────────────────────────────────────────────────────────────
 ~/src/webapp 3
   ● Fix flaky login test                   fix/login-retry        1d
     Add dark mode toggle                   main                   6d
     Refactor API client                    ⎇ api-client           5d
 ~/src/reserva 1
   ● Claude Code session TUI                main                  now
──────────────────────────────────────────────────────────────────────
 ~/src/webapp  › let's start with the retry logic
 ⏎ claude  ⇧⏎ claude in new term  ^⏎ shell  ^⇧⏎ shell in new term  n new  h/l profile  / filter  ? help  q quit
```

## Features

- Every Claude Code session on your machine, grouped by repository and sorted
  by recent activity, with live busy/idle markers for sessions that are running.
- Multiple Claude profiles (`~/.claude`, `~/.claude-work`, …) as tabs, each
  launched with its own `CLAUDE_CONFIG_DIR`.
- Resume a session or open a shell in its worktree, in the current terminal or
  a new window.
- Start new sessions in a fresh git worktree, with a branch name of your
  choosing.
- Rename, archive and filter sessions.
- Reads Claude's files; the only write is a rename record (see
  [Configuration](#configuration)).

## Requirements

- Linux or macOS (see [macOS notes](#macos-notes)). Windows is not supported.
- [Go](https://go.dev/dl/) 1.26 or newer, to build it.
- The [`claude` CLI](https://docs.claude.com/en/docs/claude-code) on your `PATH`.
- `git`, for worktree sessions.

## Install

With Go:

```sh
go install jpnadas.com/reserva@latest
```

This puts the binary in `$(go env GOPATH)/bin` (usually `~/go/bin`); make sure
that directory is on your `PATH`.

From a checkout:

```sh
git clone https://codeberg.org/jpnadas/reserva
cd reserva
go build -o reserva .
mkdir -p ~/.local/bin && mv reserva ~/.local/bin/
```

Then run `reserva`. On first run it writes a config file with every
Claude profile it finds (see [Configuration](#configuration)).

## Keys

Navigation is vim-style.

| Key | Action |
| --- | --- |
| `j` / `k`, `gg` / `G`, `ctrl+d` / `ctrl+u` | move through the list |
| `]` / `[` | next / previous project |
| `h` / `l`, `1`–`9`, `tab` | switch Claude profile |
| `enter` | resume Claude Code in the selected session, in a new terminal window (or focus its window if it is already running) |
| `shift+enter` | same, in this terminal |
| `ctrl+enter` | open a shell in the session's worktree, in a new terminal window |
| `ctrl+shift+enter` | same, in this terminal |
| `n` | new session: asks for a repository, then a name (creates a git worktree) |
| `N` | same, without a worktree |
| `R` | rename the selected session |
| `a` | archive or unarchive the selected session |
| `A` | show or hide archived sessions |
| `/` | filter sessions by title, prompt, branch or path |
| `r` | reload |
| `?` | help |
| `q` | quit |

`n` first asks for the repository. The prompt is prefilled with the selected
project, `tab` completes directory names and `~` is expanded, so you can run
reserva from anywhere and point it at any repo. `enter` on a project
header does the same with that project prefilled. Then it asks for a name,
which becomes the worktree directory name and the session's display name, and
for worktree sessions a branch name. In the last prompt `enter` starts the
session in a new terminal window and `shift+enter` starts it in this one.

Opening things in new windows keeps reserva running as a launcher. Set
`new_terminal = false` in the config to swap this, so `enter` runs in this
terminal (pausing reserva until you exit) and the shifted keys open new
windows. When no terminal is configured at all, everything runs in this
terminal.

Terminals that do not speak the
[kitty keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/)
cannot tell `shift+enter` and `ctrl+enter` apart from `enter`. There `o`, `O`,
`t` and `T` do the same four things, and the footer switches to show them.

### Jumping to running sessions

`enter` or `shift+enter` on a session marked as running does not start a
second Claude on the same conversation. Instead reserva focuses the
window that session is running in, found by walking up from the Claude
process to the terminal that owns a window. This needs the
[niri](https://github.com/YaLTeR/niri) compositor and a terminal that runs
one process per window (Alacritty, kitty, foot, …; not daemon modes such as
`alacritty msg create-window`). Sessions inside tmux or screen cannot be
traced to a window. When no window is found, the status bar says so and
nothing is started.

## Worktrees

Worktrees live in `<repo>/.claude/worktrees/<name>`, the place Claude Code
uses itself. With the branch prompt left empty, the session runs
`claude --worktree <name>` and Claude creates the worktree on branch
`worktree-<name>`. With a branch given, reserva runs `git worktree add`
itself (creating the branch from `HEAD`, or checking it out if it already
exists) and starts Claude inside it, so the branch you push can be named
independently of the worktree. Sessions in such worktrees are listed under
the main repository with a `⎇ name` marker, and the shell keys open the
worktree directory.

Set `worktrees = false` in the config to start sessions in place by default;
`N` then does the opposite of the default.

Claude Code refuses to create a worktree in a repository whose trust dialog
has not been accepted yet under that profile. Run `claude` once in the repo
(for example with `N`, or `enter` on the header with `worktrees = false`),
accept the dialog, and `n` works from then on. The repository also needs at
least one commit. When a launch fails, the status bar shows Claude's error
and the exact command that was run.

A freshly started session has no prompt yet and is listed as "(new session,
no prompt yet)" while it runs. The list rescans every 10 seconds, so sessions
started in spawned windows appear on their own; `r` forces it.

## Configuration

`~/.config/reserva/config.toml` (or `$XDG_CONFIG_HOME/reserva/`) is
created on first run. Profiles are discovered from `~/.claude` and
`~/.claude-*` directories; each profile is a `CLAUDE_CONFIG_DIR`.

```toml
default_profile = "work"
claude_bin = "claude"
claude_args = []            # appended to every claude invocation
terminal = ""               # defaults to $TERMINAL; e.g. "wezterm start --"
new_terminal = true         # enter opens a new window; false: enter runs here
shell = ""                  # defaults to $SHELL
worktrees = true

[profiles.work]
config_dir = "~/.claude-work"

[profiles.personal]
config_dir = "~/.claude-personal"
```

Profiles appear as tabs in the order they are written. Each profile's sessions
are read from `<config_dir>/projects/*/*.jsonl`; running sessions are detected
from `<config_dir>/sessions/*.json` and marked with a dot (green busy, yellow
idle).

### New-window terminal

New windows are started with the terminal named
by `terminal` (or `$TERMINAL`). For a bare program name reserva adds the
flag that introduces the command: `wezterm start --`, `gnome-terminal --`,
`tilix --`, `ptyxis --`, nothing for `foot`, and `-e` for everything else
(alacritty, kitty, ghostty, xterm, konsole, st, …). If your terminal needs
something different, spell out the full prefix, for example
`terminal = "kitty --single-instance"`.

### State and writes

Archiving hides a session from the list without touching Claude's files; the
set of archived session IDs is kept in
`~/.local/state/reserva/archived.json` (or `$XDG_STATE_HOME`). The title
bar shows how many are hidden, `A` reveals them dimmed with an `[archived]`
tag, and `a` on one of those brings it back.

Renaming appends a `custom-title` line to the session's file, the same
record Claude Code writes for `/rename` and `--name`, so the new name also
shows up in Claude's own `/resume` picker. Custom titles take precedence over
the AI-generated title in the list. That is the only write reserva makes
to Claude's directories.

### Flags

| Flag | Meaning |
| --- | --- |
| `--profile <name>` | open this profile first |
| `--config <path>` | use another config file |
| `--list` | print the sessions and exit |

## macOS notes

reserva builds and runs on macOS, and listing, resuming, renaming,
archiving and starting sessions in the current terminal all work. It was
developed on Linux, though, and macOS has not been tested much. Things to know:

- **New windows need setup.** Opening a session in a new window starts a
  terminal binary with a command to run, and `$TERMINAL` is usually unset on
  macOS. Without one, reserva runs everything in the current terminal.
  Terminal.app and iTerm2 cannot be started this way and are not supported
  for new windows. WezTerm, kitty and Alacritty work if their CLI is
  on your `PATH`, for example:

  ```toml
  terminal = "wezterm start --"
  # or
  terminal = "/Applications/kitty.app/Contents/MacOS/kitty"
  ```

  The `ghostty` CLI on macOS cannot open windows with a command, so Ghostty
  is not supported for new windows either. Jumping to a running session's
  window is niri-only, so on macOS `enter` on a running session just reports
  that it is running. Everything that runs in the current terminal works in
  any terminal.
- **Modifier keys.** Terminal.app does not support the kitty keyboard
  protocol, so use `o` / `O` / `t` / `T` instead of the `shift`/`ctrl` +
  `enter` combinations. In iTerm2 the combinations work once
  *Report keys using CSI u* is enabled in the profile's Keys settings.
- **File locations** follow the Linux (XDG) layout, `~/.config/reserva`
  and `~/.local/state/reserva`, not `~/Library`.
