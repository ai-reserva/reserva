package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSortGroups(t *testing.T) {
	now := time.Now()
	sess := func(id, title, branch string, age time.Duration) *Session {
		return &Session{ID: id, Title: title, Branch: branch, CWD: "/src/" + id, Modified: now.Add(-age)}
	}
	newGroups := func() []*Group {
		a := &Group{Repo: "/src/b-repo", Sessions: []*Session{sess("a1", "zeta", "main", time.Hour), sess("a2", "Alpha", "fix", 3*time.Hour)}}
		a.Latest = now.Add(-time.Hour)
		b := &Group{Repo: "/src/a-repo", Sessions: []*Session{sess("b1", "beta", "dev", 2*time.Hour)}}
		b.Latest = now.Add(-2 * time.Hour)
		return []*Group{b, a}
	}
	order := func(groups []*Group) string {
		var out []string
		for _, g := range groups {
			for _, s := range g.Sessions {
				out = append(out, s.ID)
			}
		}
		return strings.Join(out, ",")
	}
	tests := []struct {
		key  string
		desc bool
		want string
	}{
		{"activity", false, "a1,a2,b1"},
		{"activity", true, "b1,a2,a1"},
		{"path", false, "b1,a1,a2"},
		{"name", false, "b1,a2,a1"},
		{"name", true, "a1,a2,b1"},
		{"branch", false, "b1,a2,a1"},
	}
	for _, tt := range tests {
		m := &model{sortBy: slices.Index(sortKeys, tt.key), sortDesc: tt.desc}
		groups := newGroups()
		m.sortGroups(groups)
		if got := order(groups); got != tt.want {
			t.Errorf("sort %s (desc %v) = %s, want %s", tt.key, tt.desc, got, tt.want)
		}
	}
}
