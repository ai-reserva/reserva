package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The conversation picker: f opens it, typing searches the conversations
// of the tab's sessions, the matching sessions are listed on the left and
// the selected one's matches are previewed in context on the right (below
// the list on narrow terminals).

// finder is the picker's state. It lives behind a pointer so that it
// survives leaving and reopening the picker.
type finder struct {
	query   string
	gen     int // bumped on every new search; messages of older ones are dropped
	cancel  context.CancelFunc
	out     chan convHit // hits of the running search
	running bool
	total   int // sessions being searched
	hits    []convHit
	cursor  int
	offset  int
	moved   bool // the cursor was moved since the search started
	preview *convPreview
}

// convPreview is the loaded conversation of the selected hit.
type convPreview struct {
	id      string
	msgs    []convMsg
	lines   [][]string // msgs split into lines
	err     error
	query   string // the query matches were found for
	matches []int  // messages that mention a term
	cur     int    // index into matches shown at the top
}

// findTickMsg fires when typing has paused, to start the search.
type findTickMsg struct{ gen int }

type findHitsMsg struct {
	gen  int
	hits []convHit
	done bool
}

type previewMsg struct {
	id   string
	msgs []convMsg
	err  error
}

// findDelay is how long typing has to pause before a search starts, so
// that a search is not started and cancelled for every key.
const findDelay = 150 * time.Millisecond

func (m *model) openFinder() tea.Cmd {
	if m.find == nil {
		m.find = &finder{}
	}
	m.mode = modeFind
	m.input = m.find.query
	// Sessions may have changed since the last search, so it runs again.
	return m.startSearch()
}

func (m *model) closeFinder() {
	m.mode = modeList
	m.find.stop()
}

func (f *finder) stop() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.running = false
}

