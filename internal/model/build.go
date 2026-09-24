package model

import (
	"cmp"
	"fmt"
	"regexp"
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
	PR       *PR       `json:"pr,omitempty"`
	Issue    *Issue    `json:"issue,omitempty"`
	Teams    []string  `json:"teams,omitempty"` // my teams asked to review
	Activity time.Time `json:"activity"`        // zero when no person has touched it
	CI       CI        `json:"ci,omitempty"`    // prs
	Fix      Fix       `json:"fix,omitempty"`   // issues
	Review   string    `json:"review,omitempty"`
	Left     []Line    `json:"left"`
	Mine     []Line    `json:"mine,omitempty"`
}

// Item is what the row's pull request or issue shares with the other kind.
func (r Row) Item() Item {
	if r.Issue != nil {
		return r.Issue.Item
	}
	return r.PR.Item
}

type Section struct {
	Name string `json:"name"`
	Rows []Row  `json:"rows"`
}

// List is one tab's sections.
type List struct {
	Count    int       `json:"count"`
	Sections []Section `json:"sections"`
}

type Snapshot struct {
	Login  string    `json:"login"`
	At     time.Time `json:"at"`
	PRs    List      `json:"prs"`
	Issues List      `json:"issues"`
}

type Params struct {
	Login  string
	Teams  []string
	Ignore []string
	Now    time.Time
}

type group struct {
	name string
	tags []Tag
}

var prGroups = []group{
	{"Mine", []Tag{TagAuthor}},
	{"Requested", []Tag{TagReview, TagTeam, TagAssigned}},
	{"Mentioned", []Tag{TagMentioned}},
	{"Participating", []Tag{TagReviewed, TagCommented}},
}

var issueGroups = []group{
	{"Mine", []Tag{TagAuthor}},
	{"Assigned", []Tag{TagAssigned}},
	{"Mentioned", []Tag{TagMentioned}},
	{"Participating", []Tag{TagCommented}},
}

// Build files each pr and issue under its first matching section, longest
// neglected first.
func Build(prs []PR, issues []Issue, p Params) Snapshot {
	people := newPeople(p.Login, p.Ignore)
	teams := map[string]bool{}
	for _, t := range p.Teams {
		teams[strings.ToLower(t)] = true
	}
	var prRows, issueRows []Row
	for _, pr := range prs {
		prRows = append(prRows, Row{
			PR:       &pr,
			Teams:    myTeams(pr, teams),
			Activity: lastHumanActivity(pr, people),
			CI:       ciOf(pr.Checks),
			Review:   reviewOf(pr.ReviewDecision),
			Left:     whatsLeft(pr),
			Mine:     mySide(pr, people, teams, p.Now),
		})
	}
	for _, is := range issues {
		issueRows = append(issueRows, Row{
			Issue:    &is,
			Activity: issueActivity(is, people),
			Fix:      fixOf(is.PRs),
			Left:     issueLeft(is),
			Mine:     issueMine(is, people, p.Now),
		})
	}
	return Snapshot{Login: p.Login, At: p.Now, PRs: file(prRows, prGroups), Issues: file(issueRows, issueGroups)}
}

// file puts each row under the first group its tags match, longest
// neglected first. Rows matching none are dropped; empty groups are hidden.
func file(rows []Row, groups []group) List {
	grouped := make([][]Row, len(groups))
	for _, r := range rows {
		if i := groupOf(r.Item().Tags, groups); i >= 0 {
			grouped[i] = append(grouped[i], r)
		}
	}
	var l List
	for i, rs := range grouped {
		if len(rs) == 0 {
			continue
		}
		slices.SortStableFunc(rs, byActivity)
		l.Sections = append(l.Sections, Section{Name: groups[i].name, Rows: rs})
		l.Count += len(rs)
	}
	return l
}

func groupOf(tags []Tag, groups []group) int {
	for i, g := range groups {
		for _, t := range g.tags {
			if slices.Contains(tags, t) {
				return i
			}
		}
	}
	return -1
}

func byActivity(a, b Row) int {
	x, y := a.Item(), b.Item()
	return cmp.Or(
		a.Activity.Compare(b.Activity),
		strings.Compare(x.Repo, y.Repo),
		cmp.Compare(x.Number, y.Number),
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
	it := r.Item()
	var parts []string
	if it.Author.Login != "" && !s.IsMe(it.Author) {
		parts = append(parts, "by "+it.Author.Login)
	}
	parts = append(parts, "opened "+s.Since(it.CreatedAt)+" ago")
	if !r.Activity.IsZero() {
		parts = append(parts, "touched "+s.Since(r.Activity)+" ago")
	}
	return strings.Join(parts, " · ")
}

// Labels are the tags, naming the teams behind a team tag.
func (r Row) Labels() []string {
	tags := r.Item().Tags
	labels := make([]string, len(tags))
	for i, t := range tags {
		labels[i] = string(t)
		if t == TagTeam && len(r.Teams) > 0 {
			labels[i] += " (" + strings.Join(r.Teams, ", ") + ")"
		}
	}
	return labels
}

var agoSuffix = regexp.MustCompile(` \d+(s|m|h|d|w|mo|y) ago$`)

// Fingerprint changes when anything shown for the row does, but not when
// time alone moves an age on.
func (r Row) Fingerprint() string {
	parts := []string{r.Item().Title, string(r.CI), string(r.Fix), r.Review, r.Activity.UTC().Format(time.RFC3339Nano)}
	for _, l := range slices.Concat(r.Left, r.Mine) {
		parts = append(parts, agoSuffix.ReplaceAllString(l.Text, ""))
	}
	if r.PR != nil {
		for _, is := range r.PR.Issues {
			parts = append(parts, fmt.Sprintf("%s#%d %s", is.Repo, is.Number, is.State))
		}
	}
	if r.Issue != nil {
		parts = append(parts, strings.Join(r.Issue.Labels, ","))
		for _, a := range r.Issue.Assignees {
			parts = append(parts, a.Login)
		}
	}
	return strings.Join(parts, "\x1f")
}
