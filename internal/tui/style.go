package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/jasonmadigan/docket/internal/model"
)

type styles struct {
	header, faint, accent, bold lipgloss.Style
	badge, section, rule, link  lipgloss.Style
	selected, bar               color.Color
	kinds                       map[model.LineKind]lipgloss.Style
	ci                          map[model.CI]lipgloss.Style
	review                      map[string]lipgloss.Style
}

func newStyles(dark bool) styles {
	pick := lipgloss.LightDark(dark)
	c := func(l, d string) color.Color { return pick(lipgloss.Color(l), lipgloss.Color(d)) }
	fg := func(l, d string) lipgloss.Style { return lipgloss.NewStyle().Foreground(c(l, d)) }
	bad, wait, good := fg("#cf222e", "#ff7b72"), fg("#9a6700", "#d29922"), fg("#1a7f37", "#3fb950")
	faint, accent := fg("#656d76", "#8b949e"), fg("#0969da", "#58a6ff")
	return styles{
		header:   accent.Bold(true),
		faint:    faint,
		accent:   accent,
		bold:     lipgloss.NewStyle().Bold(true),
		badge:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(c("#0969da", "#1f6feb")),
		section:  accent.Bold(true).Background(c("#ddf4ff", "#0c2d6b")),
		rule:     fg("#d0d7de", "#30363d"),
		link:     accent.Underline(true),
		selected: c("#ddf4ff", "#1c2d41"),
		bar:      c("#eaeef2", "#161b22"),
		kinds:    map[model.LineKind]lipgloss.Style{model.Bad: bad, model.Wait: wait, model.Good: good, model.Info: faint},
		ci:       map[model.CI]lipgloss.Style{model.CIPass: good, model.CIFail: bad, model.CIRunning: wait, model.CINone: faint},
		review:   map[string]lipgloss.Style{"approved": good, "changes": bad, "review": wait},
	}
}

var (
	glyph   = map[model.LineKind]string{model.Bad: "✗", model.Wait: "○", model.Good: "✓", model.Info: "·"}
	ciGlyph = map[model.CI]string{model.CIPass: "✓", model.CIFail: "✗", model.CIRunning: "●", model.CINone: "·"}
)
