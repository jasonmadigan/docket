package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/config"
	"github.com/jasonmadigan/docket/internal/model"
)

const (
	wideAt   = 110
	minTitle = 20
)

var keyHelp = [][2]string{
	{"j k ↑ ↓", "move"},
	{"tab", "next tab; shift+tab goes back"},
	{"enter", "open PR or issue"},
	{"c", "open first failing check"},
	{"i", "open linked issues"},
	{"p", "open linked PRs"},
	{"a", "archive; on Archived, unarchive"},
	{"u", "undo the last archive"},
	{"r", "refresh now"},
	{"s", "settings"},
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
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "docket"
	if s := m.state.Snapshot; m.state.Loaded {
		v.WindowTitle = fmt.Sprintf("docket (%d)", s.PRs.Count+s.Issues.Count)
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
	case m.editing:
		return m.settingsPanel(h)
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

// headerParts lays out the top bar: the fullest left side that leaves room
// for the right, and where each tab name sits when the tabs show.
func (m Model) headerParts() (left, right string, tabs []zone) {
	bar := func(s lipgloss.Style) lipgloss.Style { return s.Background(m.st.bar) }
	plain := bar(lipgloss.NewStyle())
	s := m.state
	if s.Loaded || s.Err != nil {
		right = plain.Render(m.activity(bar) + " ")
	}
	if m.settings != nil {
		right += plain.Render(" ") + bar(m.st.link).Render("settings") + plain.Render(" ")
	}
	brand := m.st.badge.Render(" docket ")
	type option struct {
		text string
		tabs bool
	}
	options := []option{{brand, false}}
	var zones []zone
	if s.Loaded {
		var who, dot string
		if login := s.Snapshot.Login; login != "" {
			who, dot = plain.Render(" ")+bar(m.st.bold).Render(login), plain.Render(" ·")
		}
		var names strings.Builder
		x := m.method.StringWidth(brand + who + dot)
		for t := range tabCount {
			if t > 0 {
				names.WriteString(bar(m.st.faint).Render(" │"))
				x += 2
			}
			label := fmt.Sprintf("%s %d", tabNames[t], m.listOf(t).Count)
			style := m.st.faint
			if t == m.tab {
				style = m.st.header
			}
			names.WriteString(plain.Render(" ") + bar(style).Render(label))
			zones = append(zones, zone{x: x + 1, y: 0, w: m.method.StringWidth(label)})
			x += 1 + m.method.StringWidth(label)
		}
		var counts []string
		for _, sec := range m.listOf(m.tab).Sections {
			counts = append(counts, bar(m.st.faint).Render(sec.Name+" ")+bar(m.st.bold).Render(fmt.Sprint(len(sec.Rows))))
		}
		tabbed := brand + who + dot + names.String()
		options = []option{
			{tabbed + plain.Render("   ") + strings.Join(counts, plain.Render("  ")), true},
			{tabbed, true},
			{brand + who, false},
			{brand, false},
		}
	}
	for _, o := range options {
		if m.width-m.method.StringWidth(o.text)-m.method.StringWidth(right) >= 1 {
			if o.tabs {
				return o.text, right, zones
			}
			return o.text, right, nil
		}
	}
	return "", right, nil
}

// header is the top bar: who, the tabs, the sections of the tab showing,
// and what the engine is doing.
func (m Model) header() string {
	left, right, _ := m.headerParts()
	if left == "" {
		return m.method.Truncate(m.st.badge.Render(" docket ")+right, m.width, "")
	}
	room := m.width - m.method.StringWidth(left) - m.method.StringWidth(right)
	return left + lipgloss.NewStyle().Background(m.st.bar).Render(strings.Repeat(" ", room)) + right
}

// tabZones is where the header draws each tab name, when it draws them.
func (m Model) tabZones() []zone {
	_, _, zones := m.headerParts()
	return zones
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
		left = m.st.faint.Render(m.hints())
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

// hints are the keys worth knowing on the tab showing.
func (m Model) hints() string {
	var parts []string
	switch m.tab {
	case tabIssues:
		parts = []string{"tab archived", "j/k move", "enter open", "p PRs"}
	case tabArchived:
		parts = []string{"tab PRs", "j/k move", "enter open"}
	default:
		parts = []string{"tab issues", "j/k move", "enter open", "c check", "i issues"}
	}
	if m.archive != nil {
		if m.tab == tabArchived {
			parts = append(parts, "a unarchive")
		} else {
			parts = append(parts, "a archive")
		}
	}
	parts = append(parts, "/ filter", "r refresh")
	if m.settings != nil {
		parts = append(parts, "s settings")
	}
	return strings.Join(append(parts, "? help"), " · ")
}

var empty = [tabCount]string{"no open PRs involve you", "no open issues involve you", "nothing archived"}

func (m Model) list(l layout) string {
	var lines []string
	if len(m.rows) == 0 {
		text := empty[m.tab]
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
// title gets minTitle, then squeezes tags. review shows only beside prs.
func fitColumns(method ansi.Method, width int, rows []model.Row, snap model.Snapshot) columns {
	c := columns{ci: 1, age: 4}
	for _, r := range rows {
		it := r.Item()
		c.ref = max(c.ref, method.StringWidth(it.Ref()))
		if !snap.IsMe(it.Author) {
			c.author = max(c.author, method.StringWidth(it.Author.Login))
		}
		c.tags = max(c.tags, method.StringWidth(tagList(it.Tags)))
		if r.PR != nil {
			c.review = 8
		}
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
	it := r.Item()
	changed := m.changed[it.ID]
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
	g, gs := m.glyph(r)
	lead(g, c.ci, gs)
	lead(m.padLeft(snap.Since(r.Activity), c.age), c.age, m.st.faint)
	lead(m.pad(it.Ref(), c.ref), c.ref, linked(m.st.faint, it.URL))
	title := linked(lipgloss.NewStyle(), it.URL)
	if changed {
		title = title.Bold(true)
	}
	b.WriteString(paint(title).Render(m.pad(it.Title, c.title)))
	trail := func(text string, width int, s lipgloss.Style) {
		if width > 0 {
			b.WriteString(gap + paint(s).Render(m.pad(text, width)))
		}
	}
	trail(r.Review, c.review, m.st.review[r.Review])
	author := it.Author.Login
	if snap.IsMe(it.Author) {
		author = ""
	}
	trail(author, c.author, m.st.faint)
	trail(tagList(it.Tags), c.tags, m.st.accent)
	return b.String()
}

// glyph is ci for a pr, and for an issue the state of its linked prs.
func (m Model) glyph(r model.Row) (string, lipgloss.Style) {
	if r.Issue != nil {
		return fixGlyph[r.Fix], m.st.fix[r.Fix]
	}
	return ciGlyph[r.CI], m.st.ci[r.CI]
}

func (m Model) detail(w, h int) string {
	r := m.current()
	if r == nil || w < 8 || h < 3 {
		return m.fit(nil, w, h)
	}
	it := r.Item()
	inner := w - 4
	var lines []string
	for _, l := range strings.Split(m.method.Wordwrap(it.Title, inner, ""), "\n") {
		lines = append(lines, m.st.bold.Render(l))
	}
	lines = append(lines,
		linked(m.st.link, it.URL).Render(strings.TrimPrefix(it.URL, "https://")),
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
	if pr := r.PR; pr != nil {
		var checks, reviewers, issues []string
		for _, c := range pr.Checks.Failing {
			checks = append(checks, "  "+m.st.kinds[model.Bad].Render(glyph[model.Bad])+" "+linked(m.st.link, c.URL).Render(c.Name))
		}
		for _, rv := range model.Reviewers(*pr) {
			reviewers = append(reviewers, "  "+rv.Name+" "+m.st.faint.Render(rv.State))
		}
		for _, is := range pr.Issues {
			ref := linked(m.st.link, is.URL).Render(fmt.Sprintf("%s#%d", is.Repo, is.Number))
			issues = append(issues, "  "+ref+" "+is.Title+" "+m.st.faint.Render(strings.ToLower(is.State)))
		}
		block("Failing checks", checks)
		block("Reviewers", reviewers)
		block("Linked issues", issues)
	}
	if is := r.Issue; is != nil {
		var assignees, labels, prs []string
		for _, a := range is.Assignees {
			assignees = append(assignees, "  "+a.Login)
		}
		if len(is.Labels) > 0 {
			labels = []string{"  " + strings.Join(is.Labels, ", ")}
		}
		for _, pr := range is.PRs {
			ref := linked(m.st.link, pr.URL).Render(pr.Ref())
			prs = append(prs, "  "+ref+" "+pr.Title+" "+m.st.faint.Render(pr.Status()))
		}
		block("Assignees", assignees)
		block("Labels", labels)
		block("Linked PRs", prs)
	}
	title := linked(m.st.header, it.URL).Render(it.Ref())
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

type zone struct{ x, y, w int }

func (z zone) hit(x, y int) bool { return y == z.y && x >= z.x && x < z.x+z.w }

// headerButton is where the header draws its settings link.
func (m Model) headerButton() zone {
	if m.settings == nil {
		return zone{}
	}
	return zone{x: m.width - len("settings") - 1, y: 0, w: len("settings")}
}

const labelW = 18

type form struct {
	x, y, w, h                      int // the box on screen
	prev, next, field, save, cancel zone
}

// form lays out the settings box. settingsPanel draws from the same
// numbers, so clicks land on what they look like they land on.
func (m Model) form() form {
	f := form{w: max(min(72, m.width-2), 0), h: 13}
	f.x = max((m.width-f.w)/2, 0)
	f.y = 1 + max((m.bodyHeight()-f.h)/2, 0)
	left, line := f.x+2, func(i int) int { return f.y + 1 + i }
	value := config.FormatPoll(m.draftPoll)
	f.prev = zone{left + labelW, line(1), 1}
	f.next = zone{left + labelW + 2 + len(value) + 1, line(1), 1}
	f.field = zone{left + labelW, line(3), max(f.w-4-labelW, 1)}
	f.save = zone{left, line(6), len("[ Save ]")}
	f.cancel = zone{left + len("[ Save ]  "), line(6), len("[ Cancel ]")}
	return f
}

func (m Model) settingsPanel(h int) string {
	f := m.form()
	focused := func(i int, s string) string {
		if m.focus == i {
			return m.st.section.Render(s)
		}
		return s
	}
	label := func(s string) string { return m.st.bold.Render(s) + strings.Repeat(" ", labelW-len(s)) }
	field := m.ignore
	field.SetWidth(max(f.field.w-1, 1))
	lines := []string{
		"",
		label("Refresh every") + m.st.accent.Render("‹") + " " + focused(0, config.FormatPoll(m.draftPoll)) + " " + m.st.accent.Render("›"),
		"",
		label("Ignore accounts") + field.View(),
		strings.Repeat(" ", labelW) + m.st.faint.Render("bots and CI users, comma separated"),
		"",
		focused(2, "[ Save ]") + "  " + focused(3, "[ Cancel ]"),
		"",
		m.st.faint.Render("saved to " + m.settings.Path()),
		m.st.kinds[model.Bad].Render(m.formErr),
		m.st.faint.Render("tab next · ← → change · enter save · esc cancel"),
	}
	box := strings.Split(m.box(m.st.header.Render("Settings"), lines, f.w, f.h), "\n")
	out := make([]string, h)
	for i := range out {
		if j := 1 + i - f.y; j >= 0 && j < len(box) {
			out[i] = strings.Repeat(" ", f.x) + box[j]
		}
	}
	return m.fit(out, m.width, h)
}
