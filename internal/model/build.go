package model

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

type CI string

const (
	CIPass    CI = "pass"
	CIFail    CI = "fail"
	CIRunning CI = "running"
	CINone    CI = "none"
)

type Row struct {
	PR       PR        `json:"pr"`
	Teams    []string  `json:"teams,omitempty"` // my teams asked to review
	Activity time.Time `json:"activity"`        // zero when no person has touched it
	CI       CI        `json:"ci"`
	Review   string    `json:"review,omitempty"`
	Left     []Line    `json:"left"`
	Mine     []Line    `json:"mine,omitempty"`
}

type Section struct {
	Name string `json:"name"`
	Rows []Row  `json:"rows"`
}

type Snapshot struct {
	Login    string    `json:"login"`
	At       time.Time `json:"at"`
	Count    int       `json:"count"`
	Sections []Section `json:"sections"`
}

type Params struct {
	Login  string
	Teams  []string
	Ignore []string
	Now    time.Time
}

var sections = []struct {
	name string
	tags []Tag
}{
	{"Mine", []Tag{TagAuthor}},
	{"Requested", []Tag{TagReview, TagTeam, TagAssigned}},
	{"Mentioned", []Tag{TagMentioned}},
	{"Participating", []Tag{TagReviewed, TagCommented}},
}

// Build files each pr under its first matching section, longest neglected
// first.
func Build(prs []PR, p Params) Snapshot {
	people := newPeople(p.Login, p.Ignore)
	teams := map[string]bool{}
	for _, t := range p.Teams {
		teams[strings.ToLower(t)] = true
	}
	grouped := make([][]Row, len(sections))
	for _, pr := range prs {
		i := sectionOf(pr.Tags)
		if i < 0 {
			continue
		}
		grouped[i] = append(grouped[i], Row{
			PR:       pr,
			Teams:    myTeams(pr, teams),
			Activity: lastHumanActivity(pr, people),
			CI:       ciOf(pr.Checks),
			Review:   reviewOf(pr.ReviewDecision),
			Left:     whatsLeft(pr),
			Mine:     mySide(pr, people, teams, p.Now),
		})
	}
	snap := Snapshot{Login: p.Login, At: p.Now}
	for i, rows := range grouped {
		if len(rows) == 0 {
			continue
		}
		slices.SortStableFunc(rows, byActivity)
		snap.Sections = append(snap.Sections, Section{Name: sections[i].name, Rows: rows})
		snap.Count += len(rows)
	}
	return snap
}

func sectionOf(tags []Tag) int {
	for i, s := range sections {
		for _, t := range s.tags {
			if slices.Contains(tags, t) {
				return i
			}
		}
	}
	return -1
}

func byActivity(a, b Row) int {
	return cmp.Or(
		a.Activity.Compare(b.Activity),
		strings.Compare(a.PR.Repo, b.PR.Repo),
		cmp.Compare(a.PR.Number, b.PR.Number),
	)
}

func myTeams(pr PR, teams map[string]bool) []string {
	var out []string
	for _, r := range pr.Requests {
		if r.Team && teams[strings.ToLower(r.Name)] {
			out = append(out, r.Name)
		}
	}
	return out
}

func ciOf(c Checks) CI {
	switch c.State {
	case "SUCCESS":
		return CIPass
	case "FAILURE", "ERROR":
		return CIFail
	case "PENDING", "EXPECTED":
		return CIRunning
	}
	return CINone
}

func reviewOf(decision string) string {
	switch decision {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes"
	case "REVIEW_REQUIRED":
		return "review"
	}
	return ""
}

// Since is how long before the snapshot t was, or "-" when t is zero.
func (s Snapshot) Since(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return Age(s.At.Sub(t))
}

func (s Snapshot) IsMe(a Actor) bool {
	return a.Login != "" && strings.EqualFold(a.Login, s.Login)
}

// Meta reads like "by alice · opened 5d ago · touched 2d ago".
func (s Snapshot) Meta(r Row) string {
	var parts []string
	if r.PR.Author.Login != "" && !s.IsMe(r.PR.Author) {
		parts = append(parts, "by "+r.PR.Author.Login)
	}
	parts = append(parts, "opened "+s.Since(r.PR.CreatedAt)+" ago")
	if !r.Activity.IsZero() {
		parts = append(parts, "touched "+s.Since(r.Activity)+" ago")
	}
	return strings.Join(parts, " · ")
}

// Labels are the tags, naming the teams behind a team tag.
func (r Row) Labels() []string {
	labels := make([]string, len(r.PR.Tags))
	for i, t := range r.PR.Tags {
		labels[i] = string(t)
		if t == TagTeam && len(r.Teams) > 0 {
			labels[i] += " (" + strings.Join(r.Teams, ", ") + ")"
		}
	}
	return labels
}
