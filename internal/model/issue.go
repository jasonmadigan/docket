package model

import (
	"fmt"
	"slices"
	"time"
)

type Fix string

const (
	FixMerged Fix = "merged"
	FixOpen   Fix = "open"
	FixNone   Fix = "none"
)

// fixOf is open while any linked pr is, merged once one has merged and
// none is open.
func fixOf(prs []PRRef) Fix {
	fix := FixNone
	for _, pr := range prs {
		switch pr.State {
		case "OPEN":
			return FixOpen
		case "MERGED":
			fix = FixMerged
		}
	}
	return fix
}

// issueLeft is what stands before an issue can close.
func issueLeft(is Issue) []Line {
	var lines []Line
	add := func(kind LineKind, text, url string) {
		lines = append(lines, Line{Kind: kind, Text: text, URL: url})
	}
	for _, want := range []struct {
		status string
		kind   LineKind
	}{{"merged", Good}, {"open", Wait}, {"draft", Info}} {
		for _, pr := range is.PRs {
			if pr.Status() == want.status {
				add(want.kind, "PR "+pr.Ref()+" "+want.status, pr.URL)
			}
		}
	}
	if s := is.SubIssues; s.Total > 0 {
		noun := Plural(s.Total, "sub-issue", "sub-issues")
		if s.Completed >= s.Total {
			add(Good, fmt.Sprintf("all %d %s completed", s.Total, noun), "")
		} else {
			add(Wait, fmt.Sprintf("%d of %d %s completed", s.Completed, s.Total, noun), "")
		}
	}
	if len(is.Assignees) == 0 {
		add(Info, "unassigned", "")
	}
	return lines
}

// issueMine is my side of an issue: assigned, mentioned, answered.
func issueMine(is Issue, p people, now time.Time) []Line {
	var lines []Line
	if slices.Contains(is.Tags, TagAssigned) {
		lines = append(lines, Line{Kind: Info, Text: "assigned to you" + ago(assignedAt(is.Item, p), now)})
	}
	if line, ok := mentionLine(is.Item, myLastWord(is, p), p, now); ok {
		lines = append(lines, line)
	}
	if text := replies(is.Item, p); text != "" {
		lines = append(lines, Line{Kind: Wait, Text: text})
	}
	return lines
}

// assignedAt is zero once the assignment has scrolled out of the timeline
// window.
func assignedAt(it Item, p people) time.Time {
	var at time.Time
	for _, e := range it.Timeline {
		if e.Kind == EventAssigned && p.isMe(Actor{Login: e.Target}) && e.At.After(at) {
			at = e.At
		}
	}
	return at
}

func issueActivity(is Issue, p people) time.Time {
	return latest(is.Item, nil, p.human, EventComment, EventAssigned, EventReferenced, EventReopened)
}

// myLastWord is my latest comment, or opening the issue when it's mine.
func myLastWord(is Issue, p people) time.Time {
	return latest(is.Item, nil, p.isMe, EventComment)
}
