package model

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type LineKind string

const (
	Bad  LineKind = "bad"
	Wait LineKind = "wait"
	Good LineKind = "good"
	Info LineKind = "info"
)

type Line struct {
	Kind LineKind `json:"kind"`
	Text string   `json:"text"`
	URL  string   `json:"url,omitempty"`
}

// github's mention event names only the person mentioned; the comment,
// review or pr that made it lands within this of the event.
const mentionSlack = 5 * time.Second

func whatsLeft(pr PR) []Line {
	var lines []Line
	add := func(kind LineKind, text, url string) {
		lines = append(lines, Line{Kind: kind, Text: text, URL: url})
	}
	if pr.Draft {
		add(Info, "draft", "")
	}
	if pr.Mergeable == "CONFLICTING" {
		add(Bad, "conflicts", "")
	}
	if pr.MergeState == "BEHIND" {
		add(Bad, "behind base", "")
	}
	switch pr.Checks.State {
	case "FAILURE", "ERROR":
		add(Bad, failing(pr.Checks), firstURL(pr.Checks.Failing))
	case "PENDING", "EXPECTED":
		add(Wait, "CI running", "")
	case "":
		add(Info, "no CI", "")
	}
	switch pr.ReviewDecision {
	case "CHANGES_REQUESTED":
		add(Bad, changesRequested(pr.Opinions), "")
	case "REVIEW_REQUIRED":
		switch {
		case len(pr.Requests) > 0:
			add(Wait, "awaiting "+names(pr.Requests), "")
		case slices.ContainsFunc(pr.Opinions, func(r Review) bool { return r.State == "APPROVED" }):
			add(Wait, "needs more approvals", "")
		default:
			add(Bad, "no reviewer", "")
		}
	}
	if pr.UnresolvedThreads > 0 {
		add(Wait, threads(pr), "")
	}
	blocked := slices.ContainsFunc(lines, func(l Line) bool { return l.Kind == Bad || l.Kind == Wait })
	if !blocked && (pr.MergeState == "CLEAN" || pr.MergeState == "HAS_HOOKS") {
		add(Good, "ready to merge", "")
	}
	return lines
}

func failing(c Checks) string {
	if len(c.Failing) == 0 {
		return "CI failing"
	}
	shown := c.Failing[:min(3, len(c.Failing))]
	checkNames := make([]string, len(shown))
	for i, check := range shown {
		checkNames[i] = check.Name
	}
	text := "CI failing: " + strings.Join(checkNames, ", ")
	if extra := max(c.FailingCount, len(c.Failing)) - len(shown); extra > 0 {
		text += fmt.Sprintf(" +%d", extra)
	}
	return text
}

func firstURL(checks []Check) string {
	for _, c := range checks {
		if c.URL != "" {
			return c.URL
		}
	}
	return ""
}

func changesRequested(opinions []Review) string {
	var who []string
	for _, r := range opinions {
		if r.State == "CHANGES_REQUESTED" {
			who = append(who, displayName(r.Author))
		}
	}
	if len(who) == 0 {
		return "changes requested"
	}
	return "changes requested by " + strings.Join(who, ", ")
}

// displayName calls deleted accounts ghost, as github does.
func displayName(a Actor) string {
	if a.Login == "" {
		return "ghost"
	}
	return a.Login
}

func names(rs []Reviewer) string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return strings.Join(out, ", ")
}

func threads(pr PR) string {
	if pr.MoreThreads {
		return fmt.Sprintf("%d+ unresolved threads", pr.UnresolvedThreads)
	}
	return fmt.Sprintf("%d unresolved %s", pr.UnresolvedThreads, Plural(pr.UnresolvedThreads, "thread", "threads"))
}

func Plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func mySide(pr PR, p people, teams map[string]bool, now time.Time) []Line {
	var lines []Line
	add := func(kind LineKind, text string) {
		lines = append(lines, Line{Kind: kind, Text: text})
	}
	if !p.isMe(pr.Author) {
		for _, r := range pr.Requests {
			switch {
			case !r.Team && p.isMe(Actor{Login: r.Name}):
				add(Wait, "review requested"+ago(requestedAt(pr, r.Name), now))
			case r.Team && teams[strings.ToLower(r.Name)]:
				add(Wait, "review requested from "+r.Name+ago(requestedAt(pr, r.Name), now))
			}
		}
		if pr.MyReview != nil {
			add(myReview(pr, now))
		}
	}
	if line, ok := mentionLine(pr.Item, myLastActivity(pr, p), p, now); ok {
		lines = append(lines, line)
	}
	if text := replies(pr.Item, p); text != "" {
		add(Wait, text)
	}
	return lines
}

