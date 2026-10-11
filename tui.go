package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type mode int

const (
	modeList mode = iota
	modeFilter
	modeRepo
	modeName
	modeBranch
	modeRename
	modeHelp
)

type row struct {
	group *Group
	sess  *Session // nil for a group header
}

type profileData struct {
	groups  []*Group
	loaded  bool
	loading bool
	err     error
}

type loadedMsg struct {
	profile string
	groups  []*Group
	err     error
}

type liveMsg struct{}

type rescanMsg struct{}

type execDoneMsg struct {
	name    string
	cmdline string
	stderr  string
	err     error
}

type model struct {
	cfg        *Config
	profiles   []string
	profile    int
	data       map[string]*profileData
	rows       []row
	cursor     int
	offset     int
	width      int
	height     int
	mode       mode
	input      string
	filter     string
	status     string
	lastKey    string
	launchWT   bool     // pending new-session: use a worktree
	launchIn   string   // pending new-session: repo dir
	launchName string   // pending new-session: worktree / session name
	kbOK       bool     // terminal reports shift+enter / ctrl+enter
	compl      []string // completion candidates shown in the repo prompt

	archive      *archiveState
	showArchived bool
}

var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	styleTab      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleTabOn    = lipgloss.NewStyle().Bold(true).Reverse(true)
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	styleHeaderOn = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Reverse(true)
	styleRow      = lipgloss.NewStyle()
	styleRowOn    = lipgloss.NewStyle().Reverse(true)
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleBusy     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleIdle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleKey      = lipgloss.NewStyle().Bold(true)
	styleRule     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func newModel(cfg *Config, startProfile string) model {
	m := model{
		cfg:      cfg,
		profiles: cfg.ProfileNames(),
		data:     map[string]*profileData{},
		archive:  loadArchive(defaultStatePath()),
	}
	for i, p := range m.profiles {
		m.data[p] = &profileData{}
		if p == startProfile {
			m.profile = i
		}
	}
	return m
}

func (m model) current() Profile { return m.cfg.Profiles[m.profiles[m.profile]] }

func (m model) pd() *profileData { return m.data[m.profiles[m.profile]] }

func (m model) loadCmd(name string) tea.Cmd {
	p := m.cfg.Profiles[name]
	return func() tea.Msg {
		groups, err := loadProfile(p)
		return loadedMsg{profile: name, groups: groups, err: err}
	}
}

func liveTick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return liveMsg{} })
}

func rescanTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return rescanMsg{} })
}

func (m model) Init() tea.Cmd {
	m.pd().loading = true
	return tea.Batch(m.loadCmd(m.profiles[m.profile]), liveTick(), rescanTick())
}

