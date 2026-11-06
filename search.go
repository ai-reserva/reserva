package main

import (
	"bufio"
	"bytes"
	"os"
	"runtime"
	"strings"
	"sync"
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

// indexFold finds term in line ignoring ASCII case, like a case-insensitive
// bytes.Index. term is lower-case.
func indexFold(line, term []byte) int {
	if len(term) == 0 {
		return 0
	}
	lower := term[0]
	upper := lower
	if lower >= 'a' && lower <= 'z' {
		upper = lower - 'a' + 'A'
	}
	for i := 0; i+len(term) <= len(line); i++ {
		if line[i] != lower && line[i] != upper {
			continue
		}
		if bytes.EqualFold(line[i:i+len(term)], term) {
			return i
		}
	}
	return -1
}

// snippet cuts the text around a match out of a line of the session file
// (raw JSON, so escapes are undone roughly) for the status bar.
func snippet(line []byte, at, n int) string {
	const around = 40
	start := max(at-around, 0)
	end := min(at+n+around, len(line))
	s := string(line[start:end])
	s = strings.NewReplacer(`\n`, " ", `\t`, " ", `\"`, `"`, `\\`, `\`).Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(line) {
		s += "…"
	}
	return s
}

// scanFile reports whether every term occurs somewhere in the session file
// (prompts, replies and tool output alike) and returns a snippet around the
// first term's first occurrence. Terms are matched ignoring case, as plain
// text, not fuzzily: on whole conversations a fuzzy match means nothing.
func scanFile(path string, terms [][]byte) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	found := make([]bool, len(terms))
	left := len(terms)
	hit := ""
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		for i, t := range terms {
			if found[i] {
				continue
			}
			if at := indexFold(line, t); at >= 0 {
				found[i] = true
				left--
				if i == 0 {
					hit = snippet(line, at, len(t))
				}
			}
		}
		if left == 0 {
			return hit, true
		}
		if err != nil {
			return "", false
		}
	}
}

// scanSessions searches the files of the sessions for the terms, in
// parallel, and returns a snippet per matching session ID.
func scanSessions(sessions []*Session, terms []string) map[string]string {
	lower := make([][]byte, len(terms))
	for i, t := range terms {
		lower[i] = []byte(strings.ToLower(t))
	}
	var mu sync.Mutex
	hits := map[string]string{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, s := range sessions {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if snip, ok := scanFile(s.Path, lower); ok {
				mu.Lock()
				hits[s.ID] = snip
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return hits
}
