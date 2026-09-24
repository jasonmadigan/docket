package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestIssueLeft(t *testing.T) {
	someone := []Actor{user("bob")}
	pr := func(n int, state string, draft bool) PRRef {
		return PRRef{Repo: "acme/a", Number: n, URL: fmt.Sprintf("https://github.com/acme/a/pull/%d", n), State: state, Draft: draft}
	}
	cases := []struct {
		name  string
		issue Issue
		want  []Line
	}{
		{"nothing left", Issue{Assignees: someone}, nil},
		{"unassigned", Issue{}, []Line{{Kind: Info, Text: "unassigned"}}},
		{"merged, then open, then drafts", Issue{Assignees: someone, PRs: []PRRef{
			pr(1, "OPEN", true), pr(2, "OPEN", false), pr(3, "MERGED", false)}},
			[]Line{
				{Kind: Good, Text: "PR acme/a#3 merged", URL: "https://github.com/acme/a/pull/3"},
				{Kind: Wait, Text: "PR acme/a#2 open", URL: "https://github.com/acme/a/pull/2"},
				{Kind: Info, Text: "PR acme/a#1 draft", URL: "https://github.com/acme/a/pull/1"},
			}},
		{"sub-issues under way", Issue{Assignees: someone, SubIssues: SubIssues{Total: 7, Completed: 3}},
			[]Line{{Kind: Wait, Text: "3 of 7 sub-issues completed"}}},
		{"one sub-issue", Issue{Assignees: someone, SubIssues: SubIssues{Total: 1}},
			[]Line{{Kind: Wait, Text: "0 of 1 sub-issue completed"}}},
		{"sub-issues all completed", Issue{Assignees: someone, SubIssues: SubIssues{Total: 7, Completed: 7}},
			[]Line{{Kind: Good, Text: "all 7 sub-issues completed"}}},
		{"everything, in order", Issue{PRs: []PRRef{pr(2, "OPEN", false)}, SubIssues: SubIssues{Total: 2, Completed: 1}},
			[]Line{
				{Kind: Wait, Text: "PR acme/a#2 open", URL: "https://github.com/acme/a/pull/2"},
				{Kind: Wait, Text: "1 of 2 sub-issues completed"},
				{Kind: Info, Text: "unassigned"},
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := issueLeft(c.issue); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestFixOf(t *testing.T) {
	cases := []struct {
		name string
		prs  []PRRef
		want Fix
	}{
		{"none", nil, FixNone},
		{"merged", []PRRef{{State: "MERGED"}}, FixMerged},
		{"open", []PRRef{{State: "OPEN"}}, FixOpen},
		{"a draft is open", []PRRef{{State: "OPEN", Draft: true}}, FixOpen},
		{"open outranks merged", []PRRef{{State: "MERGED"}, {State: "OPEN"}}, FixOpen},
	}
	for _, c := range cases {
		if got := fixOf(c.prs); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPRRefStatus(t *testing.T) {
	for want, r := range map[string]PRRef{
		"merged": {State: "MERGED"},
		"draft":  {State: "OPEN", Draft: true},
		"open":   {State: "OPEN"},
	} {
		if got := r.Status(); got != want {
			t.Errorf("%+v: got %q, want %q", r, got, want)
		}
	}
}

func TestIssueMine(t *testing.T) {
	p := newPeople("me", nil)
	now := at(72)
	cases := []struct {
		name  string
		issue Issue
		want  []Line
	}{
		{"assigned, the event in the window", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(0), Tags: []Tag{TagAssigned},
			Timeline: []Event{{Kind: EventAssigned, Actor: user("alice"), At: at(24), Target: "Me"}},
		}}, []Line{{Kind: Info, Text: "assigned to you 2d ago"}}},
		{"assigned before the window", Issue{Item: Item{Author: user("alice"), CreatedAt: at(0), Tags: []Tag{TagAssigned}}},
			[]Line{{Kind: Info, Text: "assigned to you"}}},
		{"someone else's assignment dates nothing", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(0), Tags: []Tag{TagAssigned},
			Timeline: []Event{{Kind: EventAssigned, Actor: user("alice"), At: at(24), Target: "bob"}},
		}}, []Line{{Kind: Info, Text: "assigned to you"}}},
		{"unanswered mention", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("bob"), At: at(71)},
				{Kind: EventMentioned, Actor: user("me"), At: at(71).Add(time.Second)},
			},
		}}, []Line{{Kind: Wait, Text: "mentioned by bob 59m ago"}}},
		{"mentioned when it was opened", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(24),
			Timeline: []Event{{Kind: EventMentioned, Actor: user("me"), At: at(24)}},
		}}, []Line{{Kind: Wait, Text: "mentioned by alice 2d ago"}}},
		{"answered mention", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventMentioned, Actor: user("me"), At: at(60)},
				{Kind: EventComment, Actor: user("me"), At: at(61)},
			},
		}}, nil},
		{"replies since my comment", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("me"), At: at(10)},
				{Kind: EventComment, Actor: user("bob"), At: at(20)},
				{Kind: EventComment, Actor: bot("renovate"), At: at(30)},
			},
		}}, []Line{{Kind: Wait, Text: "1 reply since yours"}}},
		{"replies on my issue", Issue{Item: Item{
			Author: user("me"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("bob"), At: at(5)},
				{Kind: EventComment, Actor: user("carol"), At: at(6)},
			},
		}}, []Line{{Kind: Wait, Text: "2 replies since yours"}}},
		{"replies beyond the window", Issue{Item: Item{
			Author: user("alice"), CreatedAt: at(0), Tags: []Tag{TagCommented}, TimelineTruncated: true,
			Timeline: []Event{{Kind: EventComment, Actor: user("bob"), At: at(20)}},
		}}, []Line{{Kind: Wait, Text: "1+ replies since yours"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := issueMine(c.issue, p, now); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestIssueActivity(t *testing.T) {
	p := newPeople("me", []string{"ci-robot"})
	cases := []struct {
		name  string
		issue Issue
		want  time.Time
	}{
		{"opening only", Issue{Item: Item{Author: user("alice"), CreatedAt: at(1)}}, at(1)},
		{"a comment", Issue{Item: Item{Author: user("alice"), CreatedAt: at(1),
			Timeline: []Event{{Kind: EventComment, Actor: user("bob"), At: at(3)}}}}, at(3)},
		{"assignment, reference and reopening", Issue{Item: Item{Author: user("alice"), CreatedAt: at(1),
			Timeline: []Event{
				{Kind: EventAssigned, Actor: user("alice"), At: at(2), Target: "me"},
				{Kind: EventReferenced, Actor: user("bob"), At: at(4)},
				{Kind: EventReopened, Actor: user("carol"), At: at(6)},
			}}}, at(6)},
		{"bots, ignored accounts and mentions don't count", Issue{Item: Item{Author: user("alice"), CreatedAt: at(1),
			Timeline: []Event{
				{Kind: EventComment, Actor: bot("renovate"), At: at(5)},
				{Kind: EventComment, Actor: user("ci-robot"), At: at(6)},
				{Kind: EventMentioned, Actor: user("me"), At: at(7)},
			}}}, at(1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := issueActivity(c.issue, p); !got.Equal(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
