package main

import (
	"testing"
	"time"
)

func TestFold(t *testing.T) {
	now := time.Now()
	groups := []*Group{
		{Repo: "/src/a", Latest: now, Sessions: []*Session{{ID: "a1", Title: "one", Modified: now}, {ID: "a2", Title: "two", Modified: now}}},
		{Repo: "/src/b", Latest: now.Add(-time.Hour), Sessions: []*Session{{ID: "b1", Title: "three", Modified: now.Add(-time.Hour)}}},
	}
	m := &model{
		profiles: []string{"p"},
		data:     map[string]*profileData{"p": {groups: groups, loaded: true}},
		depth:    map[string]int{},
	}
	m.rebuildRows()
	if len(m.rows) != 5 {
		t.Fatalf("got %d rows, want 2 headers and 3 sessions", len(m.rows))
	}

	m.cursor = 2 // a2, a session in /src/a
	m.toggleFold()
	if len(m.rows) != 3 || m.cursor != 0 || !m.rows[0].header() {
		t.Errorf("after z on a session: %d rows, cursor %d; want /src/a folded with the cursor on its header", len(m.rows), m.cursor)
	}
	if got := m.foldMark(groups[0]); got != "[+]" {
		t.Errorf("foldMark = %q, want [+]", got)
	}
	m.toggleFold()
	if len(m.rows) != 5 {
		t.Errorf("z on a folded project: %d rows, want it open again", len(m.rows))
	}

	m.foldAll(!m.anyOpen())
	if len(m.rows) != 2 || m.anyOpen() {
		t.Errorf("Z: %d rows, want only the two headers", len(m.rows))
	}
	m.foldAll(!m.anyOpen())
	if len(m.rows) != 5 {
		t.Errorf("Z with everything folded: %d rows, want all open", len(m.rows))
	}
}
