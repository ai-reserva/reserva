package main

import (
	"strings"
	"unicode"

	"github.com/sahilm/fuzzy"
)

// A filter is split into terms on whitespace; every term has to match. A
// term with an upper-case letter is matched case-sensitively, like fzf's
// smart case.
func filterTerms(filter string) []string {
	return strings.Fields(filter)
}

func caseSensitive(term string) bool {
	return strings.IndexFunc(term, unicode.IsUpper) >= 0
}

// fuzzyScore scores term against text with sahilm/fuzzy (Sublime Text style:
// consecutive and word-initial matches score more, unmatched characters
// cost a little). ok is false when term does not match. The matcher always
// ignores case, so a smart-case term must also occur in text, in order,
// exactly as typed.
func fuzzyScore(text, term string) (score int, ok bool) {
	if term == "" {
		return 0, true
	}
	if caseSensitive(term) && !subsequence(text, term) {
		return 0, false
	}
	ms := fuzzy.FindNoSort(term, []string{text})
	if len(ms) == 0 {
		return 0, false
	}
	return ms[0].Score, true
}

// subsequence reports whether the runes of term occur in text in order.
func subsequence(text, term string) bool {
	rest := []rune(term)
	for _, r := range text {
		if len(rest) > 0 && r == rest[0] {
			rest = rest[1:]
		}
	}
	return len(rest) == 0
}

// fuzzyMatch reports whether term fuzzy-matches text.
func fuzzyMatch(text, term string) bool {
	_, ok := fuzzyScore(text, term)
	return ok
}

// titleMatch lifts any fuzzy match on the title above terms that only match
// elsewhere, since fuzzy scores can be negative.
const titleMatch = 1 << 16

// filterScore ranks a session under the filter: the fuzzy scores of the
// terms on its title added up. Terms that only match elsewhere (prompt,
// branch, path, content) add nothing, so the list orders like fzf would.
func filterScore(s *Session, f string) int {
	total := 0
	for _, term := range filterTerms(f) {
		if score, ok := fuzzyScore(s.Title, term); ok {
			total += titleMatch + score
		}
	}
	return total
}

// matchesAny reports whether term fuzzy-matches one of the texts.
func matchesAny(term string, texts ...string) bool {
	for _, t := range texts {
		if fuzzyMatch(t, term) {
			return true
		}
	}
	return false
}