// rebuildRows flattens groups into the visible list, applying the filter.
func (m *model) rebuildRows() {
	var sel *Session
	var selRepo string
	if m.cursor < len(m.rows) {
		sel = m.rows[m.cursor].sess
		selRepo = m.rows[m.cursor].group.Repo
	}
	m.rows = m.rows[:0]
	f := strings.ToLower(m.filter)
	for _, g := range m.pd().groups {
		var keep []*Session
		for _, s := range g.Sessions {
			if s.Archived && !m.showArchived {
				continue
			}
			if f == "" || matches(s, f) {
				keep = append(keep, s)
			}
		}
		if len(keep) == 0 && (f != "" || !m.showArchived && m.archivedIn(g) > 0) {
			if f == "" || !strings.Contains(strings.ToLower(g.Repo), f) {
				continue
			}
		}
		m.rows = append(m.rows, row{group: g})
		for _, s := range keep {
			m.rows = append(m.rows, row{group: g, sess: s})
		}
	}
	// Try to keep the cursor on the same item, else on the first session.
	m.cursor = -1
	for i, r := range m.rows {
		if sel != nil && r.sess != nil && r.sess.ID == sel.ID {
			m.cursor = i
			break
		}
		if sel == nil && selRepo != "" && r.sess == nil && r.group.Repo == selRepo {
			m.cursor = i
			break
		}
	}
	if m.cursor < 0 {
		m.cursor = 0
		for i, r := range m.rows {
			if r.sess != nil {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

// archivedIn counts the archived sessions of a group.
func (m *model) archivedIn(g *Group) int {
	n := 0
	for _, s := range g.Sessions {
		if s.Archived {
			n++
		}
	}
	return n
}

// archivedTotal counts archived sessions in the current profile.
func (m *model) archivedTotal() int {
	n := 0
	for _, g := range m.pd().groups {
		n += m.archivedIn(g)
	}
	return n
}

func matches(s *Session, f string) bool {
	return strings.Contains(strings.ToLower(s.Title), f) ||
		strings.Contains(strings.ToLower(s.LastPrompt), f) ||
		strings.Contains(strings.ToLower(s.Branch), f) ||
		strings.Contains(strings.ToLower(s.Repo), f) ||
		strings.Contains(strings.ToLower(s.Worktree), f) ||
		strings.HasPrefix(s.ID, f)
}

func (m *model) listHeight() int {
	h := m.height - 4 - m.statusHeight() // title bar, rule, rule, status, keys
	if h < 1 {
		h = 1
	}
	return h
}

// statusHeight is the number of rows the status area takes: one normally,
// more while a text input has wrapped, but always leaving one row for the list.
func (m *model) statusHeight() int {
	n := strings.Count(m.statusView(m.viewWidth()), "\n") + 1
	return max(1, min(n, m.height-5))
}

func (m *model) viewWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

func (m *model) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) move(delta int) {
	m.cursor += delta
	m.clampCursor()
}

func (m *model) switchProfile(delta int) tea.Cmd {
	n := len(m.profiles)
	m.profile = ((m.profile+delta)%n + n) % n
	m.cursor, m.offset = 0, 0
	m.rebuildRows()
	pd := m.pd()
	if !pd.loaded && !pd.loading {
		pd.loading = true
		return m.loadCmd(m.profiles[m.profile])
	}
	return nil
}

func (m model) selected() (*Group, *Session) {
	if m.cursor >= len(m.rows) || len(m.rows) == 0 {
		return nil, nil
	}
	r := m.rows[m.cursor]
	return r.group, r.sess
}

// launch runs argv either inline (pausing the TUI) or in a new terminal.
func (m *model) launch(dir string, argv []string, newTerm bool) tea.Cmd {
	if dir == "" {
		m.status = styleErr.Render("nothing selected")
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		m.status = styleErr.Render("directory is gone: " + dir)
		return nil
	}
	if newTerm {
		if err := spawnTerminal(m.cfg, m.current(), dir, argv); err != nil {
			m.status = styleErr.Render(err.Error())
			return nil
		}
		m.status = "spawned: " + strings.Join(argv, " ") + " in " + shortPath(dir)
		return nil
	}
	cmd, tail := inlineCmd(m.current(), dir, argv)
	name := filepath.Base(argv[0])
	cmdline := strings.Join(argv, " ") + " in " + shortPath(dir)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return execDoneMsg{name: name, cmdline: cmdline, stderr: tail.LastLine(), err: err}
	})
}

// where describes where enter (alt=false) or its shifted variant
// (alt=true) opens things.
func (m model) where(alt bool) string {
	if m.cfg.NewTerminalDefault() != alt {
		return "in new term"
	}
	return "here"
}

func (m *model) openClaude(newTerm bool) tea.Cmd {
	g, s := m.selected()
	if s == nil {
		if g == nil {
			return nil
		}
		return m.startNew(m.cfg.UseWorktrees())
	}
	if s.Running {
		// Resuming again would run two claudes on one conversation; go to
		// the window it is already running in instead.
		if err := focusWindowOf(s.PID); err != nil {
			m.status = styleErr.Render(fmt.Sprintf("already running (pid %d): %v", s.PID, err))
		} else {
			m.status = "focused " + s.Title
		}
		return nil
	}
	return m.launch(s.CWD, resumeArgv(m.cfg, s), newTerm)
}

func (m *model) openShell(newTerm bool) tea.Cmd {
	g, s := m.selected()
	dir := ""
	if s != nil {
		dir = s.CWD
	} else if g != nil {
		dir = g.Repo
	}
	return m.launch(dir, shellArgv(m.cfg), newTerm)
}

// startNew opens the repo prompt for a new session, prefilled with the
// selected project.
func (m *model) startNew(worktree bool) tea.Cmd {
	repo := ""
	g, s := m.selected()
	if s != nil {
		repo = s.Repo
	} else if g != nil {
		repo = g.Repo
	}
	if repo == "" {
		home, _ := os.UserHomeDir()
		repo = home
	}
	m.launchWT = worktree
	m.input = shortPath(repo)
	m.compl = nil
	m.mode = modeRepo
	return nil
}

// confirmRepo validates the repo prompt and moves on to the name prompt.
func (m *model) confirmRepo() {
	dir := filepath.Clean(expandHome(strings.TrimSpace(m.input)))
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		m.compl = []string{styleErr.Render("not a directory: " + dir)}
		return
	}
	m.status = ""
	if m.launchWT && gitRoot(dir) == "" {
		m.launchWT = false
		m.status = styleDim.Render("not a git repository, starting without a worktree")
	}
	m.launchIn = dir
	m.input = ""
	m.compl = nil
	m.mode = modeName
}

