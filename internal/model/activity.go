package model

import (
	"slices"
	"strings"
	"time"
)

type people struct {
	me     string
	ignore map[string]bool
}

func newPeople(me string, ignore []string) people {
	p := people{me: strings.ToLower(me), ignore: map[string]bool{}}
	for _, login := range ignore {
		p.ignore[strings.ToLower(login)] = true
	}
	return p
}

func (p people) isMe(a Actor) bool {
	return a.Login != "" && strings.ToLower(a.Login) == p.me
}

// human is false for deleted accounts, apps and ignored logins.
func (p people) human(a Actor) bool {
	return a.Login != "" && !a.Bot && !p.ignore[strings.ToLower(a.Login)]
}

// latest is the newest opening, commit or event of kinds on it by an actor
// that who accepts.
func latest(it Item, commits []Commit, who func(Actor) bool, kinds ...EventKind) time.Time {
	var last time.Time
	see := func(a Actor, at time.Time) {
		if who(a) && at.After(last) {
			last = at
		}
	}
	see(it.Author, it.CreatedAt)
	for _, c := range commits {
		see(c.Author, c.At)
	}
	for _, e := range it.Timeline {
		if slices.Contains(kinds, e.Kind) {
			see(e.Actor, e.At)
		}
	}
	return last
}

func lastHumanActivity(pr PR, p people) time.Time {
	return latest(pr.Item, pr.Commits, p.human, EventComment, EventReview, EventForcePush, EventReviewRequested)
}

func myLastActivity(pr PR, p people) time.Time {
	return latest(pr.Item, pr.Commits, p.isMe, EventComment, EventReview, EventForcePush)
}
