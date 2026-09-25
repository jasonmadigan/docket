package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func archiveFixture() ([]PR, []Issue) {
	prs := []PR{
		{Item: Item{ID: "p1", Repo: "acme/a", Number: 1, Author: user("me"), CreatedAt: at(1), Tags: []Tag{TagAuthor}}},
		{Item: Item{ID: "p2", Repo: "acme/a", Number: 2, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagReview}}},
	}
	issues := []Issue{
		{Item: Item{ID: "i1", Repo: "acme/a", Number: 3, Author: user("me"), CreatedAt: at(1), Tags: []Tag{TagAuthor}}},
		{Item: Item{ID: "i2", Repo: "acme/a", Number: 4, Author: user("bob"), CreatedAt: at(1), Tags: []Tag{TagCommented}}},
		{Item: Item{ID: "i3", Repo: "acme/a", Number: 5, Author: user("carol"), CreatedAt: at(1), Tags: []Tag{TagMentioned}}},
	}
	return prs, issues
}

func listed(l List) map[string][]string {
	out := map[string][]string{}
	for _, s := range l.Sections {
		for _, r := range s.Rows {
			out[s.Name] = append(out[s.Name], r.Item().ID)
		}
	}
	return out
}

func TestBuildShelvesArchivedItems(t *testing.T) {
	prs, issues := archiveFixture()
	snap := Build(prs, issues, Params{Login: "me", Now: at(10), Archive: map[string]time.Time{
		"p2": at(5), "i2": at(6), "i3": at(7), "closed-since": at(8),
	}})
	if got := listed(snap.PRs); !reflect.DeepEqual(got, map[string][]string{"Mine": {"p1"}}) {
		t.Fatalf("prs = %v", got)
	}
	if got := listed(snap.Issues); !reflect.DeepEqual(got, map[string][]string{"Mine": {"i1"}}) {
		t.Fatalf("issues = %v", got)
	}
	if got, want := listed(snap.Archived), map[string][]string{"Pull requests": {"p2"}, "Issues": {"i3", "i2"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("archived = %v, want %v", got, want)
	}
	var names []string
	for _, s := range snap.Archived.Sections {
		names = append(names, s.Name)
	}
	if !reflect.DeepEqual(names, []string{"Pull requests", "Issues"}) {
		t.Fatalf("archived sections = %v", names)
	}
	if snap.PRs.Count != 1 || snap.Issues.Count != 1 || snap.Archived.Count != 3 {
		t.Fatalf("counts = %d, %d, %d", snap.PRs.Count, snap.Issues.Count, snap.Archived.Count)
	}
	row, ok := snap.Archived.Find("i2")
	if !ok || !row.Archived.Equal(at(6)) || len(row.Left) == 0 {
		t.Fatalf("archived row = %+v", row)
	}
	if len(snap.Void) != 0 {
		t.Fatalf("void = %v", snap.Void)
	}
}

func TestReopeningEndsAnArchive(t *testing.T) {
	_, issues := archiveFixture()
	issues[1].Timeline = []Event{{Kind: EventReopened, Actor: user("bob"), At: at(7)}}
	issues[2].Timeline = []Event{{Kind: EventReopened, Actor: user("carol"), At: at(3)}}
	snap := Build(nil, issues, Params{Login: "me", Now: at(10), Archive: map[string]time.Time{"i2": at(6), "i3": at(6)}})
	if _, ok := snap.Issues.Find("i2"); !ok {
		t.Fatal("reopened after it was archived, i2 belongs back in its section")
	}
	if _, ok := snap.Archived.Find("i3"); !ok {
		t.Fatal("reopened before it was archived, i3 stays archived")
	}
	if !reflect.DeepEqual(snap.Void, []string{"i2"}) {
		t.Fatalf("void = %v", snap.Void)
	}
}

func TestMetaSaysWhenArchived(t *testing.T) {
	snap := Snapshot{Login: "me", At: at(48)}
	row := Row{Issue: &Issue{Item: Item{Author: user("alice"), CreatedAt: at(0)}}, Archived: at(24)}
	if got := snap.Meta(row); got != "by alice · opened 2d ago · archived 1d ago" {
		t.Fatalf("meta = %q", got)
	}
}

func TestArchivingLeavesTheFingerprint(t *testing.T) {
	prs, _ := archiveFixture()
	live, _ := Build(prs, nil, Params{Login: "me", Now: at(10)}).PRs.Find("p2")
	shelved, _ := Build(prs, nil, Params{Login: "me", Now: at(10), Archive: map[string]time.Time{"p2": at(5)}}).Archived.Find("p2")
	if live.Fingerprint() != shelved.Fingerprint() {
		t.Fatal("archiving changed what the row shows")
	}
}

func TestArchivedJSON(t *testing.T) {
	prs, _ := archiveFixture()
	raw, err := json.Marshal(Build(prs, nil, Params{Login: "me", Now: at(10), Archive: map[string]time.Time{"p2": at(5)}}))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, `"archived":{"count":1`) || !strings.Contains(got, `"archived":"2026-09-01T17:00:00Z"`) {
		t.Fatalf("json = %s", got)
	}
	if strings.Count(got, `"archived":`) != 2 {
		t.Fatalf("a live row carries archived: %s", got)
	}
}

func TestUnfiledItemsStayOutOfTheArchive(t *testing.T) {
	issues := []Issue{{Item: Item{ID: "i9", Repo: "acme/a", Number: 9, Author: user("bob"), CreatedAt: at(1)}}}
	snap := Build(nil, issues, Params{Login: "me", Now: at(10), Archive: map[string]time.Time{"i9": at(5)}})
	if snap.Issues.Count != 0 || snap.Archived.Count != 0 || len(snap.Void) != 0 {
		t.Fatalf("an item matching no section was listed: %+v", snap)
	}
}
