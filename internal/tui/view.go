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

const hints = "j/k move · enter open · c check · i issues · / filter · r refresh · ? help"

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

// bodyHeight is what's left between the header bar and the footer.
func (m Model) bodyHeight() int {
	return max(m.height-2, 0)
}

func (m Model) layout() layout {
	body := m.bodyHeight()
	if m.width >= wideAt {
		list := m.width * 3 / 5
		return layout{wide: true, listWidth: list, listHeight: body, detailWidth: m.width - list - 1, detailHeight: body}
	}
	if body < 12 || len(m.rows) == 0 {
		return layout{listWidth: m.width, listHeight: body}
	}
	// the list takes what it needs, the detail pane the rest, at least
	// min(14, half)
	list := min(max(m.listLength(), 1), body-min(14, body/2))
	return layout{listWidth: m.width, listHeight: list, detailWidth: m.width, detailHeight: body - list}
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
		return m.footer()
	}
	lines := []string{m.header()}
	if h := m.bodyHeight(); h > 0 {
		lines = append(lines, m.body(h))
	}
	return strings.Join(append(lines, m.footer()), "\n")
}

func (m Model) body(h int) string {
	l := m.layout()
	switch {
	case m.help:
		return m.fit(m.helpLines(), m.width, h)
	case !m.state.Loaded:
		text := m.st.faint.Render("no data yet")
		if m.working() {
			text = m.st.accent.Render(m.spin.View()) + " pulling data · " + phase(m.state.Progress.Phase, m.state.Progress.Done, m.state.Progress.Total)
		}
		return m.centred(text, h)
	case l.wide:
		list := strings.Split(m.list(l), "\n")
		detail := strings.Split(m.detail(l.detailWidth, l.detailHeight), "\n")
		rows := make([]string, h)
		for i := range rows {
			rows[i] = list[i] + " " + detail[i]
		}
		return strings.Join(rows, "\n")
	case l.detailHeight == 0:
		return m.list(l)
	}
	return m.list(l) + "\n" + m.detail(l.detailWidth, l.detailHeight)
}

func phase(name string, done, total int) string {
	switch {
	case name == "":
		return "starting"
	case total > 0:
		return fmt.Sprintf("%s %d/%d", name, done, total)
	}
	return name
}

func (m Model) centred(text string, h int) string {
	lines := make([]string, h)
	pad := max((m.width-m.method.StringWidth(text))/2, 0)
	lines[h/2] = strings.Repeat(" ", pad) + text
	return m.fit(lines, m.width, h)
}

// header is the top bar: who, how many, and what the engine is doing.
func (m Model) header() string {
	bar := func(s lipgloss.Style) lipgloss.Style { return s.Background(m.st.bar) }
	plain := bar(lipgloss.NewStyle())
	s := m.state
	brand := m.st.badge.Render(" docket ")
	var who, counts string
	if s.Loaded {
		who = plain.Render(fmt.Sprintf(" %d open", s.Snapshot.Count))
		if login := s.Snapshot.Login; login != "" {
			who = plain.Render(" ") + bar(m.st.bold).Render(login) + plain.Render(" ·") + who
		}
		var parts []string
		for _, sec := range s.Snapshot.Sections {
			parts = append(parts, bar(m.st.faint).Render(sec.Name+" ")+bar(m.st.bold).Render(fmt.Sprint(len(sec.Rows))))
		}
		counts = plain.Render("   ") + strings.Join(parts, plain.Render("  "))
	}
	var right string
	if s.Loaded || s.Err != nil {
		right = plain.Render(m.activity(bar) + " ")
	}
	for _, left := range []string{brand + who + counts, brand + who, brand} {
		if room := m.width - m.method.StringWidth(left) - m.method.StringWidth(right); room >= 1 {
			return left + plain.Render(strings.Repeat(" ", room)) + right
		}
	}
	return m.method.Truncate(brand+right, m.width, "")
}

func (m Model) activity(bar func(lipgloss.Style) lipgloss.Style) string {
	s := m.state
	bad := bar(m.st.kinds[model.Bad])
	switch {
	case m.working():
		p := s.Progress
		return bar(m.st.accent).Render(m.spin.View()) + bar(lipgloss.NewStyle()).Render(" pulling data · "+phase(p.Phase, p.Done, p.Total))
	case s.Err != nil && s.Loaded:
		return bad.Render(fmt.Sprintf("✗ updated %s · retry %s", m.clock(s.Updated), m.clock(s.Next)))
	case s.Err != nil:
		return bad.Render("✗ retry " + m.clock(s.Next))
	}
	return bar(m.st.faint).Render(fmt.Sprintf("updated %s · next %s", m.clock(s.Updated), m.clock(s.Next)))
}

