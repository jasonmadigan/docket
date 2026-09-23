package web

import (
	"fmt"
	"time"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/model"
)

type pageView struct {
	Title    string
	Loaded   bool
	Count    int
	Updated  string
	Next     string
	Budget   string
	Err      string
	Warnings []string
	Sections []sectionView
}

type sectionView struct {
	Name string
	Rows []rowView
}

type rowView struct {
	ID        string
	URL       string
	Ref       string
	Title     string
	Author    string
	Tags      []string
	CI        model.CI
	CILabel   string
	Review    string
	Age       string
	Meta      string
	Left      []model.Line
	Mine      []model.Line
	Failing   []model.Check
	Reviewers []model.ReviewerState
	Issues    []model.Issue
}

var ciLabels = map[model.CI]string{
	model.CIPass:    "CI passing",
	model.CIFail:    "CI failing",
	model.CIRunning: "CI running",
	model.CINone:    "no CI",
}

func view(st engine.State, loc *time.Location) pageView {
	snap := st.Snapshot
	v := pageView{Title: "docket", Loaded: st.Loaded, Warnings: st.Warnings}
	if st.Loaded {
		v.Title = fmt.Sprintf("docket (%d)", snap.Count)
		v.Count = snap.Count
		v.Updated = st.Updated.In(loc).Format("15:04")
	}
	if !st.Next.IsZero() {
		v.Next = st.Next.In(loc).Format("15:04")
	}
	if b := st.Budget; b.Limit > 0 {
		v.Budget = fmt.Sprintf("%d/%d", b.Remaining, b.Limit)
	}
	if st.Err != nil {
		v.Err = st.Err.Error()
	}
	for _, sec := range snap.Sections {
		sv := sectionView{Name: sec.Name}
		for _, r := range sec.Rows {
			sv.Rows = append(sv.Rows, rowOf(r, snap))
		}
		v.Sections = append(v.Sections, sv)
	}
	return v
}

func rowOf(r model.Row, snap model.Snapshot) rowView {
	v := rowView{
		ID:        r.PR.ID,
		URL:       webOnly(r.PR.URL),
		Ref:       r.PR.Ref(),
		Title:     r.PR.Title,
		Tags:      r.Labels(),
		CI:        r.CI,
		CILabel:   ciLabels[r.CI],
		Review:    r.Review,
		Age:       snap.Since(r.Activity),
		Meta:      snap.Meta(r),
		Left:      webLines(r.Left),
		Mine:      webLines(r.Mine),
		Reviewers: model.Reviewers(r.PR),
	}
	for _, c := range r.PR.Checks.Failing {
		v.Failing = append(v.Failing, model.Check{Name: c.Name, URL: webOnly(c.URL)})
	}
	for _, is := range r.PR.Issues {
		is.URL = webOnly(is.URL)
		v.Issues = append(v.Issues, is)
	}
	if !snap.IsMe(r.PR.Author) {
		v.Author = r.PR.Author.Login
	}
	return v
}

// webOnly drops links other than http and https: check urls come from
// third-party apps, and html/template still lets mailto and relative ones
// through.
func webOnly(url string) string {
	if browser.Valid(url) {
		return url
	}
	return ""
}

func webLines(lines []model.Line) []model.Line {
	out := make([]model.Line, len(lines))
	for i, l := range lines {
		l.URL = webOnly(l.URL)
		out[i] = l
	}
	return out
}
