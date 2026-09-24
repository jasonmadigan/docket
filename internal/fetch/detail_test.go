package fetch

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jasonmadigan/docket/internal/model"
)

func TestDetailBecomesModel(t *testing.T) {
	raw, err := os.ReadFile("testdata/detail.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"review": found("PR_1"), "requested": found("PR_1")})},
		{match: matchDetail, data: string(raw)},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PRs) != 1 {
		t.Fatalf("got %d PRs", len(res.PRs))
	}
	ts := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	want := model.PR{
		Item: model.Item{
			ID: "PR_1", Repo: "acme/widgets", Number: 42, Title: "Fix reconcile loop",
			URL:    "https://github.com/acme/widgets/pull/42",
			Author: model.Actor{Login: "alice"}, CreatedAt: ts("2026-09-01T09:00:00Z"),
			Tags: []model.Tag{model.TagReview},
			Timeline: []model.Event{
				{Kind: model.EventReviewRequested, Actor: model.Actor{Login: "alice"}, At: ts("2026-09-01T09:05:00Z"), Target: "me"},
				{Kind: model.EventComment, Actor: model.Actor{Login: "coderabbitai", Bot: true}, At: ts("2026-09-01T09:10:00Z")},
				{Kind: model.EventReview, Actor: model.Actor{Login: "me"}, At: ts("2026-09-02T11:00:00Z"), State: "COMMENTED"},
				{Kind: model.EventForcePush, Actor: model.Actor{Login: "alice"}, At: ts("2026-09-03T09:30:00Z")},
				{Kind: model.EventMentioned, Actor: model.Actor{Login: "me"}, At: ts("2026-09-03T10:00:00Z")},
				{Kind: model.EventComment, At: ts("2026-09-03T10:00:00Z")},
			},
			TimelineTruncated: true,
		},
		Mergeable: "MERGEABLE", MergeState: "BLOCKED", ReviewDecision: "REVIEW_REQUIRED", HeadOID: "c3",
		Checks: model.Checks{State: "FAILURE", FailingCount: 3, Failing: []model.Check{
			{Name: "e2e", URL: "https://ci.example/e2e"},
			{Name: "ci/prow", URL: "https://prow.example/1"},
		}},
		Requests: []model.Reviewer{{Name: "me"}, {Name: "acme/devs", Team: true}},
		Opinions: []model.Review{
			{Author: model.Actor{Login: "carol"}, State: "APPROVED", At: ts("2026-09-02T10:00:00Z"), Commit: "c2"},
		},
		MyReview:          &model.Review{Author: model.Actor{Login: "me"}, State: "COMMENTED", At: ts("2026-09-02T11:00:00Z"), Commit: "c2"},
		UnresolvedThreads: 2,
		CommitCount:       3,
		Commits: []model.Commit{
			{OID: "c1", At: ts("2026-09-01T09:00:00Z"), Author: model.Actor{Login: "alice"}},
			{OID: "c2", At: ts("2026-09-02T09:00:00Z"), Author: model.Actor{Login: "dependabot[bot]", Bot: true}},
			{OID: "c3", At: ts("2026-09-03T09:00:00Z"), Author: model.Actor{Login: "Alice"}},
		},
		Issues: []model.IssueRef{
			{Repo: "acme/widgets", Number: 7, Title: "Loop never ends", URL: "https://github.com/acme/widgets/issues/7", State: "OPEN"},
		},
	}
	if got := res.PRs[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestMyReviewSkipsPendingDrafts(t *testing.T) {
	if !strings.Contains(detailQuery, "reviews(last: 1, author: $login, states: [APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED])") {
		t.Fatal("myReviews can return a pending draft, which hides the submitted review")
	}
}
