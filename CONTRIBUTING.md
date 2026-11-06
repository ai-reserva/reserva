# Contributing to reserva

Contributions are welcome, including pull requests written entirely by an
AI agent. However a change was made, the person opening the pull request is
responsible for it: read the diff, understand it, and be ready to answer
questions about it.

Every contribution follows the principles below.

## Be respectful

Be respectful to everyone, in issues, pull requests, reviews and commit
messages, under any circumstance. No insults, harassment or belittling,
whether aimed at maintainers, contributors or users. Disagree with ideas,
not people. Contributions or comments that break this rule are closed
without further discussion.

## Keep mandatory dependencies to a minimum

reserva should stay a single binary that needs only `claude` and `git`.

- Prefer the Go standard library. A new Go module needs a clear reason, and
  the pull request should say why the standard library or an existing
  dependency is not enough.
- Integrations with external tools (`glab`, `gh`, `tea`, `niri`,
  AppleScript, …) stay optional. When the tool is missing, reserva keeps
  working and says what is unavailable, rather than failing.
- No new runtime services, daemons or network calls that are on by default.

## Keep pull requests small

- One change per pull request. A bug fix, a feature and a refactor are three
  pull requests.
- Prefer a series of small pull requests that each work on their own over
  one large one. Small changes get reviewed and merged quickly; large ones
  wait.
- Keep unrelated cleanups, reformatting and renames out of the diff.
- For larger features or changes in behaviour, open an issue first to agree
  on the approach.

## Do not introduce regressions

Before opening a pull request:

- `gofmt -l .` prints nothing, and `go vet ./...` and `go build ./...` pass.
  `golangci-lint run` should pass too (it is in `nix develop`).
- `go test ./...` passes. Add tests for logic that can be tested without a
  terminal, such as parsing session files, config handling and matching
  merge requests to sessions.
- Run reserva and try the change, along with the features around it:
  listing, resuming, starting new sessions, switching profiles. `reserva
  --list` is a quick check that sessions still load.
- Keep existing keys, flags and config options working. If a change in
  behaviour is intended, say so in the pull request and update the README.

## Respect the user's data

- reserva reads Claude's files and only ever writes the rename record (see
  the README). Do not add other writes to Claude's directories. reserva's own
  state belongs under `~/.local/state/reserva/`.
- Session files contain private conversations. Do not log, upload or send
  their contents anywhere.

## Support Linux and macOS

reserva runs on both. Do not break one while working on the other.
Platform-specific code goes in `_linux.go` / `_darwin.go` / `_other.go`
files, like `focus_*.go`. Say in the pull request which platforms you
tested on.

## Document what users see

When a change affects keys, flags, config options or behaviour, update the
README in the same pull request.

## Commit messages

Follow the style of the existing history: a short subject in the imperative
mood ("Show open pull requests from GitHub per session"), then a body that
explains why the change was made and anything that is not obvious from the
diff. Agent-written commits may carry a `Co-Authored-By` trailer.