// completePath extends a partially typed directory path, returning the new
// input and the candidates when it is still ambiguous.
func completePath(in string) (string, []string) {
	home, _ := os.UserHomeDir()
	exp := in
	if home != "" && (in == "~" || strings.HasPrefix(in, "~/")) {
		exp = home + in[1:] // keep a trailing slash, unlike filepath.Join
	}
	matches, _ := filepath.Glob(exp + "*")
	var dirs []string
	for _, mt := range matches {
		if st, err := os.Stat(mt); err == nil && st.IsDir() && !strings.HasPrefix(filepath.Base(mt), ".") {
			dirs = append(dirs, mt)
		}
	}
	if len(dirs) == 0 {
		return in, nil
	}
	out := dirs[0]
	if len(dirs) == 1 {
		out += "/"
	} else {
		for _, d := range dirs[1:] {
			for !strings.HasPrefix(d, out) {
				out = out[:len(out)-1]
			}
		}
	}
	if strings.HasPrefix(in, "~") && home != "" && strings.HasPrefix(out, home) {
		out = "~" + out[len(home):]
	}
	var names []string
	for _, d := range dirs {
		names = append(names, filepath.Base(d)+"/")
	}
	if len(dirs) == 1 {
		names = nil
	}
	return out, names
}

// confirmName stores the session name and either launches (plain session)
// or moves on to the branch prompt (worktree session).
func (m *model) confirmName(newTerm bool) tea.Cmd {
	m.launchName = strings.ReplaceAll(strings.TrimSpace(m.input), " ", "-")
	if !m.launchWT {
		m.mode = modeList
		return m.launch(m.launchIn, newSessionArgv(m.cfg, m.launchName, false), newTerm)
	}
	m.input = ""
	m.mode = modeBranch
	return nil
}

