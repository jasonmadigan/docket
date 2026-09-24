// Package demo is a made-up snapshot for screenshots: every name in it is
// fictional.
package demo

import (
	"strconv"
	"time"

	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

const me = "jasonmadigan"

var Now = time.Date(2026, 9, 23, 9, 41, 0, 0, time.UTC)

const day = 24 * time.Hour

func ago(d time.Duration) time.Time { return Now.Add(-d) }

func user(login string) model.Actor { return model.Actor{Login: login} }

func url(repo, kind string, n int) string {
	return "https://github.com/" + repo + "/" + kind + "/" + strconv.Itoa(n)
}

func pr(repo string, n int, title, author string, created time.Duration, tags ...model.Tag) model.PR {
	return model.PR{
		Item: model.Item{
			ID: repo + "#" + strconv.Itoa(n), Repo: repo, Number: n, Title: title, URL: url(repo, "pull", n),
			Author: user(author), CreatedAt: ago(created), Tags: tags,
		},
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", Checks: model.Checks{State: "SUCCESS"},
	}
}

func commits(author string, at ...time.Duration) ([]model.Commit, string) {
	var cs []model.Commit
	for i, a := range at {
		cs = append(cs, model.Commit{OID: author + strconv.Itoa(i), At: ago(a), Author: user(author)})
	}
	return cs, cs[len(cs)-1].OID
}

func PRs() []model.PR {
	var prs []model.PR

	p := pr("acme/gateway", 1422, "Retry upstream connect on transient DNS failures", me, 3*day, model.TagAuthor)
	p.Commits, p.HeadOID = commits(me, 3*day, 26*time.Hour)
	p.CommitCount = 2
	p.MergeState, p.ReviewDecision = "CLEAN", "APPROVED"
	p.Opinions = []model.Review{{Author: user("alice"), State: "APPROVED", At: ago(2 * time.Hour)}}
	p.Timeline = []model.Event{{Kind: model.EventReview, Actor: user("alice"), At: ago(2 * time.Hour), State: "APPROVED"}}
	prs = append(prs, p)

	p = pr("acme/operator", 2310, "Reconcile policies when the gateway class changes", me, 6*day, model.TagAuthor)
	p.Commits, p.HeadOID = commits(me, 6*day, 2*day)
	p.CommitCount = 2
	p.ReviewDecision = "CHANGES_REQUESTED"
	p.Checks = model.Checks{State: "FAILURE", FailingCount: 3, Failing: []model.Check{
		{Name: "e2e-kind", URL: "https://github.com/acme/operator/actions/runs/1"},
		{Name: "lint", URL: "https://github.com/acme/operator/actions/runs/2"},
		{Name: "unit (1.26)", URL: "https://github.com/acme/operator/actions/runs/3"},
	}}
	p.Opinions = []model.Review{{Author: user("carol"), State: "CHANGES_REQUESTED", At: ago(26 * time.Hour)}}
	p.UnresolvedThreads = 3
	p.Issues = []model.IssueRef{{Repo: "acme/operator", Number: 2287, Title: "Gateway class changes are ignored", URL: url("acme/operator", "issues", 2287), State: "OPEN"}}
	p.Timeline = []model.Event{
		{Kind: model.EventReview, Actor: user("carol"), At: ago(26 * time.Hour), State: "CHANGES_REQUESTED"},
		{Kind: model.EventComment, Actor: user("bob"), At: ago(20 * time.Hour)},
		{Kind: model.EventComment, Actor: user("carol"), At: ago(5 * time.Hour)},
	}
	prs = append(prs, p)

	p = pr("acme/docs", 318, "Document rate limit response headers", me, 4*time.Hour, model.TagAuthor)
	p.Commits, p.HeadOID = commits(me, 4*time.Hour)
	p.CommitCount = 1
	p.Draft, p.MergeState = true, "DRAFT"
	p.Checks = model.Checks{State: "PENDING"}
	prs = append(prs, p)

	p = pr("acme/console", 871, "Add a topology view for route policies", "alice", 2*day, model.TagReview)
	p.Commits, p.HeadOID = commits("alice", 2*day, 3*time.Hour)
	p.CommitCount = 2
	p.ReviewDecision = "REVIEW_REQUIRED"
	p.Requests = []model.Reviewer{{Name: me}}
	p.Timeline = []model.Event{{Kind: model.EventReviewRequested, Actor: user("alice"), At: ago(3 * time.Hour), Target: me}}
	prs = append(prs, p)

	p = pr("acme/operator", 2298, "Bump controller-runtime to v0.22", "dependabot[bot]", 6*day, model.TagTeam)
	p.Author.Bot = true
	p.Commits, p.HeadOID = commits("dependabot[bot]", 6*day)
	p.Commits[0].Author.Bot = true
	p.CommitCount = 1
	p.Mergeable, p.MergeState, p.ReviewDecision = "CONFLICTING", "DIRTY", "REVIEW_REQUIRED"
	p.Requests = []model.Reviewer{{Name: "acme/maintainers", Team: true}}
	p.Timeline = []model.Event{
		{Kind: model.EventReviewRequested, Actor: model.Actor{Login: "dependabot", Bot: true}, At: ago(6 * day), Target: "acme/maintainers"},
		{Kind: model.EventComment, Actor: user("frank"), At: ago(5 * day)},
	}
	prs = append(prs, p)

	p = pr("acme/cli", 154, "Add --output yaml to status", "dave", 9*day, model.TagReview, model.TagReviewed)
	p.Commits, p.HeadOID = commits("dave", 9*day, 2*day, 26*time.Hour)
	p.CommitCount = 3
	p.ReviewDecision = "CHANGES_REQUESTED"
	p.Requests = []model.Reviewer{{Name: me}}
	p.Opinions = []model.Review{{Author: user(me), State: "CHANGES_REQUESTED", At: ago(3 * day)}}
	p.MyReview = &model.Review{Author: user(me), State: "CHANGES_REQUESTED", At: ago(3 * day), Commit: p.Commits[0].OID}
	p.Timeline = []model.Event{
		{Kind: model.EventReview, Actor: user(me), At: ago(3 * day), State: "CHANGES_REQUESTED"},
		{Kind: model.EventReviewRequested, Actor: user("dave"), At: ago(day), Target: me},
		{Kind: model.EventComment, Actor: user("dave"), At: ago(day)},
	}
	prs = append(prs, p)

	p = pr("acme/gateway", 1399, "Support weighted backends", "bob", 12*day, model.TagMentioned)
	p.Commits, p.HeadOID = commits("bob", 12*day, 3*day)
	p.CommitCount = 2
	p.ReviewDecision = "REVIEW_REQUIRED"
	p.Requests = []model.Reviewer{{Name: "erin"}}
	p.Timeline = []model.Event{
		{Kind: model.EventComment, Actor: user("bob"), At: ago(2 * day)},
		{Kind: model.EventMentioned, Actor: user(me), At: ago(2 * day)},
	}
	prs = append(prs, p)

	p = pr("acme/rfcs", 42, "RFC: policy attachment v2", "erin", 60*day, model.TagMentioned, model.TagCommented)
	p.Commits, p.HeadOID = commits("erin", 60*day)
	p.CommitCount = 1
	p.Checks, p.MergeState = model.Checks{}, "CLEAN"
	p.UnresolvedThreads = 12
	p.Timeline = []model.Event{
		{Kind: model.EventComment, Actor: user(me), At: ago(25 * day)},
		{Kind: model.EventComment, Actor: user("erin"), At: ago(21 * day)},
		{Kind: model.EventMentioned, Actor: user(me), At: ago(21 * day)},
		{Kind: model.EventComment, Actor: user("frank"), At: ago(20 * day)},
		{Kind: model.EventComment, Actor: user("alice"), At: ago(18 * day)},
	}
	prs = append(prs, p)

	p = pr("acme/console", 850, "Keyboard navigation for the policy list", "carol", 8*day, model.TagReviewed, model.TagCommented)
	p.Commits, p.HeadOID = commits("carol", 8*day, 5*day)
	p.CommitCount = 2
	p.ReviewDecision = "REVIEW_REQUIRED"
	p.Requests = []model.Reviewer{{Name: "bob"}}
	p.Opinions = []model.Review{{Author: user(me), State: "APPROVED", At: ago(4 * day)}}
	p.MyReview = &model.Review{Author: user(me), State: "APPROVED", At: ago(4 * day), Commit: p.HeadOID}
	p.Timeline = []model.Event{{Kind: model.EventReview, Actor: user(me), At: ago(4 * day), State: "APPROVED"}}
	prs = append(prs, p)

	p = pr("acme/operator", 2201, "Add metrics for reconcile latency", "frank", 20*day, model.TagCommented)
	p.Commits, p.HeadOID = commits("frank", 20*day, 7*day)
	p.CommitCount = 2
	p.MergeState, p.ReviewDecision = "BEHIND", "REVIEW_REQUIRED"
	p.Opinions = []model.Review{{Author: user("alice"), State: "APPROVED", At: ago(6 * day)}}
	p.Timeline = []model.Event{
		{Kind: model.EventComment, Actor: user(me), At: ago(10 * day)},
		{Kind: model.EventComment, Actor: user("frank"), At: ago(9 * day)},
		{Kind: model.EventComment, Actor: user("alice"), At: ago(8 * day)},
		{Kind: model.EventComment, Actor: user("frank"), At: ago(7 * day)},
		{Kind: model.EventReview, Actor: user("alice"), At: ago(6 * day), State: "APPROVED"},
		{Kind: model.EventComment, Actor: user("bob"), At: ago(6 * day)},
	}
	prs = append(prs, p)

	p = pr("acme/docs", 301, "Move the docs site to the new theme", "erin", 70*day, model.TagReviewed)
	p.Commits, p.HeadOID = commits("erin", 70*day, 49*day)
	p.CommitCount = 2
	p.ReviewDecision = "REVIEW_REQUIRED"
	p.MyReview = &model.Review{Author: user(me), State: "COMMENTED", At: ago(50 * day), Commit: p.Commits[0].OID}
	p.Timeline = []model.Event{{Kind: model.EventReview, Actor: user(me), At: ago(50 * day), State: "COMMENTED"}}
	prs = append(prs, p)

	return prs
}

// State is a loaded engine state over PRs, with two of them just changed.
func State() engine.State {
	return engine.State{
		Snapshot: model.Build(PRs(), nil, model.Params{Login: me, Teams: []string{"acme/maintainers"}, Now: Now}),
		Loaded:   true,
		Updated:  Now,
		Next:     Now.Add(time.Minute),
		Budget:   gh.Budget{Cost: 4, Remaining: 4731, Limit: 5000, ResetAt: Now.Add(38 * time.Minute)},
		Changed:  []string{"acme/operator#2310", "acme/console#871"},
	}
}
