package main

import (
	"testing"
	"time"
)

func TestProjectFromRemote(t *testing.T) {
	tests := []struct{ remote, want string }{
		{"git@github.com:ai-reserva/reserva.git", "ai-reserva/reserva"},
		{"https://github.com/ai-reserva/reserva.git", "ai-reserva/reserva"},
		{"https://gitlab.com/group/sub/repo", "group/sub/repo"},
		{"ssh://git@gitlab.example.com:2222/group/repo.git", "group/repo"},
		{"/local/path/repo", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := projectFromRemote(tt.remote); got != tt.want {
			t.Errorf("projectFromRemote(%q) = %q, want %q", tt.remote, got, tt.want)
		}
	}
}

func TestApplyMRs(t *testing.T) {
	now := time.Now()
	linked := &MR{URL: "https://x/pr/1", Project: "org/app", SourceBranch: "other", Updated: now.Add(-time.Hour)}
	byBranch := &MR{URL: "https://x/pr/2", Project: "Org/App", SourceBranch: "feature", Updated: now}
	elsewhere := &MR{URL: "https://x/pr/3", Project: "org/lib", SourceBranch: "feature", Updated: now}

	withLink := &Session{ID: "a", Branch: "feature", PRLinks: []string{linked.URL, byBranch.URL}}
	onBranch := &Session{ID: "b", Branch: "feature"}
	unrelated := &Session{ID: "c", Branch: "main", MRs: []*MR{elsewhere}}
	groups := []*Group{{Project: "org/app", Sessions: []*Session{withLink, onBranch, unrelated}}}

	applyMRs(groups, []*MR{linked, byBranch, elsewhere})

	if got := withLink.MRs; len(got) != 2 || got[0] != byBranch || got[1] != linked {
		t.Errorf("linked session MRs = %v, want [pr/2 pr/1] without duplicates, newest first", got)
	}
	if got := onBranch.MRs; len(got) != 1 || got[0] != byBranch {
		t.Errorf("branch session MRs = %v, want [pr/2] matched case-insensitively by project", got)
	}
	if len(unrelated.MRs) != 0 {
		t.Errorf("unrelated session MRs = %v, want stale MRs cleared", unrelated.MRs)
	}
}

func TestDecodeArrays(t *testing.T) {
	got, err := decodeArrays[int]([]byte("[1,2]\n[3]\n[]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("decodeArrays = %v, want [1 2 3]", got)
	}
	if _, err := decodeArrays[int]([]byte("[1] [oops")); err == nil {
		t.Error("want an error for truncated output")
	}
}

func TestGitlabStatus(t *testing.T) {
	tests := []struct {
		mr   gitlabMR
		want string
	}{
		{gitlabMR{MergeStatus: "mergeable", HasConflicts: true}, "conflicts"},
		{gitlabMR{MergeStatus: "mergeable"}, "mergeable"},
		{gitlabMR{MergeStatus: "not_approved"}, "needs approval"},
		{gitlabMR{MergeStatus: "need_rebase"}, "needs rebase"},
		{gitlabMR{MergeStatus: "ci_still_running"}, "ci"},
		{gitlabMR{MergeStatus: "checking"}, ""},
	}
	for _, tt := range tests {
		if got := gitlabStatus(tt.mr); got != tt.want {
			t.Errorf("gitlabStatus(%+v) = %q, want %q", tt.mr, got, tt.want)
		}
	}
}
