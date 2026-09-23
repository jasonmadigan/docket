package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/model"
)

type Engine interface {
	Subscribe() (<-chan engine.State, func())
	Refresh()
}

type Options struct {
	Open     func(urls ...string) tea.Cmd // defaults to the browser, or the clipboard over ssh
	Location *time.Location
}

type (
	stateMsg engine.State
	flashMsg string
)

type Model struct {
	states  <-chan engine.State
	refresh func()
	open    func(urls ...string) tea.Cmd
	loc     *time.Location

	state     engine.State
	groups    []model.Section // after filtering
	rows      []model.Row     // groups flattened, in display order
	cursor    int
	selected  string // pr id under the cursor, kept across refreshes
	offset    int    // first list line in view
	width     int
	height    int
	filter    textinput.Model
	filtering bool
	help      bool
	flash     string
	st        styles
	// measure as the renderer does: wcwidth until the terminal reports
	// unicode core (mode 2027), then graphemes
	method ansi.Method
	spin   spinner.Model
	// spinning is true while a tick is in flight, so only one chain runs
	spinning bool
	// changed holds prs new or different since a poll, until visited
	changed map[string]bool
	seen    time.Time // the poll changed was last filled from
}

func Run(ctx context.Context, eng Engine) error {
	m, unsubscribe := New(eng, Options{})
	defer unsubscribe()
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func New(eng Engine, opts Options) (Model, func()) {
	states, unsubscribe := eng.Subscribe()
	if opts.Open == nil {
		opts.Open = openLinks
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	filter := textinput.New()
	filter.Prompt = "/"
	filter.Placeholder = "repo, title, author or tag"
	return Model{
		states:  states,
		refresh: eng.Refresh,
		open:    opts.Open,
		loc:     opts.Location,
		filter:  filter,
		st:      newStyles(true),
		spin:    spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		changed: map[string]bool{},
	}, unsubscribe
}

func openLinks(urls ...string) tea.Cmd {
	urls = slices.DeleteFunc(slices.Clone(urls), func(u string) bool { return !browser.Valid(u) })
	if len(urls) == 0 {
		return flash("refusing to open a non-web link")
	}
	if browser.Remote() {
		n := len(urls)
		return tea.Batch(
			tea.SetClipboard(strings.Join(urls, "\n")),
			flash(fmt.Sprintf("copied %d %s", n, model.Plural(n, "link", "links"))),
		)
	}
	return func() tea.Msg {
		for _, u := range urls {
			if err := browser.Open(u); err != nil {
				return flashMsg(err.Error())
			}
		}
		return nil
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(wait(m.states), tea.RequestBackgroundColor)
}

func wait(states <-chan engine.State) tea.Cmd {
	return func() tea.Msg { return stateMsg(<-states) }
}

func flash(text string) tea.Cmd {
	return func() tea.Msg { return flashMsg(text) }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case stateMsg:
		m.state = engine.State(msg)
		m.flash = ""
		if s := m.state; s.Loaded && !s.Busy && !s.Updated.Equal(m.seen) {
			m.seen = s.Updated
			for _, id := range s.Changed {
				m.changed[id] = true
			}
		}
		m.rebuild()
		cmds := []tea.Cmd{wait(m.states)}
		if m.working() && !m.spinning {
			m.spinning = true
			cmds = append(cmds, m.spin.Tick)
		}
		return m, tea.Batch(cmds...)
	case spinner.TickMsg:
		if !m.working() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.ModeReportMsg:
		if msg.Mode == ansi.ModeUnicodeCore && (msg.Value == ansi.ModeReset || msg.Value == ansi.ModeSet || msg.Value == ansi.ModePermanentlySet) {
			m.method = ansi.GraphemeWidth
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.SetWidth(max(msg.Width-4, 1))
		m.scroll()
		return m, nil
	case tea.BackgroundColorMsg:
		m.st = newStyles(msg.IsDark())
		return m, nil
	case flashMsg:
		m.flash = string(msg)
		return m, nil
	case tea.KeyPressMsg:
		if m.filtering {
			return m.filterKey(msg)
		}
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	row := m.current()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "r":
		m.refresh()
		m.flash = "refreshing"
	case "?":
		m.help = !m.help
	case "/":
		m.filtering, m.help = true, false
		return m, m.filter.Focus()
	case "esc":
		m.help = false
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.rebuild()
		}
	case "enter":
		if row != nil {
			return m, m.open(row.PR.URL)
		}
	case "c":
		if row == nil {
			break
		}
		for _, c := range row.PR.Checks.Failing {
			if c.URL != "" {
				return m, m.open(c.URL)
			}
		}
		m.flash = "no failing check to open"
	case "i":
		if row == nil {
			break
		}
		if len(row.PR.Issues) == 0 {
			m.flash = "no linked issues"
			break
		}
		urls := make([]string, len(row.PR.Issues))
		for i, is := range row.PR.Issues {
			urls[i] = is.URL
		}
		return m, m.open(urls...)
	}
	return m, nil
}

func (m Model) filterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return m, nil
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.rebuild()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.rebuild()
	return m, cmd
}

// rebuild applies the filter, keeping the selected pr under the cursor
// while it survives.
func (m *Model) rebuild() {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	present := map[string]bool{}
	for _, sec := range m.state.Snapshot.Sections {
		for _, r := range sec.Rows {
			present[r.PR.ID] = true
		}
	}
	for id := range m.changed {
		if !present[id] {
			delete(m.changed, id)
		}
	}
	m.groups, m.rows = nil, nil
	for _, sec := range m.state.Snapshot.Sections {
		g := model.Section{Name: sec.Name}
		for _, r := range sec.Rows {
			if query == "" || strings.Contains(haystack(r), query) {
				g.Rows = append(g.Rows, r)
			}
		}
		if len(g.Rows) > 0 {
			m.groups = append(m.groups, g)
			m.rows = append(m.rows, g.Rows...)
		}
	}
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	for i, r := range m.rows {
		if r.PR.ID == m.selected {
			m.cursor = i
			break
		}
	}
	m.remember()
	m.scroll()
}

func haystack(r model.Row) string {
	parts := append([]string{r.PR.Ref(), r.PR.Title, r.PR.Author.Login}, r.Labels()...)
	return strings.ToLower(strings.Join(parts, " "))
}

func (m *Model) remember() {
	m.selected = ""
	if m.cursor < len(m.rows) {
		m.selected = m.rows[m.cursor].PR.ID
		delete(m.changed, m.selected)
	}
}

// working is true while a poll runs, or before the first has landed.
func (m Model) working() bool {
	return m.state.Busy || (!m.state.Loaded && m.state.Err == nil)
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
	m.remember()
	m.scroll()
}

func (m Model) current() *model.Row {
	if m.cursor < len(m.rows) {
		return &m.rows[m.cursor]
	}
	return nil
}

func (m Model) listLength() int {
	n := 0
	for _, g := range m.groups {
		n += 1 + len(g.Rows)
	}
	return n
}

func (m Model) cursorLine() int {
	line, i := 0, 0
	for _, g := range m.groups {
		line++
		for range g.Rows {
			if i == m.cursor {
				return line
			}
			line++
			i++
		}
	}
	return 0
}

// scroll keeps the cursor in view along with the line above it: its
// section header or the previous row.
func (m *Model) scroll() {
	h, total := m.layout().listHeight, m.listLength()
	if h <= 0 || total <= h {
		m.offset = 0
		return
	}
	line := m.cursorLine()
	switch {
	case line-1 < m.offset:
		m.offset = max(line-1, 0)
	case line >= m.offset+h:
		m.offset = line - h + 1
	}
	m.offset = min(m.offset, total-h)
}