// startSearch cancels the running search and starts one for the query.
func (m *model) startSearch() tea.Cmd {
	f := m.find
	f.stop()
	f.gen++
	f.hits, f.cursor, f.offset, f.moved = nil, 0, 0, false
	q := newConvQuery(f.query)
	if len(q.terms) == 0 {
		return nil
	}
	var sessions []*Session
	for _, g := range m.pd().groups {
		for _, s := range g.Sessions {
			if !s.Archived || m.showArchived {
				sessions = append(sessions, s)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel, f.running, f.total = cancel, true, len(sessions)
	f.out = make(chan convHit, 64)
	go searchSessions(ctx, sessions, q, f.out)
	return waitHits(f.gen, f.out)
}

// waitHits waits for the next hits of a search, taking whatever else has
// arrived meanwhile too.
func waitHits(gen int, out chan convHit) tea.Cmd {
	return func() tea.Msg {
		hit, ok := <-out
		if !ok {
			return findHitsMsg{gen: gen, done: true}
		}
		msg := findHitsMsg{gen: gen, hits: []convHit{hit}}
		for len(msg.hits) < 256 {
			select {
			case hit, ok := <-out:
				if !ok {
					msg.done = true
					return msg
				}
				msg.hits = append(msg.hits, hit)
			default:
				return msg
			}
		}
		return msg
	}
}

// addHits merges arriving hits into the list, best first: sessions with
// more messages that mention every term, then more that mention one, then
// the most recent. The cursor stays on the best one until it is moved,
// then on the session it was moved to.
func (f *finder) addHits(hits []convHit) {
	sel := f.selected()
	f.hits = append(f.hits, hits...)
	sort.SliceStable(f.hits, func(i, j int) bool {
		a, b := f.hits[i], f.hits[j]
		if a.All != b.All {
			return a.All > b.All
		}
		if a.Any != b.Any {
			return a.Any > b.Any
		}
		return a.Session.Modified.After(b.Session.Modified)
	})
	f.cursor = 0
	for i, h := range f.hits {
		if f.moved && h.Session == sel {
			f.cursor = i
		}
	}
}

func (f *finder) selected() *Session {
	if f.cursor < len(f.hits) {
		return f.hits[f.cursor].Session
	}
	return nil
}

// previewCmd loads the selected session's conversation unless it is the
// one already loaded, and finds the query's matches in it.
func (m *model) previewCmd() tea.Cmd {
	f := m.find
	s := f.selected()
	if s == nil {
		return nil
	}
	if p := f.preview; p != nil && p.id == s.ID {
		if p.query != f.query {
			p.findMatches(f.query)
		}
		return nil
	}
	f.preview = &convPreview{id: s.ID}
	return func() tea.Msg {
		msgs, err := readConversation(s.Path)
		return previewMsg{id: s.ID, msgs: msgs, err: err}
	}
}

func (p *convPreview) findMatches(query string) {
	q := newConvQuery(query)
	p.query, p.matches, p.cur = query, nil, 0
	best := -1
	for i, msg := range p.msgs {
		bits := q.mentions(msg.Text)
		if bits == 0 {
			continue
		}
		if bits == q.allBits() && best < 0 {
			best = len(p.matches)
		}
		p.matches = append(p.matches, i)
	}
	// Start on the first message that mentions every term, if any does.
	if best > 0 {
		p.cur = best
	}
}

func (m model) updateFinder(msg tea.Msg) (tea.Model, tea.Cmd) {
	f := m.find
	switch msg := msg.(type) {
	case findTickMsg:
		if msg.gen == f.gen && m.mode == modeFind {
			return m, m.startSearch()
		}
	case findHitsMsg:
		if msg.gen != f.gen || !f.running {
			return m, nil
		}
		f.addHits(msg.hits)
		if msg.done {
			f.stop()
		}
		m.clampFind()
		cmds := []tea.Cmd{m.previewCmd()}
		if !msg.done {
			cmds = append(cmds, waitHits(msg.gen, f.out))
		}
		return m, tea.Batch(cmds...)
	case previewMsg:
		if p := f.preview; p != nil && p.id == msg.id {
			p.msgs, p.err = msg.msgs, msg.err
			p.lines = make([][]string, len(msg.msgs))
			for i, cm := range msg.msgs {
				p.lines[i] = strings.Split(cleanText(cm.Text), "\n")
			}
			p.findMatches(f.query)
		}
	}
	return m, nil
}

func (m model) handleFindKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.find
	switch key := msg.String(); key {
	case "esc":
		m.closeFinder()
		return m, nil
	case "enter", "shift+enter", "alt+enter":
		s := f.selected()
		if s == nil {
			return m, nil
		}
		m.closeFinder()
		return m, m.resume(s, m.cfg.NewTerminalDefault() != (key != "enter"))
	case "down", "ctrl+n":
		f.cursor, f.moved = f.cursor+1, true
	case "up", "ctrl+p":
		f.cursor, f.moved = f.cursor-1, true
	case "pgdown":
		f.cursor, f.moved = f.cursor+m.findListHeight(), true
	case "pgup":
		f.cursor, f.moved = f.cursor-m.findListHeight(), true
	case "ctrl+f", "ctrl+b":
		if p := f.preview; p != nil && len(p.matches) > 0 {
			d := 1
			if key == "ctrl+b" {
				d = -1
			}
			p.cur = (p.cur + d + len(p.matches)) % len(p.matches)
		}
		return m, nil
	default:
		input := editText(m.input, msg)
		if input == m.input {
			return m, nil
		}
		m.input, f.query = input, input
		f.stop()
		f.gen++
		f.hits, f.cursor, f.offset, f.moved = nil, 0, 0, false
		gen := f.gen
		return m, tea.Tick(findDelay, func(time.Time) tea.Msg { return findTickMsg{gen: gen} })
	}
	m.clampFind()
	return m, m.previewCmd()
}

// findSplit reports whether the preview goes beside the list rather than
// below it, and how wide the list is.
func (m *model) findSplit() (side bool, listW int) {
	w := m.viewWidth()
	if w < 100 {
		return false, w
	}
	return true, min(max(w*2/5, 30), 60)
}

func (m *model) findListHeight() int {
	h := m.listHeight() - 1 // the count line
	if side, _ := m.findSplit(); !side {
		h = max(2, m.listHeight()/3) - 1
	}
	return max(1, h)
}

func (m *model) clampFind() {
	f := m.find
	f.cursor = max(0, min(f.cursor, len(f.hits)-1))
	h := m.findListHeight()
	if f.cursor < f.offset {
		f.offset = f.cursor
	}
	if f.cursor >= f.offset+h {
		f.offset = f.cursor - h + 1
	}
}

// findView renders the picker in the list area: hits and preview.
func (m model) findView(w int) string {
	h := m.listHeight()
	side, listW := m.findSplit()
	list := m.findList(listW, m.findListHeight())
	var lines []string
	if side {
		prev := m.find.previewLines(w-listW-3, h)
		for i := range h {
			left := ""
			if i < len(list) {
				left = list[i]
			}
			lines = append(lines, pad(left, listW)+styleRule.Render(" │ ")+prev[i])
		}
	} else {
		lines = append(lines, list...)
		lines = append(lines, styleRule.Render(strings.Repeat("┄", w)))
		lines = append(lines, m.find.previewLines(w, max(h-len(lines), 0))...)
	}
	for i := range lines {
		lines[i] = truncate(lines[i], w)
	}
	return strings.Join(lines, "\n") + "\n"
}

// findList renders the hits, then a line that counts them.
func (m model) findList(w, h int) []string {
	f := m.find
	var lines []string
	for i := f.offset; i < len(f.hits) && i < f.offset+h; i++ {
		s := f.hits[i].Session
		title := s.Title
		if title == "" {
			title = "(no prompt yet)"
		}
		if m.all() {
			title = s.Profile + " " + title
		}
		n := fmt.Sprintf(" %d", f.hits[i].Any)
		text := pad(truncate(" "+title, w-len(n)), w-len(n))
		if i == f.cursor {
			lines = append(lines, styleRowOn.Render(text+n))
		} else {
			lines = append(lines, text+styleDim.Render(n))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	count := ""
	switch {
	case len(newConvQuery(f.query).terms) == 0:
		count = "type to search the conversations"
	case f.running:
		count = fmt.Sprintf("%d of %d sessions · searching…", len(f.hits), f.total)
	case len(f.hits) == 0:
		count = fmt.Sprintf("no conversation mentions every term (%d searched)", f.total)
	default:
		count = fmt.Sprintf("%d of %d sessions", len(f.hits), f.total)
	}
	return append(lines, styleDim.Render(" "+truncate(count, w-1)))
}

// previewLines renders h lines of the selected conversation, w wide: the
// current match and the messages around it, the terms highlighted.
func (f *finder) previewLines(w, h int) []string {
	var out []string
	p := f.preview
	switch s := f.selected(); {
	case p == nil || s == nil || p.id != s.ID:
	case p.err != nil:
		out = append(out, styleErr.Render(p.err.Error()))
	case p.msgs == nil:
		out = append(out, styleDim.Render("loading…"))
	case len(p.matches) == 0:
		out = append(out, styleDim.Render("no message mentions the terms"))
	default:
		terms := filterTerms(p.query)
		out = append(out, styleDim.Render(fmt.Sprintf("match %d of %d  (ctrl+f/b next/previous)", p.cur+1, len(p.matches))))
		at := p.matches[p.cur]
		// One message of context before the match, a few lines of it.
		if at > 0 {
			out = append(out, p.render(at-1, w, 3, nil)...)
		}
		for i := at; i < len(p.msgs) && len(out) < h; i++ {
			lim := 6
			if i == at {
				lim = h
			}
			out = append(out, p.render(i, w, lim, terms)...)
		}
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// render renders message i: a header, then up to lim lines of its text
// wrapped to w. With terms, the text starts a little above the first line
// that mentions one, and the terms are highlighted.
func (p *convPreview) render(i, w, lim int, terms []string) []string {
	msg := p.msgs[i]
	var head string
	switch msg.Role {
	case "user":
		head = styleHeader.Render("user")
	case "assistant":
		head = styleTitle.Render("claude")
	case "call":
		head = styleMR.Render("→ " + msg.Name)
	case "tool":
		head = styleDim.Render("tool output")
	}
	if !msg.Time.IsZero() {
		head += styleDim.Render(" · " + msg.Time.Local().Format("2 Jan 15:04"))
	}
	out := []string{"", head}
	src := p.lines[i]
	from := 0
	if terms != nil {
		for j, l := range src {
			if anyTermIn(l, terms) {
				from = max(0, j-2)
				break
			}
		}
	}
	var body []string
	if from > 0 {
		body = append(body, styleDim.Render("  …"))
	}
	for _, l := range src[from:] {
		if len(body) >= lim {
			break
		}
		// Only what can be shown is wrapped; a line of a tool's output can
		// be megabytes long.
		if n := w * lim; len(l) > n {
			l = strings.ToValidUTF8(l[:n], "")
		}
		for _, wl := range strings.Split(ansi.Wrap(l, max(w-2, 10), ""), "\n") {
			if terms != nil {
				wl = highlight(wl, terms)
			}
			body = append(body, "  "+wl)
		}
	}
	if len(body) > lim {
		body = append(body[:lim-1], styleDim.Render("  …"))
	}
	return append(out, body...)
}

func anyTermIn(text string, terms []string) bool {
	for _, t := range terms {
		if termIn(text, t) {
			return true
		}
	}
	return false
}

// highlight marks the terms in text, matched like termIn.
func highlight(text string, terms []string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		n := 0
		for _, t := range terms {
			if i+len(t) > len(text) {
				continue
			}
			if strings.EqualFold(text[i:i+len(t)], t) {
				n = max(n, len(t))
			}
		}
		if n == 0 {
			b.WriteByte(text[i])
			i++
			continue
		}
		b.WriteString(styleMatch.Render(text[i : i+n]))
		i += n
	}
	return b.String()
}

// cleanText makes text from a session file safe to print: tabs become
// spaces and other control characters, such as the escape sequences of
// coloured tool output, are dropped.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r >= ' ' && r != 0x7f {
			return r
		}
		return -1
	}, s)
}
