package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/model"
)

const (
	wideAt   = 110
	minTitle = 20
)

var keyHelp = [][2]string{
	{"j k ↑ ↓", "move"},
	{"enter", "open PR"},
	{"c", "open first failing check"},
	{"i", "open linked issues"},
	{"r", "refresh now"},
	{"/", "filter; esc clears"},
	{"?", "close help"},
	{"q", "quit"},
}

type layout struct {
	wide                      bool
	listWidth, listHeight     int
	detailWidth, detailHeight int
}

func (m Model) layout() layout {
	body := max(m.height-1, 0)
	if m.width >= wideAt {
		list := m.width * 3 / 5
		return layout{wide: true, listWidth: list, listHeight: body, detailWidth: m.width - list - 3, detailHeight: body}
	}
	if body < 6 || len(m.rows) == 0 {
		return layout{listWidth: m.width, listHeight: body}
	}
	// the list takes what it needs, the detail pane the rest, at least
	// min(12, half)
	list := min(max(m.listLength(), 1), body-1-min(12, body/2))
	return layout{listWidth: m.width, listHeight: list, detailWidth: m.width, detailHeight: body - 1 - list}
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "docket"
	if m.state.Loaded {
		v.WindowTitle = fmt.Sprintf("docket (%d)", m.state.Snapshot.Count)
	}
	return v
}

func (m Model) render() string {
	switch {
	case m.width <= 0 || m.height <= 0:
		return ""
	case m.height == 1:
		return m.status()
	}
	l := m.layout()
	body := m.height - 1
	var view string
	switch {
	case m.help:
		view = m.fit(m.helpLines(), m.width, body)
	case !m.state.Loaded:
		text := "fetching…"
		if m.state.Err != nil {
			text = "no data yet"
		}
		placed := lipgloss.Place(m.width, body, lipgloss.Center, lipgloss.Center, m.st.faint.Render(text))
		view = m.fit(strings.Split(placed, "\n"), m.width, body)
	case l.wide:
		list := strings.Split(m.list(l), "\n")
		detail := strings.Split(m.detail(l.detailWidth, l.detailHeight), "\n")
		divider := m.st.faint.Render(" │ ")
		rows := make([]string, body)
		for i := range rows {
			rows[i] = list[i] + divider + detail[i]
		}
		view = strings.Join(rows, "\n")
	case l.detailHeight == 0:
		view = m.list(l)
	default:
		rule := m.st.faint.Render(strings.Repeat("─", m.width))
		view = strings.Join([]string{m.list(l), rule, m.detail(l.detailWidth, l.detailHeight)}, "\n")
	}
	return view + "\n" + m.status()
}

func (m Model) list(l layout) string {
	var lines []string
	if len(m.rows) == 0 {
		text := "nothing open involves you"
		if m.filter.Value() != "" {
			text = "no matches"
		}
		lines = []string{"  " + m.st.faint.Render(text)}
	} else {
		all := m.listLines(l.listWidth)
		lines = all[min(m.offset, len(all)):]
	}
	return m.fit(lines, l.listWidth, l.listHeight)
}

func (m Model) listLines(width int) []string {
	cols := fitColumns(m.method, width, m.rows, m.state.Snapshot)
	var lines []string
	i := 0
	for _, g := range m.groups {
		lines = append(lines, m.st.header.Render(fmt.Sprintf("  %s (%d)", g.Name, len(g.Rows))))
		for _, r := range g.Rows {
			lines = append(lines, m.rowLine(r, cols, i == m.cursor))
			i++
		}
	}
	return lines
}

type columns struct {
	ci, age, ref, title, review, author, tags int // 0 hides a column
}

