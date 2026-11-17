package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Conversation search: f opens a picker that searches the messages of the
// sessions (prompts, replies, tool calls and their output) for the terms,
// lists the sessions that mention every term and previews the matches.
// There is no index; every search reads the files again.

// convMsg is one message of a conversation as the preview shows it.
type convMsg struct {
	Role string // user, assistant, call (a tool use) or tool (its result)
	Name string // the tool's name, for a call
	Time time.Time
	Text string
}

type convLine struct {
	Type        string    `json:"type"`
	IsMeta      bool      `json:"isMeta"`
	IsSidechain bool      `json:"isSidechain"`
	Timestamp   time.Time `json:"timestamp"`
	Message     *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type convPart struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

// parseConvLine returns the messages of one line of a session file. Only
// the conversation counts: records of other types, meta messages and
// subagent sidechains give none, and neither do keys such as the cwd.
func parseConvLine(line []byte) []convMsg {
	var l convLine
	if json.Unmarshal(line, &l) != nil || l.Message == nil || l.IsMeta || l.IsSidechain {
		return nil
	}
	if l.Type != "user" && l.Type != "assistant" {
		return nil
	}
	raw := bytes.TrimSpace(l.Message.Content)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s) == "" {
			return nil
		}
		return []convMsg{{Role: l.Type, Time: l.Timestamp, Text: s}}
	}
	var parts []convPart
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	var msgs []convMsg
	for _, p := range parts {
		m := convMsg{Role: l.Type, Time: l.Timestamp}
		switch p.Type {
		case "text":
			m.Text = p.Text
		case "tool_use":
			m.Role, m.Name, m.Text = "call", p.Name, toolInput(p.Input)
		case "tool_result":
			m.Role, m.Text = "tool", resultText(p.Content)
		}
		if strings.TrimSpace(m.Text) != "" {
			msgs = append(msgs, m)
		}
	}
	return msgs
}

// toolInput renders the input of a tool call: its most telling field when
// it has one of the usual ones, otherwise the JSON.
func toolInput(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) == nil {
		for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "prompt", "description"} {
			if s, ok := in[k].(string); ok && s != "" {
				return s
			}
		}
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return ""
	}
	return b.String()
}

// resultText extracts the text of a tool result, a string or text parts.
func resultText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var parts []convPart
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// convQuery is a search: its terms, matched like the filter's (smart
// case), and the lower-case, JSON-escaped probes that let lines which
// cannot match be skipped without decoding them.
type convQuery struct {
	terms  []string
	probes [][]byte
}

func newConvQuery(q string) convQuery {
	c := convQuery{terms: filterTerms(q)}
	for _, t := range c.terms {
		esc, _ := json.Marshal(strings.ToLower(t))
		c.probes = append(c.probes, esc[1:len(esc)-1])
	}
	return c
}

// mayMatch reports whether the raw line mentions any term at all.
func (q convQuery) mayMatch(line []byte) bool {
	lower := bytes.ToLower(line)
	for _, p := range q.probes {
		if bytes.Contains(lower, p) {
			return true
		}
	}
	return false
}

// termIn reports whether text contains term, ignoring case unless the term
// has an upper-case letter.
func termIn(text, term string) bool {
	if caseSensitive(term) {
		return strings.Contains(text, term)
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(term))
}

// mentions reports which terms text contains, as a bit set.
func (q convQuery) mentions(text string) uint64 {
	var bits uint64
	for i, t := range q.terms {
		if i < 64 && termIn(text, t) {
			bits |= 1 << i
		}
	}
	return bits
}

func (q convQuery) allBits() uint64 {
	if len(q.terms) >= 64 {
		return ^uint64(0)
	}
	return 1<<len(q.terms) - 1
}

// convHit is a session whose conversation mentions every term.
type convHit struct {
	Session *Session
	Any     int // messages that mention a term
	All     int // messages that mention every term
}

// scanConversation reads a session file and counts the messages that
// mention the terms. ok is false unless every term occurs somewhere in the
// conversation, or when ctx is cancelled.
func scanConversation(ctx context.Context, path string, q convQuery) (hit convHit, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return hit, false
	}
	defer f.Close()
	var seen uint64
	r := bufio.NewReaderSize(f, 1<<20)
	for n := 0; ; n++ {
		if n%256 == 0 && ctx.Err() != nil {
			return hit, false
		}
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && q.mayMatch(line) {
			for _, m := range parseConvLine(line) {
				bits := q.mentions(m.Text)
				if bits == 0 {
					continue
				}
				seen |= bits
				hit.Any++
				if bits == q.allBits() {
					hit.All++
				}
			}
		}
		if err != nil {
			return hit, len(q.terms) > 0 && seen == q.allBits()
		}
	}
}

// searchSessions scans the conversations in parallel and sends every hit
// on out, which it closes when done or when ctx is cancelled.
func searchSessions(ctx context.Context, sessions []*Session, q convQuery, out chan<- convHit) {
	jobs := make(chan *Session)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if hit, ok := scanConversation(ctx, s.Path, q); ok {
					hit.Session = s
					select {
					case out <- hit:
					case <-ctx.Done():
					}
				}
			}
		}()
	}
	for _, s := range sessions {
		select {
		case jobs <- s:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	close(out)
}

// readConversation returns every message of a session file, for the
// preview.
func readConversation(path string) ([]convMsg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var msgs []convMsg
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			msgs = append(msgs, parseConvLine(line)...)
		}
		if err != nil {
			return msgs, nil
		}
	}
}
