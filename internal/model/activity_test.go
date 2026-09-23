package model

import (
	"testing"
	"time"
)

func TestLastHumanActivity(t *testing.T) {
	p := newPeople("me", []string{"CI-Robot"})
	cases := []struct {
		name string
		pr   PR
		want time.Time
	}{
		{"opening only", PR{Author: user("alice"), CreatedAt: at(1)}, at(1)},
		{"bots and ignored accounts don't count", PR{
			Author: user("alice"), CreatedAt: at(0),
			Commits: []Commit{
				{OID: "c1", At: at(1), Author: user("alice")},
				{OID: "c2", At: at(9), Author: bot("dependabot[bot]")},
			},
			Timeline: []Event{
				{Kind: EventComment, Actor: user("bob"), At: at(2)},
				{Kind: EventReviewRequested, Actor: user("alice"), At: at(3), Target: "me"},
				{Kind: EventComment, Actor: bot("coderabbitai"), At: at(10)},
				{Kind: EventComment, Actor: user("ci-robot"), At: at(11)},
				{Kind: EventMentioned, Actor: user("me"), At: at(12)},
			},
		}, at(3)},
		{"force push", PR{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{{Kind: EventForcePush, Actor: user("alice"), At: at(4)}},
		}, at(4)},
		{"only bots", PR{
			Author: bot("renovate"), CreatedAt: at(0),
			Commits: []Commit{{OID: "c1", At: at(1), Author: bot("renovate")}},
		}, time.Time{}},
		{"deleted account", PR{
			Author: Actor{}, CreatedAt: at(0),
			Timeline: []Event{{Kind: EventComment, At: at(5)}},
		}, time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lastHumanActivity(c.pr, p); !got.Equal(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestMyLastActivity(t *testing.T) {
	p := newPeople("Me", nil)
	pr := PR{
		Author: user("alice"), CreatedAt: at(0),
		Commits: []Commit{{OID: "c1", At: at(3), Author: user("me")}},
		Timeline: []Event{
			{Kind: EventComment, Actor: user("me"), At: at(5)},
			{Kind: EventForcePush, Actor: user("me"), At: at(7)},
			{Kind: EventReviewRequested, Actor: user("me"), At: at(8), Target: "bob"},
			{Kind: EventMentioned, Actor: user("me"), At: at(9)},
			{Kind: EventComment, Actor: user("bob"), At: at(10)},
		},
	}
	if got := myLastActivity(pr, p); !got.Equal(at(7)) {
		t.Fatalf("got %v, want %v", got, at(7))
	}
}
