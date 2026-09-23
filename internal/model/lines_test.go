package model

import (
	"reflect"
	"testing"
	"time"
)

func TestWhatsLeft(t *testing.T) {
	green := Checks{State: "SUCCESS"}
	cases := []struct {
		name string
		pr   PR
		want []Line
	}{
		{"draft without checks", PR{Draft: true, MergeState: "DRAFT"},
			[]Line{{Kind: Info, Text: "draft"}, {Kind: Info, Text: "no CI"}}},
		{"conflicts", PR{Mergeable: "CONFLICTING", MergeState: "DIRTY", Checks: green},
			[]Line{{Kind: Bad, Text: "conflicts"}}},
		{"behind base", PR{Mergeable: "MERGEABLE", MergeState: "BEHIND", Checks: green},
			[]Line{{Kind: Bad, Text: "behind base"}}},
		{"failing checks", PR{MergeState: "UNSTABLE", Checks: Checks{State: "FAILURE", FailingCount: 5, Failing: []Check{
			{Name: "e2e", URL: "https://ci.example/e2e"}, {Name: "lint"}, {Name: "unit"}, {Name: "docs"}}}},
			[]Line{{Kind: Bad, Text: "CI failing: e2e, lint, unit +2", URL: "https://ci.example/e2e"}}},
		{"failing without names", PR{Checks: Checks{State: "ERROR"}},
			[]Line{{Kind: Bad, Text: "CI failing"}}},
		{"running", PR{Checks: Checks{State: "PENDING"}},
			[]Line{{Kind: Wait, Text: "CI running"}}},
		{"changes requested", PR{Checks: green, ReviewDecision: "CHANGES_REQUESTED", Opinions: []Review{
			{Author: user("bob"), State: "CHANGES_REQUESTED"}, {Author: user("carol"), State: "APPROVED"}}},
			[]Line{{Kind: Bad, Text: "changes requested by bob"}}},
		{"awaiting reviewers", PR{Checks: green, ReviewDecision: "REVIEW_REQUIRED", Requests: []Reviewer{
			{Name: "bob"}, {Name: "acme/devs", Team: true}}},
			[]Line{{Kind: Wait, Text: "awaiting bob, acme/devs"}}},
		{"needs more approvals", PR{Checks: green, ReviewDecision: "REVIEW_REQUIRED", Opinions: []Review{
			{Author: user("carol"), State: "APPROVED"}}},
			[]Line{{Kind: Wait, Text: "needs more approvals"}}},
		{"no reviewer", PR{Checks: green, ReviewDecision: "REVIEW_REQUIRED"},
			[]Line{{Kind: Bad, Text: "no reviewer"}}},
		{"one thread", PR{Checks: green, UnresolvedThreads: 1},
			[]Line{{Kind: Wait, Text: "1 unresolved thread"}}},
		{"threads past the page", PR{Checks: green, UnresolvedThreads: 3, MoreThreads: true},
			[]Line{{Kind: Wait, Text: "3+ unresolved threads"}}},
		{"ready", PR{MergeState: "CLEAN", Checks: green, ReviewDecision: "APPROVED"},
			[]Line{{Kind: Good, Text: "ready to merge"}}},
		{"ready without CI", PR{MergeState: "CLEAN"},
			[]Line{{Kind: Info, Text: "no CI"}, {Kind: Good, Text: "ready to merge"}}},
		{"clean but threads open", PR{MergeState: "CLEAN", Checks: green, UnresolvedThreads: 2},
			[]Line{{Kind: Wait, Text: "2 unresolved threads"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := whatsLeft(c.pr); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestMySide(t *testing.T) {
	p := newPeople("me", nil)
	teams := map[string]bool{"acme/devs": true}
	now := at(72)
	cases := []struct {
		name string
		pr   PR
		want []Line
	}{
		{"direct request", PR{
			Requests: []Reviewer{{Name: "me"}},
			Timeline: []Event{{Kind: EventReviewRequested, Actor: user("alice"), At: at(24), Target: "me"}},
		}, []Line{{Kind: Wait, Text: "review requested 2d ago"}}},
		{"direct request past the window", PR{Requests: []Reviewer{{Name: "Me"}}},
			[]Line{{Kind: Wait, Text: "review requested"}}},
		{"team request", PR{
			Requests: []Reviewer{{Name: "acme/devs", Team: true}, {Name: "other/ops", Team: true}},
			Timeline: []Event{{Kind: EventReviewRequested, Actor: user("alice"), At: at(69), Target: "acme/devs"}},
		}, []Line{{Kind: Wait, Text: "review requested from acme/devs 3h ago"}}},
		{"reviewed the head", PR{HeadOID: "c2", MyReview: &Review{State: "APPROVED", At: at(48), Commit: "c2"}},
			[]Line{{Kind: Info, Text: "you approved 1d ago"}}},
		{"commits since my review", PR{
			HeadOID: "c3", CommitCount: 3,
			Commits:  []Commit{{OID: "c1"}, {OID: "c2"}, {OID: "c3"}},
			MyReview: &Review{State: "CHANGES_REQUESTED", At: at(48), Commit: "c1"},
		}, []Line{{Kind: Wait, Text: "you requested changes, 2 commits since"}}},
		{"rewritten since my review", PR{
			HeadOID: "x2", CommitCount: 2,
			Commits:  []Commit{{OID: "x1"}, {OID: "x2"}},
			MyReview: &Review{State: "COMMENTED", At: at(48), Commit: "c1"},
		}, []Line{{Kind: Wait, Text: "rewritten since your review"}}},
		{"reviewed commit past the window", PR{
			HeadOID: "x2", CommitCount: 150,
			Commits:  []Commit{{OID: "x1"}, {OID: "x2"}},
			MyReview: &Review{State: "COMMENTED", At: at(48), Commit: "c1"},
		}, []Line{{Kind: Wait, Text: "you commented, 2+ commits since"}}},
		{"dismissed", PR{HeadOID: "c1", MyReview: &Review{State: "DISMISSED", At: at(48), Commit: "c1"}},
			[]Line{{Kind: Wait, Text: "your review was dismissed 1d ago"}}},
		{"unanswered mention", PR{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("bob"), At: at(71)},
				{Kind: EventMentioned, Actor: user("me"), At: at(71).Add(time.Second)},
			},
		}, []Line{{Kind: Wait, Text: "mentioned by bob 59m ago"}}},
		{"answered mention", PR{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("bob"), At: at(60)},
				{Kind: EventMentioned, Actor: user("me"), At: at(60)},
				{Kind: EventComment, Actor: user("me"), At: at(61)},
			},
		}, nil},
		{"mentioned in the description", PR{
			Author: user("alice"), CreatedAt: at(10),
			Timeline: []Event{{Kind: EventMentioned, Actor: user("me"), At: at(10)}},
		}, []Line{{Kind: Wait, Text: "mentioned by alice 2d ago"}}},
		{"mentioner unknown", PR{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{{Kind: EventMentioned, Actor: user("me"), At: at(50)}},
		}, []Line{{Kind: Wait, Text: "mentioned 22h ago"}}},
		{"replies since my comment", PR{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("me"), At: at(10)},
				{Kind: EventComment, Actor: user("bob"), At: at(20)},
				{Kind: EventReview, Actor: user("carol"), At: at(30), State: "COMMENTED"},
				{Kind: EventComment, Actor: bot("coderabbitai"), At: at(40)},
			},
		}, []Line{{Kind: Wait, Text: "2 replies since yours"}}},
		{"replies on my PR", PR{
			Author: user("me"), CreatedAt: at(0),
			Timeline: []Event{{Kind: EventReview, Actor: user("bob"), At: at(5), State: "APPROVED"}},
		}, []Line{{Kind: Wait, Text: "1 reply since yours"}}},
		{"replies beyond the window", PR{
			Author: user("alice"), CreatedAt: at(0), Tags: []Tag{TagCommented}, TimelineTruncated: true,
			Timeline: []Event{{Kind: EventComment, Actor: user("bob"), At: at(20)}},
		}, []Line{{Kind: Wait, Text: "1+ replies since yours"}}},
		{"truncated but never spoke", PR{
			Author: user("alice"), CreatedAt: at(0), Tags: []Tag{TagMentioned}, TimelineTruncated: true,
			Timeline: []Event{{Kind: EventComment, Actor: user("bob"), At: at(20)}},
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mySide(c.pr, p, teams, now); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}
