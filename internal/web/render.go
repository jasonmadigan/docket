package web

import (
	"fmt"
	"time"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/model"
)

type pageView struct {
	Settings     bool
	SettingsPath string
	Title        string
	Loaded       bool
	Login        string
	Busy         bool
	Phase        string
	Updated      string
	Next         string
	Budget       string
	Err          string
	Warnings     []string
	Tabs         []tabView
}

// tabView is one list; app.js shows the one the url fragment names.
type tabView struct {
	Key      string
	Name     string
	Count    int
	Hidden   bool
	Empty    string
	Sections []sectionView
}

type sectionView struct {
	Name string
	Rows []rowView
}

type rowView struct {
	ID         string
	Kind       string // pr or issue
	Changed    bool
	Archived   bool
	CanArchive bool
	URL        string
	Ref        string
	Title      string
	Author     string
	Tags       []string
	Glyph      string // css classes: ci for a pr, fix for an issue
	GlyphLabel string
	Review     string
	Age        string
	Meta       string
	Left       []model.Line
	Mine       []model.Line
	Failing    []model.Check
	Reviewers  []model.ReviewerState
	Issues     []model.IssueRef
	Assignees  []string
	Labels     []string
	PRs        []linkedPR
}

type linkedPR struct {
	Ref, Title, URL, Status string
}

var ciLabels = map[model.CI]string{
	model.CIPass:    "CI passing",
	model.CIFail:    "CI failing",
	model.CIRunning: "CI running",
	model.CINone:    "no CI",
}

var fixLabels = map[model.Fix]string{
	model.FixMerged: "linked PR merged",
	model.FixOpen:   "linked PR open",
	model.FixNone:   "no linked PR",
}

func view(st engine.State, loc *time.Location, canArchive bool) pageView {
	snap := st.Snapshot
	v := pageView{Title: "docket", Loaded: st.Loaded, Login: snap.Login, Warnings: st.Warnings}
	if st.Loaded {
		v.Title = fmt.Sprintf("docket (%d)", snap.PRs.Count+snap.Issues.Count)
		v.Updated = st.Updated.In(loc).Format("15:04")
	}
	if v.Busy = st.Busy || (!st.Loaded && st.Err == nil); v.Busy {
		v.Phase = phase(st.Progress.Phase, st.Progress.Done, st.Progress.Total)
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
	changed := map[string]bool{}
	for _, id := range st.Changed {
		changed[id] = true
	}
	for i, t := range []struct {
		key, name, empty string
		list             model.List
	}{
		{"prs", "Pull requests", "No open PRs involve you.", snap.PRs},
		{"issues", "Issues", "No open issues involve you.", snap.Issues},
		{"archived", "Archived", "Nothing archived.", snap.Archived},
	} {
		tv := tabView{Key: t.key, Name: t.name, Count: t.list.Count, Hidden: i > 0, Empty: t.empty}
		for _, sec := range t.list.Sections {
			sv := sectionView{Name: sec.Name}
			for _, r := range sec.Rows {
				row := rowOf(r, snap)
				row.Changed = changed[row.ID]
				row.CanArchive = canArchive
				sv.Rows = append(sv.Rows, row)
			}
			tv.Sections = append(tv.Sections, sv)
		}
		v.Tabs = append(v.Tabs, tv)
	}
	return v
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

func rowOf(r model.Row, snap model.Snapshot) rowView {
	it := r.Item()
	v := rowView{
		ID:       it.ID,
		URL:      webOnly(it.URL),
		Ref:      it.Ref(),
		Title:    it.Title,
		Tags:     r.Labels(),
		Review:   r.Review,
		Age:      snap.Since(r.Activity),
		Meta:     snap.Meta(r),
		Left:     webLines(r.Left),
		Mine:     webLines(r.Mine),
		Archived: !r.Archived.IsZero(),
	}
	if !snap.IsMe(it.Author) {
		v.Author = it.Author.Login
	}
	if pr := r.PR; pr != nil {
		v.Kind, v.Glyph, v.GlyphLabel = "pr", "ci ci-"+string(r.CI), ciLabels[r.CI]
		v.Reviewers = model.Reviewers(*pr)
		for _, c := range pr.Checks.Failing {
			v.Failing = append(v.Failing, model.Check{Name: c.Name, URL: webOnly(c.URL)})
		}
		for _, is := range pr.Issues {
			is.URL = webOnly(is.URL)
			v.Issues = append(v.Issues, is)
		}
	}
	if is := r.Issue; is != nil {
		v.Kind, v.Glyph, v.GlyphLabel = "issue", "fix fix-"+string(r.Fix), fixLabels[r.Fix]
		v.Labels = is.Labels
		for _, a := range is.Assignees {
			v.Assignees = append(v.Assignees, a.Login)
		}
		for _, pr := range is.PRs {
			v.PRs = append(v.PRs, linkedPR{Ref: pr.Ref(), Title: pr.Title, URL: webOnly(pr.URL), Status: pr.Status()})
		}
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
