package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Lines as Claude Code writes them: assistant records put the message
// before the type, and every record carries the cwd and branch.
var convLines = []string{
	`{"parentUuid":null,"cwd":"/home/u/reserva","gitBranch":"main","type":"user","message":{"role":"user","content":"fix the keepass sync"},"timestamp":"2026-11-03T14:02:00Z"}`,
	`{"parentUuid":"a","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Looking at the vault code"}]},"cwd":"/home/u/reserva","gitBranch":"main","type":"assistant"}`,
	`{"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"grep -rn Vault ."}}]},"cwd":"/home/u/reserva","type":"assistant"}`,
	`{"cwd":"/home/u/reserva","type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"vault.go:12: undefined: Vault"}]}]}}`,
	`{"type":"user","isMeta":true,"message":{"role":"user","content":"<command-name>keepass-meta</command-name>"}}`,
	`{"type":"user","isSidechain":true,"message":{"role":"user","content":"sidechain about Gitea"}}`,
	`{"type":"last-prompt","lastPrompt":"Gitea tokens"}`,
}

func writeConv(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadConversation(t *testing.T) {
	msgs, err := readConversation(writeConv(t, convLines...))
	if err != nil {
		t.Fatal(err)
	}
	want := []convMsg{
		{Role: "user", Text: "fix the keepass sync"},
		{Role: "assistant", Text: "Looking at the vault code"},
		{Role: "call", Name: "Bash", Text: "grep -rn Vault ."},
		{Role: "tool", Text: "vault.go:12: undefined: Vault"},
	}
	if len(msgs) != len(want) {
		t.Fatalf("got %d messages %+v, want %d", len(msgs), msgs, len(want))
	}
	for i, w := range want {
		g := msgs[i]
		if g.Role != w.Role || g.Name != w.Name || g.Text != w.Text {
			t.Errorf("message %d = %+v, want %+v", i, g, w)
		}
	}
	if msgs[0].Time.IsZero() {
		t.Error("the user message's timestamp was not read")
	}
}

func TestScanConversation(t *testing.T) {
	path := writeConv(t, convLines...)
	tests := []struct {
		query    string
		ok       bool
		any, all int
	}{
		{"keepass sync", true, 1, 1}, // both terms in one message
		{"vault", true, 3, 3},        // reply, tool call and its output
		{"keepass vault", true, 4, 0},
		{"VAULT", true, 3, 3},    // case is ignored
		{"reserva", false, 0, 0}, // only in the cwd
		{"main", false, 0, 0},    // only in the branch
		{"gitea", false, 0, 0},   // sidechain and last-prompt records
		{"keepass missing", false, 1, 0},
		{"", false, 0, 0},
	}
	for _, tt := range tests {
		hit, ok := scanConversation(context.Background(), path, newConvQuery(tt.query))
		if ok != tt.ok || hit.Any != tt.any || hit.All != tt.all {
			t.Errorf("%q: ok=%v any=%d all=%d, want ok=%v any=%d all=%d", tt.query, ok, hit.Any, hit.All, tt.ok, tt.any, tt.all)
		}
	}
	if _, ok := scanConversation(context.Background(), filepath.Join(t.TempDir(), "none.jsonl"), newConvQuery("x")); ok {
		t.Error("unreadable file, want no hit")
	}
}

func TestScanConversationEscapedTerm(t *testing.T) {
	path := writeConv(t, `{"type":"user","message":{"content":"say \"hi\" twice"}}`)
	if _, ok := scanConversation(context.Background(), path, newConvQuery(`"hi"`)); !ok {
		t.Error(`a term with quotes should match the JSON-escaped text`)
	}
}

func TestSearchSessions(t *testing.T) {
	dir := t.TempDir()
	var sessions []*Session
	for _, c := range []struct{ id, text string }{{"a", "keepass sync"}, {"b", "nothing here"}, {"c", "Keepass and the sync loop"}} {
		p := filepath.Join(dir, c.id+".jsonl")
		if err := os.WriteFile(p, []byte(`{"type":"user","message":{"content":"`+c.text+`"}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, &Session{ID: c.id, Path: p})
	}
	out := make(chan convHit)
	go searchSessions(context.Background(), sessions, newConvQuery("keepass sync"), out)
	got := map[string]bool{}
	for h := range out {
		got[h.Session.ID] = true
	}
	if len(got) != 2 || !got["a"] || !got["c"] {
		t.Errorf("hits = %v, want a and c", got)
	}
}

func TestCleanText(t *testing.T) {
	if got := cleanText("\x1b[31mred\x1b[0m\tx\ny"); got != "[31mred[0m    x\ny" {
		t.Errorf("cleanText = %q", got)
	}
}
