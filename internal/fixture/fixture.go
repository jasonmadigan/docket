package fixture

import (
	"errors"
	"time"

	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

var Now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func ago(d time.Duration) time.Time { return Now.Add(-d) }

func user(login string) model.Actor { return model.Actor{Login: login} }

// PRs covers every section and most of what's left.
func PRs() []model.PR {
	return []model.PR{
		{
			Item: model.Item{
				ID: "PR_1", Repo: "acme/widgets", Number: 42, Title: "Fix reconcile loop when the gateway disappears",
				URL: "https://github.com/acme/widgets/pull/42", Author: user("me"), CreatedAt: ago(5 * day),
				Tags: []model.Tag{model.TagAuthor},
				Timeline: []model.Event{
					{Kind: model.EventReview, Actor: user("carol"), At: ago(2 * day), State: "CHANGES_REQUESTED"},
				},
			},
			Mergeable: "MERGEABLE", MergeState: "BLOCKED", ReviewDecision: "CHANGES_REQUESTED", HeadOID: "c2",
			Checks: model.Checks{State: "FAILURE", FailingCount: 1,
				Failing: []model.Check{{Name: "e2e", URL: "https://ci.example/e2e"}}},
			Opinions:          []model.Review{{Author: user("carol"), State: "CHANGES_REQUESTED", At: ago(2 * day)}},
			UnresolvedThreads: 2,
			CommitCount:       2,
			Commits: []model.Commit{
				{OID: "c1", At: ago(5 * day), Author: user("me")},
				{OID: "c2", At: ago(3 * day), Author: user("me")},
			},
			Issues: []model.IssueRef{{Repo: "acme/widgets", Number: 7, Title: "Loop never ends",
				URL: "https://github.com/acme/widgets/issues/7", State: "OPEN"}},
		},
		{
			Item: model.Item{
				ID: "PR_2", Repo: "acme/widgets", Number: 51, Title: "Add rate limit docs",
				URL: "https://github.com/acme/widgets/pull/51", Author: user("me"), CreatedAt: ago(40 * day),
				Tags: []model.Tag{model.TagAuthor},
				Timeline: []model.Event{
					{Kind: model.EventReview, Actor: user("dave"), At: ago(39 * day), State: "APPROVED"},
				},
			},
			Mergeable: "MERGEABLE", MergeState: "CLEAN", ReviewDecision: "APPROVED", HeadOID: "d1",
			Checks:      model.Checks{State: "SUCCESS"},
			Opinions:    []model.Review{{Author: user("dave"), State: "APPROVED", At: ago(39 * day)}},
			CommitCount: 1,
			Commits:     []model.Commit{{OID: "d1", At: ago(40 * day), Author: user("me")}},
		},
		{
			Item: model.Item{
				ID: "PR_3", Repo: "acme/gateway", Number: 1203, Title: "Support weighted backends",
				URL: "https://github.com/acme/gateway/pull/1203", Author: user("alice"), CreatedAt: ago(3 * day),
				Tags: []model.Tag{model.TagReview, model.TagCommented},
				Timeline: []model.Event{
					{Kind: model.EventReviewRequested, Actor: user("alice"), At: ago(3 * day), Target: "me"},
					{Kind: model.EventReview, Actor: user("me"), At: ago(2 * day), State: "COMMENTED"},
					{Kind: model.EventComment, Actor: user("alice"), At: ago(day)},
					{Kind: model.EventReviewRequested, Actor: user("alice"), At: ago(day), Target: "me"},
				},
			},
			Mergeable: "MERGEABLE", MergeState: "BLOCKED", ReviewDecision: "REVIEW_REQUIRED", HeadOID: "e3",
			Checks:      model.Checks{State: "PENDING"},
			Requests:    []model.Reviewer{{Name: "me"}},
			MyReview:    &model.Review{Author: user("me"), State: "COMMENTED", At: ago(2 * day), Commit: "e1"},
			CommitCount: 3,
			Commits: []model.Commit{
				{OID: "e1", At: ago(3 * day), Author: user("alice")},
				{OID: "e2", At: ago(26 * time.Hour), Author: user("alice")},
				{OID: "e3", At: ago(25 * time.Hour), Author: user("alice")},
			},
		},
		{
			Item: model.Item{
				ID: "PR_4", Repo: "acme/gateway", Number: 1188, Title: "Bump Envoy to 1.37",
				URL: "https://github.com/acme/gateway/pull/1188", Author: user("bob"), CreatedAt: ago(9 * day),
				Tags: []model.Tag{model.TagTeam},
				Timeline: []model.Event{
					{Kind: model.EventReviewRequested, Actor: user("bob"), At: ago(9 * day), Target: "acme/devs"},
				},
			},
			Mergeable: "CONFLICTING", MergeState: "DIRTY", ReviewDecision: "REVIEW_REQUIRED", HeadOID: "f1",
			Checks:      model.Checks{State: "SUCCESS"},
			Requests:    []model.Reviewer{{Name: "acme/devs", Team: true}},
			CommitCount: 1,
			Commits:     []model.Commit{{OID: "f1", At: ago(9 * day), Author: user("bob")}},
		},
		{
			Item: model.Item{
				ID: "PR_5", Repo: "acme/docs", Number: 88, Title: "Rewrite the getting started guide",
				URL: "https://github.com/acme/docs/pull/88", Author: user("erin"), CreatedAt: ago(800 * day),
				Tags: []model.Tag{model.TagMentioned},
				Timeline: []model.Event{
					{Kind: model.EventComment, Actor: user("erin"), At: ago(700 * day)},
					{Kind: model.EventMentioned, Actor: user("me"), At: ago(700 * day)},
				},
			},
			Mergeable: "MERGEABLE", MergeState: "CLEAN", HeadOID: "g1",
			CommitCount: 1,
			Commits:     []model.Commit{{OID: "g1", At: ago(800 * day), Author: user("erin")}},
		},
	}
}

// Issues fills each issue section, with linked PRs and sub-issues.
func Issues() []model.Issue {
	return []model.Issue{
		{
			Item: model.Item{
				ID: "I_1", Repo: "acme/widgets", Number: 7, Title: "Loop never ends",
				URL: "https://github.com/acme/widgets/issues/7", Author: user("alice"), CreatedAt: ago(8 * day),
				Tags: []model.Tag{model.TagAssigned},
				Timeline: []model.Event{
					{Kind: model.EventAssigned, Actor: user("alice"), At: ago(6 * day), Target: "me"},
					{Kind: model.EventReferenced, Actor: user("me"), At: ago(5 * day)},
				},
			},
			Assignees: []model.Actor{user("me")},
			Labels:    []string{"bug"},
			PRs: []model.PRRef{{Repo: "acme/widgets", Number: 42, Title: "Fix reconcile loop when the gateway disappears",
				URL: "https://github.com/acme/widgets/pull/42", State: "OPEN"}},
		},
		{
			Item: model.Item{
				ID: "I_2", Repo: "acme/gateway", Number: 1180, Title: "Document weighted backends",
				URL: "https://github.com/acme/gateway/issues/1180", Author: user("me"), CreatedAt: ago(12 * day),
				Tags:     []model.Tag{model.TagAuthor},
				Timeline: []model.Event{{Kind: model.EventComment, Actor: user("bob"), At: ago(10 * day)}},
			},
			Labels:    []string{"docs"},
			SubIssues: model.SubIssues{Total: 3, Completed: 1},
		},
		{
			Item: model.Item{
				ID: "I_3", Repo: "acme/docs", Number: 90, Title: "Broken link on the install page",
				URL: "https://github.com/acme/docs/issues/90", Author: user("erin"), CreatedAt: ago(4 * day),
				Tags: []model.Tag{model.TagMentioned, model.TagCommented},
				Timeline: []model.Event{
					{Kind: model.EventComment, Actor: user("me"), At: ago(3 * day)},
					{Kind: model.EventComment, Actor: user("erin"), At: ago(day)},
					{Kind: model.EventMentioned, Actor: user("me"), At: ago(day)},
				},
			},
			Assignees: []model.Actor{user("erin")},
			PRs: []model.PRRef{{Repo: "acme/docs", Number: 91, Title: "Fix the install link",
				URL: "https://github.com/acme/docs/pull/91", State: "MERGED"}},
		},
	}
}

func State() engine.State {
	return engine.State{
		Snapshot: model.Build(PRs(), Issues(), model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: Now}),
		Loaded:   true,
		Updated:  Now,
		Next:     Now.Add(time.Minute),
		Budget:   gh.Budget{Cost: 4, Remaining: 4812, Limit: 5000, ResetAt: Now.Add(40 * time.Minute)},
	}
}

// Failed is State after a later poll failed.
func Failed() engine.State {
	s := State()
	s.Err = errors.New(`Post "https://api.github.com/graphql": dial tcp: lookup api.github.com: no such host`)
	s.Next = Now.Add(2 * time.Minute)
	s.Warnings = []string{"Resource protected by organization SAML enforcement."}
	return s
}
