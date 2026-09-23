package web

import (
	"fmt"
	"time"

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
		URL:       r.PR.URL,
		Ref:       r.PR.Ref(),
		Title:     r.PR.Title,
		Tags:      r.Labels(),
		CI:        r.CI,
		CILabel:   ciLabels[r.CI],
		Review:    r.Review,
		Age:       snap.Since(r.Activity),
		Meta:      snap.Meta(r),
		Left:      r.Left,
		Mine:      r.Mine,
		Failing:   r.PR.Checks.Failing,
		Reviewers: model.Reviewers(r.PR),
		Issues:    r.PR.Issues,
	}
	if !snap.IsMe(r.PR.Author) {
		v.Author = r.PR.Author.Login
	}
	return v
}
