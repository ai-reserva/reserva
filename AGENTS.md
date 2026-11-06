# Instructions for AI agents

Always follow [CONTRIBUTING.md](CONTRIBUTING.md) when making changes to this
repository. Read it before starting work. In short:

- Do not add Go modules or required external tools unless the user asks for
  it and agrees it is needed. External integrations stay optional.
- Keep each change small and focused on one thing. Do not reformat, rename or
  clean up code that the task does not touch. If a task is large, propose
  splitting it into several pull requests.
- Before calling a change done, run `gofmt -l .`, `go vet ./...`,
  `go build ./...` and `go test ./...`, and fix anything they report. Add
  tests for testable logic you change.
- Never write to Claude's directories beyond the existing rename record, and
  never log or send session contents anywhere.
- Keep Linux and macOS working; put platform-specific code in
  `_linux.go` / `_darwin.go` / `_other.go` files.
- Update the README when keys, flags, config options or behaviour change.
- Be respectful in everything you write: code comments, commit messages,
  pull request descriptions and replies to reviewers.