func ago(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	return " " + Age(now.Sub(at)) + " ago"
}

// requestedAt is zero once the request has scrolled out of the timeline
// window.
func requestedAt(pr PR, target string) time.Time {
	var at time.Time
	for _, e := range pr.Timeline {
		if e.Kind == EventReviewRequested && strings.EqualFold(e.Target, target) && e.At.After(at) {
			at = e.At
		}
	}
	return at
}

func verb(state string) string {
	switch state {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "requested changes"
	}
	return "commented"
}

func myReview(pr PR, now time.Time) (LineKind, string) {
	r := pr.MyReview
	if r.State == "DISMISSED" {
		return Wait, "your review was dismissed" + ago(r.At, now)
	}
	did := "you " + verb(r.State)
	if r.Commit == "" || r.Commit == pr.HeadOID {
		return Info, did + ago(r.At, now)
	}
	for i, c := range pr.Commits {
		if c.OID != r.Commit {
			continue
		}
		n := len(pr.Commits) - 1 - i
		if n == 0 {
			return Info, did + ago(r.At, now)
		}
		return Wait, fmt.Sprintf("%s, %d %s since", did, n, Plural(n, "commit", "commits"))
	}
	if pr.CommitCount > len(pr.Commits) {
		return Wait, fmt.Sprintf("%s, %d+ commits since", did, len(pr.Commits))
	}
	return Wait, "rewritten since your review"
}

// mentionLine names who mentioned me and when, while nothing from me, my
// last word being at mine, has followed.
func mentionLine(it Item, mine time.Time, p people, now time.Time) (Line, bool) {
	at, ok := unansweredMention(it, mine, p)
	if !ok {
		return Line{}, false
	}
	text := "mentioned"
	if by := mentioner(it, p, at); by != "" {
		text += " by " + by
	}
	return Line{Kind: Wait, Text: text + ago(at, now)}, true
}

func unansweredMention(it Item, mine time.Time, p people) (time.Time, bool) {
	var at time.Time
	for _, e := range it.Timeline {
		if e.Kind == EventMentioned && p.isMe(e.Actor) && e.At.After(at) {
			at = e.At
		}
	}
	if at.IsZero() || !mine.Before(at) {
		return time.Time{}, false
	}
	return at, true
}

func mentioner(it Item, p people, at time.Time) string {
	best, gap := "", mentionSlack+1
	see := func(a Actor, t time.Time) {
		d := t.Sub(at).Abs()
		if a.Login != "" && !p.isMe(a) && d <= mentionSlack && d < gap {
			best, gap = a.Login, d
		}
	}
	see(it.Author, it.CreatedAt)
	for _, e := range it.Timeline {
		if e.Kind == EventComment || e.Kind == EventReview {
			see(e.Actor, e.At)
		}
	}
	return best
}

// replies counts people's comments and reviews since my last one, or since
// I opened it. Approvals and dismissals aren't replies. When my last word
// is older than the timeline window the count is a floor.
func replies(it Item, p people) string {
	spoke := func(e Event) bool { return e.Kind == EventComment || e.Kind == EventReview }
	replied := func(e Event) bool {
		return e.Kind == EventComment || (e.Kind == EventReview && e.State != "APPROVED" && e.State != "DISMISSED")
	}
	var mine time.Time
	inWindow := false
	for _, e := range it.Timeline {
		if spoke(e) && p.isMe(e.Actor) && e.At.After(mine) {
			mine, inWindow = e.At, true
		}
	}
	if !inWindow {
		earlier := slices.Contains(it.Tags, TagCommented) || slices.Contains(it.Tags, TagReviewed)
		switch {
		case p.isMe(it.Author):
			mine = it.CreatedAt
		case !it.TimelineTruncated || !earlier:
			return ""
		}
	}
	n := 0
	for _, e := range it.Timeline {
		if replied(e) && !p.isMe(e.Actor) && p.human(e.Actor) && e.At.After(mine) {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	if !inWindow && it.TimelineTruncated {
		return fmt.Sprintf("%d+ replies since yours", n)
	}
	return fmt.Sprintf("%d %s since yours", n, Plural(n, "reply", "replies"))
}
