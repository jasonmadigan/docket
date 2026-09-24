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
		{"opening only", PR{Item: Item{Author: user("alice"), CreatedAt: at(1)}}, at(1)},
		{"bots and ignored accounts don't count", PR{
			Item: Item{
				Author: user("alice"), CreatedAt: at(0),
				Timeline: []Event{
					{Kind: EventComment, Actor: user("bob"), At: at(2)},
					{Kind: EventReviewRequested, Actor: user("alice"), At: at(3), Target: "me"},
					{Kind: EventComment, Actor: bot("coderabbitai"), At: at(10)},
					{Kind: EventComment, Actor: user("ci-robot"), At: at(11)},
					{Kind: EventMentioned, Actor: user("me"), At: at(12)},
				},
			},
			Commits: []Commit{
				{OID: "c1", At: at(1), Author: user("alice")},
				{OID: "c2", At: at(9), Author: bot("dependabot[bot]")},
			},
		}, at(3)},
		{"force push", PR{Item: Item{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{{Kind: EventForcePush, Actor: user("alice"), At: at(4)}},
		}}, at(4)},
		{"reopening", PR{Item: Item{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{{Kind: EventReopened, Actor: user("bob"), At: at(6)}},
		}}, at(6)},
		{"only bots", PR{
			Item:    Item{Author: bot("renovate"), CreatedAt: at(0)},
			Commits: []Commit{{OID: "c1", At: at(1), Author: bot("renovate")}},
		}, time.Time{}},
		{"deleted account", PR{Item: Item{
			Author: Actor{}, CreatedAt: at(0),
			Timeline: []Event{{Kind: EventComment, At: at(5)}},
		}}, time.Time{}},
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
		Item: Item{
			Author: user("alice"), CreatedAt: at(0),
			Timeline: []Event{
				{Kind: EventComment, Actor: user("me"), At: at(5)},
				{Kind: EventForcePush, Actor: user("me"), At: at(7)},
				{Kind: EventReviewRequested, Actor: user("me"), At: at(8), Target: "bob"},
				{Kind: EventMentioned, Actor: user("me"), At: at(9)},
				{Kind: EventComment, Actor: user("bob"), At: at(10)},
			},
		},
		Commits: []Commit{{OID: "c1", At: at(3), Author: user("me")}},
	}
	if got := myLastActivity(pr, p); !got.Equal(at(7)) {
		t.Fatalf("got %v, want %v", got, at(7))
	}
}