// confirmBranch launches a worktree session. Without a branch, claude creates
// the worktree itself (branch worktree-<name>); with one, the TUI creates it.
func (m *model) confirmBranch(newTerm bool) tea.Cmd {
	m.mode = modeList
	branch := strings.TrimSpace(m.input)
	name := m.launchName
	if branch == "" {
		return m.launch(m.launchIn, newSessionArgv(m.cfg, name, true), newTerm)
	}
	if name == "" {
		name = strings.ReplaceAll(branch, "/", "-")
	}
	path, err := createWorktree(m.launchIn, name, branch)
	if err != nil {
		m.status = styleErr.Render(err.Error())
		return nil
	}
	return m.launch(path, newSessionArgv(m.cfg, name, false), newTerm)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		return m, nil

	case tea.KeyboardEnhancementsMsg:
		m.kbOK = true
		return m, nil

	case loadedMsg:
		pd := m.data[msg.profile]
		pd.loading = false
		pd.loaded = true
		pd.err = msg.err
		if msg.err == nil {
			pd.groups = msg.groups
			m.archive.apply(pd.groups)
		}
		if msg.profile == m.profiles[m.profile] {
			m.rebuildRows()
		}
		return m, nil

	case rescanMsg:
		// Pick up sessions started outside the TUI or in spawned terminals.
		pd := m.pd()
		if m.mode == modeList && !pd.loading {
			pd.loading = true
			return m, tea.Batch(m.loadCmd(m.profiles[m.profile]), rescanTick())
		}
		return m, rescanTick()

	case liveMsg:
		for name, pd := range m.data {
			if pd.loaded {
				applyLive(pd.groups, loadLive(m.cfg.Profiles[name].ConfigDir))
			}
		}
		return m, liveTick()

	case execDoneMsg:
		if msg.err != nil {
			detail := msg.err.Error()
			if msg.stderr != "" {
				detail = msg.stderr
			}
			m.status = styleErr.Render(msg.name + ": " + detail)
			if lipgloss.Width(m.status)+len(msg.cmdline)+4 < m.width {
				m.status += styleDim.Render("  (" + msg.cmdline + ")")
			}
		} else {
			m.status = ""
		}
		pd := m.pd()
		pd.loading = true
		return m, m.loadCmd(m.profiles[m.profile])

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	prev := m.lastKey
	m.lastKey = key

	switch m.mode {
	case modeHelp:
		if key == "esc" || key == "q" || key == "?" {
			m.mode = modeList
		}
		return m, nil

	case modeFilter:
		switch key {
		case "esc":
			m.filter, m.input = "", ""
			m.mode = modeList
			m.rebuildRows()
		case "enter":
			m.mode = modeList
		default:
			m.input = editText(m.input, msg)
			m.filter = m.input
			m.rebuildRows()
		}
		return m, nil

	case modeRepo:
		switch key {
		case "esc":
			m.mode = modeList
			m.compl = nil
		case "enter":
			m.confirmRepo()
		case "tab":
			m.input, m.compl = completePath(m.input)
		default:
			m.input = editText(m.input, msg)
			m.compl = nil
		}
		return m, nil

	case modeName:
		switch key {
		case "esc":
			m.mode = modeList
		case "enter":
			return m, m.confirmName(m.cfg.NewTerminalDefault())
		case "shift+enter":
			return m, m.confirmName(!m.cfg.NewTerminalDefault())
		default:
			m.input = editText(m.input, msg)
		}
		return m, nil

	case modeRename:
		switch key {
		case "esc":
			m.mode = modeList
		case "enter":
			m.mode = modeList
			_, s := m.selected()
			title := strings.TrimSpace(m.input)
			if s == nil || title == "" || title == s.Title {
				return m, nil
			}
			if err := renameSession(s, title); err != nil {
				m.status = styleErr.Render("rename: " + err.Error())
				return m, nil
			}
			s.Title = title
			m.status = "renamed to " + title
		default:
			m.input = editText(m.input, msg)
		}
		return m, nil

	case modeBranch:
		switch key {
		case "esc":
			m.mode = modeList
		case "enter":
			return m, m.confirmBranch(m.cfg.NewTerminalDefault())
		case "shift+enter":
			return m, m.confirmBranch(!m.cfg.NewTerminalDefault())
		default:
			m.input = editText(m.input, msg)
		}
		return m, nil
	}

	// modeList
	m.status = ""
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "ctrl+d":
		m.move(m.listHeight() / 2)
	case "ctrl+u":
		m.move(-m.listHeight() / 2)
	case "G", "shift+g", "end":
		m.cursor = len(m.rows) - 1
		m.clampCursor()
	case "g", "home":
		if key == "home" || prev == "g" {
			m.cursor = 0
			m.clampCursor()
			m.lastKey = ""
		}
	case "}", "]":
		m.jumpGroup(1)
	case "{", "[":
		m.jumpGroup(-1)
	case "h", "left", "shift+tab":
		return m, m.switchProfile(-1)
	case "l", "right", "tab":
		return m, m.switchProfile(1)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := int(key[0] - '1')
		if i < len(m.profiles) {
			return m, m.switchProfile(i - m.profile)
		}
	case "enter", "o":
		return m, m.openClaude(m.cfg.NewTerminalDefault())
	case "shift+enter", "alt+enter", "O", "shift+o":
		return m, m.openClaude(!m.cfg.NewTerminalDefault())
	case "ctrl+enter", "t":
		return m, m.openShell(m.cfg.NewTerminalDefault())
	case "ctrl+shift+enter", "ctrl+alt+enter", "T", "shift+t":
		return m, m.openShell(!m.cfg.NewTerminalDefault())
	case "a":
		if _, s := m.selected(); s != nil {
			s.Archived = m.archive.toggle(s.ID)
			if err := m.archive.save(); err != nil {
				m.status = styleErr.Render("archive: " + err.Error())
			} else if s.Archived {
				m.status = "archived " + s.Title + styleDim.Render("  (A shows archived)")
			} else {
				m.status = "unarchived " + s.Title
			}
			m.rebuildRows()
		}
	case "A", "shift+a":
		m.showArchived = !m.showArchived
		m.rebuildRows()
	case "R", "shift+r":
		if _, s := m.selected(); s != nil {
			m.input = s.Title
			m.mode = modeRename
		}
	case "n":
		return m, m.startNew(m.cfg.UseWorktrees())
	case "N", "shift+n":
		return m, m.startNew(!m.cfg.UseWorktrees())
	case "/":
		m.mode = modeFilter
		m.input = m.filter
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.rebuildRows()
		}
	case "r":
		pd := m.pd()
		if !pd.loading {
			pd.loading = true
			return m, m.loadCmd(m.profiles[m.profile])
		}
	}
	return m, nil
}