// fitColumns drops author, review, age, ref and ci in that order until the
// title gets minTitle, then squeezes tags.
func fitColumns(method ansi.Method, width int, rows []model.Row, snap model.Snapshot) columns {
	c := columns{ci: 1, age: 4, review: 8}
	for _, r := range rows {
		c.ref = max(c.ref, method.StringWidth(r.PR.Ref()))
		if !snap.IsMe(r.PR.Author) {
			c.author = max(c.author, method.StringWidth(r.PR.Author.Login))
		}
		c.tags = max(c.tags, method.StringWidth(tagList(r.PR.Tags)))
	}
	c.ref, c.author, c.tags = min(c.ref, 30), min(c.author, 14), min(c.tags, 24)
	for _, drop := range []*int{&c.author, &c.review, &c.age, &c.ref, &c.ci} {
		if c.fixed()+minTitle <= width {
			break
		}
		*drop = 0
	}
	if over := c.fixed() + minTitle - width; over > 0 && c.tags > 0 {
		c.tags = max(c.tags-over, 0)
	}
	c.title = max(width-c.fixed(), 0)
	return c
}

// fixed is the two-column margin plus each shown column and its gap.
func (c columns) fixed() int {
	n := 2
	for _, w := range []int{c.ci, c.age, c.ref, c.review, c.author, c.tags} {
		if w > 0 {
			n += w + 1
		}
	}
	return n
}

func (m Model) rowLine(r model.Row, c columns, selected bool) string {
	paint := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(m.st.selected)
		}
		return s
	}
	gap := paint(lipgloss.NewStyle()).Render(" ")
	snap := m.state.Snapshot
	marker := "  "
	if selected {
		marker = "▌ "
	}
	var b strings.Builder
	b.WriteString(paint(m.st.accent).Render(marker))
	lead := func(text string, width int, s lipgloss.Style) {
		if width > 0 {
			b.WriteString(paint(s).Render(text) + gap)
		}
	}
	lead(ciGlyph[r.CI], c.ci, m.st.ci[r.CI])
	lead(m.padLeft(snap.Since(r.Activity), c.age), c.age, m.st.faint)
	lead(m.pad(r.PR.Ref(), c.ref), c.ref, m.st.faint)
	b.WriteString(paint(linked(lipgloss.NewStyle(), r.PR.URL)).Render(m.pad(r.PR.Title, c.title)))
	trail := func(text string, width int, s lipgloss.Style) {
		if width > 0 {
			b.WriteString(gap + paint(s).Render(m.pad(text, width)))
		}
	}
	trail(r.Review, c.review, m.st.review[r.Review])
	author := r.PR.Author.Login
	if snap.IsMe(r.PR.Author) {
		author = ""
	}
	trail(author, c.author, m.st.faint)
	trail(tagList(r.PR.Tags), c.tags, m.st.accent)
	return b.String()
}

func (m Model) detail(w, h int) string {
	r := m.current()
	if r == nil {
		return m.fit(nil, w, h)
	}
	pr := r.PR
	lines := []string{linked(m.st.bold, pr.URL).Render(pr.Ref())}
	lines = append(lines, strings.Split(m.method.Wordwrap(pr.Title, w, ""), "\n")...)
	lines = append(lines, m.st.faint.Render(m.state.Snapshot.Meta(*r)), m.st.accent.Render(strings.Join(r.Labels(), ", ")))
	block := func(title string, body []string) {
		if len(body) > 0 {
			lines = append(append(lines, "", m.st.header.Render(title)), body...)
		}
	}
	block("What's left", m.facts(r.Left))
	block("You", m.facts(r.Mine))
	var checks, reviewers, issues []string
	for _, c := range pr.Checks.Failing {
		checks = append(checks, "  "+m.st.kinds[model.Bad].Render(glyph[model.Bad])+" "+linked(lipgloss.NewStyle(), c.URL).Render(c.Name))
	}
	for _, rv := range model.Reviewers(pr) {
		reviewers = append(reviewers, "  "+rv.Name+" "+m.st.faint.Render(rv.State))
	}
	for _, is := range pr.Issues {
		ref := linked(lipgloss.NewStyle(), is.URL).Render(fmt.Sprintf("%s#%d", is.Repo, is.Number))
		issues = append(issues, "  "+ref+" "+is.Title+" "+m.st.faint.Render(strings.ToLower(is.State)))
	}
	block("Failing checks", checks)
	block("Reviewers", reviewers)
	block("Linked issues", issues)
	return m.fit(lines, w, h)
}

