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
	bold   = lipgloss.NewStyle().Bold(true)
	faint  = lipgloss.NewStyle().Faint(true)
	accent = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	red    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	green  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	ci     = map[model.CI]lipgloss.Style{model.CIPass: green, model.CIFail: red.Bold(true), model.CIRunning: yellow, model.CINone: faint}
	kinds  = map[model.LineKind]lipgloss.Style{model.Bad: red, model.Wait: yellow, model.Good: green, model.Info: faint}
)

func JSON(w io.Writer, s model.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// Text prints a line per pr, its tags and status beneath, then its link.
// It is styled and hyperlinked; write it through a colorprofile writer,
// which strips that when the output isn't a terminal.
func Text(w io.Writer, s model.Snapshot) error {
	var b strings.Builder
	indent := strings.Repeat(" ", 16)
	for i, sec := range s.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(bold.Render(sec.Name) + faint.Render(fmt.Sprintf(" (%d)", len(sec.Rows))) + "\n")
		for _, r := range sec.Rows {
			fmt.Fprintf(&b, "  %s %s  %s  %s\n",
				ci[r.CI].Render(fmt.Sprintf("%-7s", r.CI)),
				faint.Render(fmt.Sprintf("%4s", s.Since(r.Activity))),
				link(accent, r.PR.URL).Render(r.PR.Ref()),
				r.PR.Title)
			facts := []string{accent.Render(strings.Join(r.Labels(), ", "))}
			for _, l := range slices.Concat(r.Left, r.Mine) {
				facts = append(facts, link(kinds[l.Kind], l.URL).Render(l.Text))
			}
			for _, is := range r.PR.Issues {
				ref := link(accent, is.URL).Render(fmt.Sprintf("%s#%d", is.Repo, is.Number))
				facts = append(facts, "linked "+ref+" "+strings.ToLower(is.State))
			}
			b.WriteString(indent + strings.Join(facts, faint.Render(" · ")) + "\n")
			b.WriteString(indent + faint.Render(r.PR.URL) + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// link hyperlinks web urls only: check urls come from third-party apps.
func link(s lipgloss.Style, url string) lipgloss.Style {
	if browser.Valid(url) {
		return s.Hyperlink(url)
	}
	return s
}
