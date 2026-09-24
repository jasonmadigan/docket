package dump

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/model"
)

// basic ansi colours, so the terminal's own theme decides light or dark
var (
	bold    = lipgloss.NewStyle().Bold(true)
	faint   = lipgloss.NewStyle().Faint(true)
	accent  = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	red     = lipgloss.NewStyle().Foreground(lipgloss.Red)
	green   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	yellow  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	heading = lipgloss.NewStyle().Bold(true).Underline(true)
	ci      = map[model.CI]lipgloss.Style{model.CIPass: green, model.CIFail: red.Bold(true), model.CIRunning: yellow, model.CINone: faint}
	fix     = map[model.Fix]lipgloss.Style{model.FixMerged: green, model.FixOpen: yellow, model.FixNone: faint}
	kinds   = map[model.LineKind]lipgloss.Style{model.Bad: red, model.Wait: yellow, model.Good: green, model.Info: faint}
)

func JSON(w io.Writer, s model.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

var indent = strings.Repeat(" ", 16)

// Text prints pull requests, then issues: a line per item, its tags and
// status beneath, then its link. It is styled and hyperlinked; write it
// through a colorprofile writer, which strips that when the output isn't a
// terminal.
func Text(w io.Writer, s model.Snapshot) error {
	var blocks []string
	for _, l := range []struct {
		name string
		list model.List
	}{{"Pull requests", s.PRs}, {"Issues", s.Issues}} {
		if l.list.Count == 0 {
			continue
		}
		var b strings.Builder
		b.WriteString(heading.Render(l.name) + "\n")
		for _, sec := range l.list.Sections {
			b.WriteString("\n" + bold.Render(sec.Name) + faint.Render(fmt.Sprintf(" (%d)", len(sec.Rows))) + "\n")
			for _, r := range sec.Rows {
				row(&b, s, r)
			}
		}
		blocks = append(blocks, b.String())
	}
	_, err := io.WriteString(w, strings.Join(blocks, "\n"))
	return err
}

func row(b *strings.Builder, s model.Snapshot, r model.Row) {
	it := r.Item()
	state, style := string(r.CI), ci[r.CI]
	if r.Issue != nil {
		state, style = string(r.Fix), fix[r.Fix]
	}
	fmt.Fprintf(b, "  %s %s  %s  %s\n",
		style.Render(fmt.Sprintf("%-7s", state)),
		faint.Render(fmt.Sprintf("%4s", s.Since(r.Activity))),
		link(accent, it.URL).Render(it.Ref()),
		it.Title)
	facts := []string{accent.Render(strings.Join(r.Labels(), ", "))}
	for _, l := range slices.Concat(r.Left, r.Mine) {
		facts = append(facts, link(kinds[l.Kind], l.URL).Render(l.Text))
	}
	if pr := r.PR; pr != nil {
		for _, is := range pr.Issues {
			ref := link(accent, is.URL).Render(fmt.Sprintf("%s#%d", is.Repo, is.Number))
			facts = append(facts, "linked "+ref+" "+strings.ToLower(is.State))
		}
	}
	if is := r.Issue; is != nil {
		if len(is.Assignees) > 0 {
			names := make([]string, len(is.Assignees))
			for i, a := range is.Assignees {
				names[i] = a.Login
			}
			facts = append(facts, "assignees "+strings.Join(names, ", "))
		}
		if len(is.Labels) > 0 {
			facts = append(facts, "labels "+strings.Join(is.Labels, ", "))
		}
	}
	b.WriteString(indent + strings.Join(facts, faint.Render(" · ")) + "\n")
	b.WriteString(indent + faint.Render(it.URL) + "\n")
}

// link hyperlinks web urls only: check urls come from third-party apps.
func link(s lipgloss.Style, url string) lipgloss.Style {
	if browser.Valid(url) {
		return s.Hyperlink(url)
	}
	return s
}
