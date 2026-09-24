package model

import (
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
	snap := Build(prs, Params{Login: "me", Now: at(10)})
	got := map[string][]string{}
	var order []string
	for _, s := range snap.Sections {
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
	if snap.Count != 5 || snap.Login != "me" || !snap.At.Equal(at(10)) {
		t.Fatalf("snapshot = %d %q %v", snap.Count, snap.Login, snap.At)
	}
}

func TestBuildBreaksTiesByRepoThenNumber(t *testing.T) {
	prs := []PR{
		{Item: Item{ID: "z9", Repo: "acme/z", Number: 9, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
		{Item: Item{ID: "a9", Repo: "acme/a", Number: 9, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
		{Item: Item{ID: "a2", Repo: "acme/a", Number: 2, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
	}
	var ids []string
	for _, r := range Build(prs, Params{Login: "me", Now: at(10)}).Sections[0].Rows {
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
	row := Build([]PR{pr}, Params{Login: "me", Teams: []string{"Acme/Devs"}, Now: at(10)}).Sections[0].Rows[0]
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
	early := Build([]PR{pr}, Params{Login: "me", Now: at(2)}).Sections[0].Rows[0]
	late := Build([]PR{pr}, Params{Login: "me", Now: at(90)}).Sections[0].Rows[0]
	if early.Fingerprint() != late.Fingerprint() {
		t.Fatalf("time passing changed the fingerprint:\n%q\n%q", early.Fingerprint(), late.Fingerprint())
	}
	pr.Checks = Checks{State: "FAILURE"}
	failing := Build([]PR{pr}, Params{Login: "me", Now: at(2)}).Sections[0].Rows[0]
	if failing.Fingerprint() == early.Fingerprint() {
		t.Fatal("a check failing left the fingerprint alone")
	}
	pr.Timeline = append(pr.Timeline, Event{Kind: EventComment, Actor: user("carol"), At: at(3)})
	commented := Build([]PR{pr}, Params{Login: "me", Now: at(4)}).Sections[0].Rows[0]
	if commented.Fingerprint() == failing.Fingerprint() {
		t.Fatal("a new comment left the fingerprint alone")
	}
}