func (m *model) jumpGroup(dir int) {
	i := m.cursor + dir
	for i >= 0 && i < len(m.rows) {
		if m.rows[i].sess == nil {
			m.cursor = i
			m.clampCursor()
			return
		}
		i += dir
	}
}

// editText applies a key press to a single-line text input.
func editText(s string, msg tea.KeyPressMsg) string {
	switch msg.String() {
	case "backspace":
		if len(s) > 0 {
			r := []rune(s)
			return string(r[:len(r)-1])
		}
		return s
	case "ctrl+u":
		return ""
	case "ctrl+w":
		s = strings.TrimRight(s, " ")
		if i := strings.LastIndex(s, " "); i >= 0 {
			return s[:i+1]
		}
		return ""
	}
	if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		return s + msg.Text
	}
	return s
}

// ---------------------------------------------------------------- rendering

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+"/") {
			return "~" + p[len(home):]
		}
	}
	return p
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("Jan 02")
	}
}

// truncate cuts a (possibly styled) string to w cells.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func pad(s string, w int) string {
	n := lipgloss.Width(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func (m model) View() tea.View {
	var b strings.Builder
	w := m.viewWidth()
	// A wrapped input line shrinks the list; keep the cursor row in view.
	m.clampCursor()

	// Title bar with profile tabs.
	var tabs []string
	for i, p := range m.profiles {
		label := fmt.Sprintf(" %d:%s ", i+1, p)
		if i == m.profile {
			tabs = append(tabs, styleTabOn.Render(label))
		} else {
			tabs = append(tabs, styleTab.Render(label))
		}
	}
	bar := styleTitle.Render(" reserva ") + " " + strings.Join(tabs, " ")
	if n := m.archivedTotal(); n > 0 {
		hint := fmt.Sprintf("%d archived hidden (A shows)", n)
		if m.showArchived {
			hint = fmt.Sprintf("showing %d archived (A hides)", n)
		}
		bar += "  " + styleDim.Render(hint)
	}
	b.WriteString(truncate(bar, w) + "\n")
	b.WriteString(styleRule.Render(strings.Repeat("─", w)) + "\n")

	switch m.mode {
	case modeHelp:
		b.WriteString(m.helpView())
	default:
		b.WriteString(m.listView(w))
	}

	b.WriteString(styleRule.Render(strings.Repeat("─", w)) + "\n")
	status := strings.Split(m.statusView(w), "\n")
	status = status[len(status)-m.statusHeight():] // keep the end, where the cursor is
	b.WriteString(strings.Join(status, "\n") + "\n")
	b.WriteString(m.keysView(w))

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = "reserva"
	return v
}

func (m model) listView(w int) string {
	var b strings.Builder
	h := m.listHeight()
	pd := m.pd()
	if pd.loading && !pd.loaded {
		b.WriteString(styleDim.Render("  loading sessions from " + shortPath(m.current().ConfigDir) + "…"))
		b.WriteString(strings.Repeat("\n", h))
		return b.String()
	}
	if pd.err != nil {
		b.WriteString(styleErr.Render("  " + pd.err.Error()))
		b.WriteString(strings.Repeat("\n", h))
		return b.String()
	}
	if len(m.rows) == 0 {
		msg := "  no sessions in " + shortPath(m.current().ConfigDir)
		if m.filter != "" {
			msg = "  no sessions match " + m.filter
		}
		b.WriteString(styleDim.Render(msg))
		b.WriteString(strings.Repeat("\n", h))
		return b.String()
	}
	timeW := 7
	branchW := 18
	if w < 70 {
		branchW = 10
	}
	titleW := w - 4 - branchW - timeW - 3
	if titleW < 10 {
		titleW = 10
	}
	end := m.offset + h
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.offset; i < end; i++ {
		r := m.rows[i]
		on := i == m.cursor
		if r.sess == nil {
			n := len(r.group.Sessions)
			if !m.showArchived {
				n -= m.archivedIn(r.group)
			}
			label := fmt.Sprintf(" %s", shortPath(r.group.Repo))
			count := fmt.Sprintf(" %d", n)
			if n == 0 {
				count = " no sessions yet"
			}
			line := pad(truncate(label, w-lipgloss.Width(count)-1)+styleDim.Render(count), w)
			if on {
				line = styleHeaderOn.Render(pad(truncate(label, w-len(count)-1)+count, w))
			} else {
				line = styleHeader.Render(truncate(label, w-len(count)-1)) + styleDim.Render(count)
			}
			b.WriteString(line + "\n")
			continue
		}
		s := r.sess
		mark := " "
		switch {
		case s.Running && s.Status == "busy":
			mark = styleBusy.Render("●")
		case s.Running:
			mark = styleIdle.Render("●")
		case s.Missing:
			mark = styleErr.Render("✗")
		}
		if on {
			mark = lipgloss.NewStyle().Reverse(true).Render(stripStyle(mark))
		}
		branch := s.Branch
		if s.Worktree != "" {
			branch = "⎇ " + s.Worktree
		}
		titleText := s.Title
		if titleText == "" {
			titleText = "(new session, no prompt yet)"
		}
		if s.Archived {
			titleText += "  [archived]"
		}
		title := pad(truncate(titleText, titleW), titleW)
		branch = pad(truncate(branch, branchW), branchW)
		when := fmt.Sprintf("%*s", timeW, relTime(s.Modified))
		body := fmt.Sprintf("%s  %s %s", title, branch, when)
		if on {
			b.WriteString("  " + mark + " " + styleRowOn.Render(pad(body, w-4)) + "\n")
		} else if s.Missing || s.Archived {
			b.WriteString("  " + mark + " " + styleDim.Render(body) + "\n")
		} else {
			b.WriteString("  " + mark + " " + styleRow.Render(title) + "  " + styleDim.Render(branch+" "+when) + "\n")
		}
	}
	for i := end - m.offset; i < h; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func stripStyle(s string) string {
	switch {
	case strings.Contains(s, "●"):
		return "●"
	case strings.Contains(s, "✗"):
		return "✗"
	}
	return " "
}

// inputLine renders a prompt followed by a single-line text input and cursor,
// wrapped onto as many lines of w cells as it needs so all of it stays readable.
func inputLine(prompt, input string, w int) string {
	return ansi.Wrap(prompt+input+"▏", w, "-/")
}

func (m model) statusView(w int) string {
	switch m.mode {
	case modeFilter:
		return inputLine(" /", m.input, w)
	case modeRepo:
		kind := "new session"
		if m.launchWT {
			kind = "new worktree session"
		}
		return inputLine(fmt.Sprintf(" %s — repository: ", kind), m.input, w)
	case modeName:
		kind := "new session"
		if m.launchWT {
			kind = "new worktree session"
		}
		return inputLine(fmt.Sprintf(" %s in %s — name (optional): ", kind, shortPath(m.launchIn)), m.input, w)
	case modeRename:
		return inputLine(" rename session: ", m.input, w)
	case modeBranch:
		def := "worktree-<generated>"
		if m.launchName != "" {
			def = "worktree-" + m.launchName
		}
		return inputLine(fmt.Sprintf(" worktree %q in %s — branch (empty = %s): ", m.launchName, shortPath(m.launchIn), def), m.input, w)
	}
	if m.status != "" {
		return " " + truncate(m.status, w-2)
	}
	_, s := m.selected()
	if s != nil {
		detail := shortPath(s.CWD)
		if s.LastPrompt != "" && s.LastPrompt != s.Title {
			detail += "  " + styleDim.Render("› "+s.LastPrompt)
		}
		if s.Running {
			detail = fmt.Sprintf("%s (pid %d) ", s.Status, s.PID) + detail
		}
		return " " + truncate(detail, w-2)
	}
	if g, _ := m.selected(); g != nil {
		return " " + truncate(shortPath(g.Repo)+"  "+styleDim.Render("enter starts a new session in this repository"), w-2)
	}
	if m.filter != "" {
		return styleDim.Render(" filter: " + m.filter + "  (esc clears)")
	}
	return styleDim.Render(" " + shortPath(m.current().ConfigDir))
}

func (m model) keysView(w int) string {
	k := func(key, desc string) string { return styleKey.Render(key) + styleDim.Render(" "+desc) }
	var parts []string
	switch m.mode {
	case modeFilter:
		parts = []string{k("type", "to filter"), k("enter", "keep"), k("esc", "clear")}
	case modeRepo:
		if len(m.compl) > 0 {
			return " " + truncate(strings.Join(m.compl, "  "), w-1)
		}
		parts = []string{k("enter", "next"), k("tab", "complete path"), k("esc", "cancel")}
	case modeName:
		if m.launchWT {
			parts = []string{k("enter", "next: branch"), k("esc", "cancel")}
		} else {
			parts = []string{k("enter", "start "+m.where(false)), k("shift+enter", "start "+m.where(true)), k("esc", "cancel")}
		}
	case modeBranch:
		parts = []string{k("enter", "start "+m.where(false)), k("shift+enter", "start "+m.where(true)), k("esc", "cancel")}
	case modeRename:
		parts = []string{k("enter", "rename"), k("ctrl+u", "clear"), k("esc", "cancel")}
	case modeHelp:
		parts = []string{k("esc", "back")}
	default:
		alt := m.where(true)
		if m.kbOK {
			parts = []string{k("⏎", "claude"), k("⇧⏎", "claude "+alt), k("^⏎", "shell"), k("^⇧⏎", "shell "+alt)}
		} else {
			parts = []string{k("o", "claude"), k("O", "claude "+alt), k("t", "shell"), k("T", "shell "+alt)}
		}
		parts = append(parts, k("n", "new"), k("a", "archive"), k("h/l", "profile"), k("/", "filter"), k("?", "help"), k("q", "quit"))
	}
	return " " + truncate(strings.Join(parts, "  "), w-1)
}

func (m model) helpView() string {
	lines := []string{
		"  Navigation   j/k ↑/↓ move   gg/G top/bottom   ctrl+d/u half page   ]/[ next/prev project",
		"               h/l, 1-9, tab switch profile",
		"",
		"  Sessions     enter             resume claude code " + pad(m.where(false), 22) + "(or: o)",
		"               shift+enter       resume claude code " + pad(m.where(true), 22) + "(or: O)",
		"               ctrl+enter        shell in the worktree, " + pad(m.where(false), 18) + "(or: t)",
		"               ctrl+shift+enter  shell in the worktree, " + pad(m.where(true), 18) + "(or: T)",
		"               enter on a running session focuses its window (niri)",
		"               enter on a project header starts a new session there",
		"",
		"  New          n  new session: asks for a repository (tab completes, prefilled with",
		"                  the selected project), a name, and for worktrees a branch",
		"                  (empty = worktree-<name>); N uses the opposite worktree setting",
		"                  in the last prompt: enter starts it " + m.where(false) + ", shift+enter " + m.where(true),
		"",
		"  Other        a  archive/unarchive    A  show archived    R  rename    /  filter",
		"               r  reload    ?  help    q  quit",
		"",
		"  Marks        ● busy   ● idle   ✗ directory missing   ⎇ worktree",
		"  Config       " + shortPath(m.cfg.path),
	}
	if !m.kbOK {
		lines = append(lines, "",
			"  Note         this terminal did not enable the kitty keyboard protocol, so shift+enter",
			"               and ctrl+enter look like plain enter; use the o / O / t / T fallbacks.")
	}
	h := m.listHeight()
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n") + "\n"
}