// footer carries messages and errors, else key hints, with the budget on
// the right; the budget goes before any words get cut.
func (m Model) footer() string {
	if m.filtering {
		return m.fit([]string{m.filter.View()}, m.width, 1)
	}
	var parts []string
	if m.flash != "" {
		parts = append(parts, m.flash)
	}
	if err := m.state.Err; err != nil {
		parts = append(parts, m.st.kinds[model.Bad].Render("fetch failed: "+err.Error()))
	}
	if q := m.filter.Value(); q != "" {
		parts = append(parts, fmt.Sprintf("filter %q: %d", q, len(m.rows)))
	}
	if n := len(m.state.Warnings); n > 0 {
		parts = append(parts, m.st.kinds[model.Wait].Render(fmt.Sprintf("%d %s, see ?", n, model.Plural(n, "warning", "warnings"))))
	}
	left := strings.Join(parts, " · ")
	if left == "" {
		left = m.st.faint.Render(hints)
	}
	var right string
	if b := m.state.Budget; b.Limit > 0 {
		right = m.st.faint.Render(fmt.Sprintf("budget %d/%d", b.Remaining, b.Limit))
	}
	room := m.width - m.method.StringWidth(left) - m.method.StringWidth(right)
	if room < 1 {
		return m.method.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", room) + right
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
		badge := m.st.section.Render(fmt.Sprintf(" %s %d ", g.Name, len(g.Rows)))
		rule := m.st.rule.Render(strings.Repeat("─", max(width-m.method.StringWidth(badge)-3, 0)))
		lines = append(lines, "  "+badge+" "+rule)
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
	changed := m.changed[r.PR.ID]
	var b strings.Builder
	switch {
	case selected:
		b.WriteString(paint(m.st.accent).Render("▌ "))
	case changed:
		b.WriteString(m.st.kinds[model.Wait].Render("◆ "))
	default:
		b.WriteString("  ")
	}
	lead := func(text string, width int, s lipgloss.Style) {
		if width > 0 {
			b.WriteString(paint(s).Render(text) + gap)
		}
	}
	lead(ciGlyph[r.CI], c.ci, m.st.ci[r.CI])
	lead(m.padLeft(snap.Since(r.Activity), c.age), c.age, m.st.faint)
	lead(m.pad(r.PR.Ref(), c.ref), c.ref, linked(m.st.faint, r.PR.URL))
	title := linked(lipgloss.NewStyle(), r.PR.URL)
	if changed {
		title = title.Bold(true)
	}
	b.WriteString(paint(title).Render(m.pad(r.PR.Title, c.title)))
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
	if r == nil || w < 8 || h < 3 {
		return m.fit(nil, w, h)
	}
	pr := r.PR
	inner := w - 4
	var lines []string
	for _, l := range strings.Split(m.method.Wordwrap(pr.Title, inner, ""), "\n") {
		lines = append(lines, m.st.bold.Render(l))
	}
	lines = append(lines,
		linked(m.st.link, pr.URL).Render(strings.TrimPrefix(pr.URL, "https://")),
		m.st.faint.Render(m.state.Snapshot.Meta(*r)),
		m.st.accent.Render(strings.Join(r.Labels(), ", ")),
	)
	block := func(title string, body []string) {
		if len(body) > 0 {
			lines = append(append(lines, "", m.st.header.Render(title)), body...)
		}
	}
	block("What's left", m.facts(r.Left))
	block("You", m.facts(r.Mine))
	var checks, reviewers, issues []string
	for _, c := range pr.Checks.Failing {
		checks = append(checks, "  "+m.st.kinds[model.Bad].Render(glyph[model.Bad])+" "+linked(m.st.link, c.URL).Render(c.Name))
	}
	for _, rv := range model.Reviewers(pr) {
		reviewers = append(reviewers, "  "+rv.Name+" "+m.st.faint.Render(rv.State))
	}
	for _, is := range pr.Issues {
		ref := linked(m.st.link, is.URL).Render(fmt.Sprintf("%s#%d", is.Repo, is.Number))
		issues = append(issues, "  "+ref+" "+is.Title+" "+m.st.faint.Render(strings.ToLower(is.State)))
	}
	block("Failing checks", checks)
	block("Reviewers", reviewers)
	block("Linked issues", issues)
	title := linked(m.st.header, pr.URL).Render(pr.Ref())
	return m.box(title, lines, w, h)
}

// box draws a rounded border titled with title, fitting lines inside.
func (m Model) box(title string, lines []string, w, h int) string {
	edge := m.st.rule
	title = m.method.Truncate(title, max(w-5, 0), "…")
	fill := max(w-m.method.StringWidth(title)-5, 0)
	out := []string{edge.Render("╭─ ") + title + edge.Render(" "+strings.Repeat("─", fill)+"╮")}
	for _, line := range strings.Split(m.fit(lines, w-4, h-2), "\n") {
		out = append(out, edge.Render("│ ")+line+edge.Render(" │"))
	}
	out = append(out, edge.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

func (m Model) facts(lines []model.Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		text := lipgloss.NewStyle()
		if l.URL != "" {
			text = m.st.link
		}
		out[i] = "  " + m.st.kinds[l.Kind].Render(glyph[l.Kind]) + " " + linked(text, l.URL).Render(l.Text)
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
