package main

import "testing"

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		text, term string
		want       bool
	}{
		{"Fix flaky login test", "flt", true},
		{"Fix flaky login test", "FLT", false}, // smart case: upper case must match as typed
		{"Fix flaky login test", "Flt", true},
		{"Fix flaky login test", "zz", false},
		{"Refactor API client", "apicl", true},
		{"Refactor API client", "API", true},
		{"", "a", false},
		{"anything", "", true},
	}
	for _, tt := range tests {
		if got := fuzzyMatch(tt.text, tt.term); got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.text, tt.term, got, tt.want)
		}
	}
}

func TestFilterScore(t *testing.T) {
	tight := &Session{Title: "login test"}
	loose := &Session{Title: "long ongoing integration test"}
	elsewhere := &Session{Title: "unrelated", Branch: "login"}
	if a, b := filterScore(tight, "login"), filterScore(loose, "login"); a <= b {
		t.Errorf("consecutive match scored %d, scattered one %d; want the first higher", a, b)
	}
	if a, b := filterScore(loose, "login"), filterScore(elsewhere, "login"); a <= b {
		t.Errorf("title match scored %d, branch-only match %d; want the title higher", a, b)
	}
}

func TestMatches(t *testing.T) {
	s := &Session{ID: "abc123", Title: "Fix flaky login test", Branch: "fix/login", Repo: "/src/webapp"}
	tests := []struct {
		filter string
		want   bool
	}{
		{"flt", true},
		{"flt webapp", true},  // fuzzy title and path substring
		{"flt mobile", false}, // every term has to match
		{"fix/log", true},
		{"abc", true},
		{"bc12", false}, // IDs match by prefix only
	}
	for _, tt := range tests {
		if got := matches(s, tt.filter); got != tt.want {
			t.Errorf("matches(%q) = %v, want %v", tt.filter, got, tt.want)
		}
	}
}
