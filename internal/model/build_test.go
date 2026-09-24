package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestBuildFilesAndSorts(t *testing.T) {
	prs := []PR{
		{Item: Item{ID: "a", Repo: "acme/a", Number: 1, Author: user("me"), CreatedAt: at(1), Tags: []Tag{TagAuthor, TagMentioned}}},
		{Item: Item{ID: "b", Repo: "acme/b", Number: 2, Author: user("bob"), CreatedAt: at(5), Tags: []Tag{TagReview}}},
		{Item: Item{ID: "c", Repo: "acme/c", Number: 3, Author: user("carol"), CreatedAt: at(1), Tags: []Tag{TagTeam}}},
		{Item: Item{ID: "d", Repo: "acme/d", Number: 4, Author: bot("dependabot"), CreatedAt: at(9), Tags: []Tag{TagAssigned}}},
		{Item: Item{ID: "e", Repo: "acme/e", Number: 5, Author: user("erin"), CreatedAt: at(2), Tags: []Tag{TagCommented, TagReviewed}}},
		{Item: Item{ID: "f", Repo: "acme/f", Number: 6, Author: user("fred"), CreatedAt: at(2)}},
	}
	snap := Build(prs, nil, Params{Login: "me", Now: at(10)})
	got := map[string][]string{}
	var order []string
	for _, s := range snap.PRs.Sections {
		order = append(order, s.Name)
		for _, r := range s.Rows {
			got[s.Name] = append(got[s.Name], r.PR.ID)
		}
	}
	if want := []string{"Mine", "Requested", "Participating"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("sections = %v, want %v", order, want)
	}
	want := map[string][]string{"Mine": {"a"}, "Requested": {"d", "c", "b"}, "Participating": {"e"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if snap.PRs.Count != 5 || snap.Login != "me" || !snap.At.Equal(at(10)) {
		t.Fatalf("snapshot = %d %q %v", snap.PRs.Count, snap.Login, snap.At)
	}
}

func TestBuildBreaksTiesByRepoThenNumber(t *testing.T) {
	prs := []PR{
		{Item: Item{ID: "z9", Repo: "acme/z", Number: 9, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
		{Item: Item{ID: "a9", Repo: "acme/a", Number: 9, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
		{Item: Item{ID: "a2", Repo: "acme/a", Number: 2, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
	}
	var ids []string
	for _, r := range Build(prs, nil, Params{Login: "me", Now: at(10)}).PRs.Sections[0].Rows {
		ids = append(ids, r.PR.ID)
	}
	if want := []string{"a2", "a9", "z9"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
}

func TestBuildFillsRows(t *testing.T) {
	pr := PR{
		Item:           Item{ID: "a", Repo: "acme/a", Number: 1, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagTeam}},
		Checks:         Checks{State: "FAILURE"},
		ReviewDecision: "CHANGES_REQUESTED",
		Requests:       []Reviewer{{Name: "acme/devs", Team: true}, {Name: "other/ops", Team: true}},
	}
	row := Build([]PR{pr}, nil, Params{Login: "me", Teams: []string{"Acme/Devs"}, Now: at(10)}).PRs.Sections[0].Rows[0]
	if row.CI != CIFail || row.Review != "changes" || !row.Activity.Equal(at(1)) {
		t.Fatalf("row = %+v", row)
	}
	if !reflect.DeepEqual(row.Teams, []string{"acme/devs"}) {
		t.Fatalf("teams = %v", row.Teams)
	}
	if !reflect.DeepEqual(row.Labels(), []string{"team (acme/devs)"}) {
		t.Fatalf("labels = %v", row.Labels())
	}
	if len(row.Left) == 0 || len(row.Mine) != 1 || row.Mine[0].Text != "review requested from acme/devs" {
		t.Fatalf("lines = %+v / %+v", row.Left, row.Mine)
	}
}

func TestSnapshotPresentation(t *testing.T) {
	snap := Snapshot{Login: "me", At: at(48)}
	row := Row{PR: &PR{Item: Item{Author: user("alice"), CreatedAt: at(0)}}, Activity: at(45)}
	if got := snap.Meta(row); got != "by alice · opened 2d ago · touched 3h ago" {
		t.Fatalf("meta = %q", got)
	}
	if got := snap.Meta(Row{PR: &PR{Item: Item{Author: user("ME"), CreatedAt: at(0)}}}); got != "opened 2d ago" {
		t.Fatalf("meta = %q", got)
	}
	if got := snap.Since(time.Time{}); got != "-" {
		t.Fatalf("since zero = %q", got)
	}
	if snap.IsMe(Actor{}) {
		t.Fatal("an empty actor is not me")
	}
}

func TestFingerprintIgnoresTheClock(t *testing.T) {
	pr := PR{
		Item: Item{
			ID: "a", Repo: "acme/a", Number: 1, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview},
			Timeline: []Event{{Kind: EventReviewRequested, Actor: user("bob"), At: at(1), Target: "me"}},
		},
		Requests: []Reviewer{{Name: "me"}},
	}
	early := Build([]PR{pr}, nil, Params{Login: "me", Now: at(2)}).PRs.Sections[0].Rows[0]
	late := Build([]PR{pr}, nil, Params{Login: "me", Now: at(90)}).PRs.Sections[0].Rows[0]
	if early.Fingerprint() != late.Fingerprint() {
		t.Fatalf("time passing changed the fingerprint:\n%q\n%q", early.Fingerprint(), late.Fingerprint())
	}
	pr.Checks = Checks{State: "FAILURE"}
	failing := Build([]PR{pr}, nil, Params{Login: "me", Now: at(2)}).PRs.Sections[0].Rows[0]
	if failing.Fingerprint() == early.Fingerprint() {
		t.Fatal("a check failing left the fingerprint alone")
	}
	pr.Timeline = append(pr.Timeline, Event{Kind: EventComment, Actor: user("carol"), At: at(3)})
	commented := Build([]PR{pr}, nil, Params{Login: "me", Now: at(4)}).PRs.Sections[0].Rows[0]
	if commented.Fingerprint() == failing.Fingerprint() {
		t.Fatal("a new comment left the fingerprint alone")
	}
}

func TestBuildFilesIssues(t *testing.T) {
	issues := []Issue{
		{Item: Item{ID: "i1", Repo: "acme/a", Number: 1, Author: user("me"), CreatedAt: at(3), Tags: []Tag{TagAuthor, TagCommented}}},
		{Item: Item{ID: "i2", Repo: "acme/a", Number: 2, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagAssigned, TagMentioned}}},
		{Item: Item{ID: "i3", Repo: "acme/a", Number: 3, Author: user("carol"), CreatedAt: at(2), Tags: []Tag{TagMentioned}}},
		{Item: Item{ID: "i4", Repo: "acme/a", Number: 4, Author: user("dave"), CreatedAt: at(1), Tags: []Tag{TagCommented}}},
		{Item: Item{ID: "i5", Repo: "acme/a", Number: 5, Author: user("erin"), CreatedAt: at(1)}},
	}
	snap := Build(nil, issues, Params{Login: "me", Now: at(10)})
	got := map[string][]string{}
	var order []string
	for _, s := range snap.Issues.Sections {
		order = append(order, s.Name)
		for _, r := range s.Rows {
			got[s.Name] = append(got[s.Name], r.Issue.ID)
		}
	}
	if want := []string{"Mine", "Assigned", "Mentioned", "Participating"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("sections = %v, want %v", order, want)
	}
	want := map[string][]string{"Mine": {"i1"}, "Assigned": {"i2"}, "Mentioned": {"i3"}, "Participating": {"i4"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if snap.Issues.Count != 4 || snap.PRs.Count != 0 || snap.PRs.Sections != nil {
		t.Fatalf("counts: %d issues, %d prs", snap.Issues.Count, snap.PRs.Count)
	}
}

func TestBuildFillsIssueRows(t *testing.T) {
	is := Issue{
		Item: Item{ID: "i1", Repo: "acme/a", Number: 1, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagAssigned},
			Timeline: []Event{{Kind: EventAssigned, Actor: user("bob"), At: at(2), Target: "me"}}},
		Assignees: []Actor{user("me")},
		PRs:       []PRRef{{Repo: "acme/a", Number: 9, URL: "https://github.com/acme/a/pull/9", State: "MERGED"}},
	}
	row := Build(nil, []Issue{is}, Params{Login: "me", Now: at(50)}).Issues.Sections[0].Rows[0]
	if row.PR != nil || row.Issue == nil || row.Item().ID != "i1" {
		t.Fatalf("row = %+v", row)
	}
	if row.Fix != FixMerged || row.CI != "" || row.Review != "" || !row.Activity.Equal(at(2)) {
		t.Fatalf("row = %+v", row)
	}
	wantLeft := []Line{{Kind: Good, Text: "PR acme/a#9 merged", URL: "https://github.com/acme/a/pull/9"}}
	wantMine := []Line{{Kind: Info, Text: "assigned to you 2d ago"}}
	if !reflect.DeepEqual(row.Left, wantLeft) || !reflect.DeepEqual(row.Mine, wantMine) {
		t.Fatalf("lines = %+v / %+v", row.Left, row.Mine)
	}
}

func TestFingerprintNoticesIssueChanges(t *testing.T) {
	is := Issue{Item: Item{ID: "i1", Repo: "acme/a", Number: 1, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagAssigned}}}
	fp := func(is Issue, now time.Time) string {
		return Build(nil, []Issue{is}, Params{Login: "me", Now: now}).Issues.Sections[0].Rows[0].Fingerprint()
	}
	base := fp(is, at(2))
	if fp(is, at(90)) != base {
		t.Fatal("time passing changed the fingerprint")
	}
	labelled := is
	labelled.Labels = []string{"bug"}
	if fp(labelled, at(2)) == base {
		t.Fatal("a new label left the fingerprint alone")
	}
}

func TestSnapshotJSONHoldsBothLists(t *testing.T) {
	prs := []PR{{Item: Item{ID: "p1", Repo: "acme/a", Number: 1, Author: user("me"), CreatedAt: at(1), Tags: []Tag{TagAuthor}}}}
	issues := []Issue{{Item: Item{ID: "i1", Repo: "acme/a", Number: 2, Author: user("me"), CreatedAt: at(1), Tags: []Tag{TagAuthor}}}}
	raw, err := json.Marshal(Build(prs, issues, Params{Login: "me", Now: at(2)}))
	if err != nil {
		t.Fatal(err)
	}
	type list struct {
		Count    int `json:"count"`
		Sections []struct {
			Rows []map[string]json.RawMessage `json:"rows"`
		} `json:"sections"`
	}
	var back struct {
		PRs    list `json:"prs"`
		Issues list `json:"issues"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.PRs.Count != 1 || back.Issues.Count != 1 {
		t.Fatalf("json = %s", raw)
	}
	pr, is := back.PRs.Sections[0].Rows[0], back.Issues.Sections[0].Rows[0]
	if _, ok := pr["pr"]; !ok {
		t.Errorf("pr row = %v", pr)
	}
	if _, ok := is["issue"]; !ok {
		t.Errorf("issue row = %v", is)
	}
	if _, ok := pr["fix"]; ok {
		t.Errorf("a pr row carries fix: %v", pr)
	}
	if _, ok := is["ci"]; ok {
		t.Errorf("an issue row carries ci: %v", is)
	}
}
