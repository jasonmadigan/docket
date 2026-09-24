package model

import (
	"fmt"
	"time"
)

type Tag string

const (
	TagAuthor    Tag = "author"
	TagReview    Tag = "review"
	TagTeam      Tag = "team"
	TagAssigned  Tag = "assigned"
	TagMentioned Tag = "mentioned"
	TagReviewed  Tag = "reviewed"
	TagCommented Tag = "commented"
)

type Actor struct {
	Login string `json:"login"`
	Bot   bool   `json:"bot,omitempty"` // github app, or a login or commit name ending in [bot]
}

type EventKind string

const (
	EventComment         EventKind = "comment"
	EventReview          EventKind = "review"
	EventForcePush       EventKind = "force_push"
	EventReviewRequested EventKind = "review_requested"
	EventMentioned       EventKind = "mentioned"  // actor is the person mentioned, not the author
	EventAssigned        EventKind = "assigned"   // target is the assignee
	EventReferenced      EventKind = "referenced" // another issue or pr mentioned this one
	EventReopened        EventKind = "reopened"
)

type Event struct {
	Kind   EventKind `json:"kind"`
	Actor  Actor     `json:"actor"`
	At     time.Time `json:"at"`
	State  string    `json:"state,omitempty"`  // reviews
	Target string    `json:"target,omitempty"` // review requests: login or org/team
}

type Reviewer struct {
	Name string `json:"name"` // login, or org/team
	Team bool   `json:"team,omitempty"`
}

type Review struct {
	Author Actor     `json:"author"`
	State  string    `json:"state"`
	At     time.Time `json:"at"`
	Commit string    `json:"commit,omitempty"`
}

type Commit struct {
	OID    string    `json:"oid"`
	At     time.Time `json:"at"`
	Author Actor     `json:"author"`
}

type Check struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type Checks struct {
	State        string  `json:"state,omitempty"` // head commit rollup; empty with no checks
	Failing      []Check `json:"failing,omitempty"`
	FailingCount int     `json:"failingCount,omitempty"` // exact, even past the first page
}

// IssueRef is an issue a pr says it closes.
type IssueRef struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

// Item is what pull requests and issues share.
type Item struct {
	ID                string    `json:"id"`
	Repo              string    `json:"repo"`
	Number            int       `json:"number"`
	Title             string    `json:"title"`
	URL               string    `json:"url"`
	Author            Actor     `json:"author"`
	CreatedAt         time.Time `json:"createdAt"`
	Tags              []Tag     `json:"tags"`
	Timeline          []Event   `json:"-"` // last 100, oldest first
	TimelineTruncated bool      `json:"-"`
}

func (it Item) Ref() string {
	return fmt.Sprintf("%s#%d", it.Repo, it.Number)
}

type PR struct {
	Item
	Draft             bool       `json:"draft,omitempty"`
	Mergeable         string     `json:"mergeable"`
	MergeState        string     `json:"mergeState"`
	ReviewDecision    string     `json:"reviewDecision,omitempty"`
	HeadOID           string     `json:"headOid"`
	Checks            Checks     `json:"checks"`
	Requests          []Reviewer `json:"requests,omitempty"`
	Opinions          []Review   `json:"opinions,omitempty"` // latest approval or change request per reviewer
	MyReview          *Review    `json:"myReview,omitempty"`
	UnresolvedThreads int        `json:"unresolvedThreads,omitempty"`
	MoreThreads       bool       `json:"moreThreads,omitempty"` // over 100 threads; the count is a floor
	CommitCount       int        `json:"commitCount"`
	Commits           []Commit   `json:"-"` // last 100, oldest first
	Issues            []IssueRef `json:"issues,omitempty"`
}

// PRRef is a pull request that says it closes an issue.
type PRRef struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"` // OPEN or MERGED; ones closed unmerged are dropped
	Draft  bool   `json:"draft,omitempty"`
}

func (r PRRef) Ref() string {
	return fmt.Sprintf("%s#%d", r.Repo, r.Number)
}

// Status is merged, draft or open.
func (r PRRef) Status() string {
	switch {
	case r.State == "MERGED":
		return "merged"
	case r.Draft:
		return "draft"
	}
	return "open"
}

type SubIssues struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}

type Issue struct {
	Item
	Assignees []Actor   `json:"assignees,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	SubIssues SubIssues `json:"subIssues,omitzero"`
	PRs       []PRRef   `json:"prs,omitempty"`
}