func (m Model) facts(lines []model.Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + m.st.kinds[l.Kind].Render(glyph[l.Kind]) + " " + linked(lipgloss.NewStyle(), l.URL).Render(l.Text)
	}
	return out
}

// linked makes s a hyperlink, for web links only: check urls come from
// third-party apps, and a terminal will happily open file:// ones.
func linked(s lipgloss.Style, url string) lipgloss.Style {
	if browser.Valid(url) {
		return s.Hyperlink(url)
	}
	return s
}

func (m Model) status() string {
	if m.filtering {
		return m.fit([]string{m.filter.View()}, m.width, 1)
	}
	left := m.statusText()
	right := m.st.faint.Render(m.budgetText())
	room := m.width - m.method.StringWidth(left) - m.method.StringWidth(right)
	if room < 1 {
		return m.method.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", room) + right
}

func (m Model) statusText() string {
	if m.flash != "" {
		return m.flash + " · " + m.stateText()
	}
	return m.stateText()
}

func (m Model) stateText() string {
	s := m.state
	bad := m.st.kinds[model.Bad]
	switch {
	case s.Err != nil && s.Loaded:
		return bad.Render(fmt.Sprintf("updated %s · retry %s · fetch failed: %v", m.clock(s.Updated), m.clock(s.Next), s.Err))
	case s.Err != nil:
		return bad.Render(fmt.Sprintf("retry %s · fetch failed: %v", m.clock(s.Next), s.Err))
	case !s.Loaded:
		return "fetching…"
	}
	text := fmt.Sprintf("%d PRs · updated %s · next %s", s.Snapshot.Count, m.clock(s.Updated), m.clock(s.Next))
	if q := m.filter.Value(); q != "" {
		text += fmt.Sprintf(" · filter %q: %d", q, len(m.rows))
	}
	if n := len(s.Warnings); n > 0 {
		text += fmt.Sprintf(" · %d %s, see ?", n, model.Plural(n, "warning", "warnings"))
	}
	return text
}

func (m Model) budgetText() string {
	text := "? help"
	if b := m.state.Budget; b.Limit > 0 {
		text = fmt.Sprintf("budget %d/%d · %s", b.Remaining, b.Limit, text)
	}
	return text
}

func (m Model) clock(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(m.loc).Format("15:04")
}

func (m Model) helpLines() []string {
	lines := []string{m.st.header.Render("  Keys")}
	for _, k := range keyHelp {
		lines = append(lines, fmt.Sprintf("  %-9s %s", k[0], k[1]))
	}
	if len(m.state.Warnings) > 0 {
		lines = append(lines, "", m.st.header.Render("  Warnings"))
		for _, w := range m.state.Warnings {
			lines = append(lines, "  "+w)
		}
	}
	return lines
}

// fit pads or clips lines to exactly w columns by h rows.
func (m Model) fit(lines []string, w, h int) string {
	out := make([]string, h)
	for i := range out {
		var line string
		if i < len(lines) {
			line = m.method.Truncate(lines[i], w, "")
		}
		out[i] = line + strings.Repeat(" ", max(w-m.method.StringWidth(line), 0))
	}
	return strings.Join(out, "\n")
}

func (m Model) pad(s string, w int) string {
	s = m.method.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-m.method.StringWidth(s), 0))
}

func (m Model) padLeft(s string, w int) string {
	s = m.method.Truncate(s, w, "…")
	return strings.Repeat(" ", max(w-m.method.StringWidth(s), 0)) + s
}

func tagList(tags []model.Tag) string {
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = string(t)
	}
	return strings.Join(names, ",")
}
