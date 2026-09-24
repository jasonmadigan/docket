package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

type reply struct {
	match string // the query must contain this
	data  string
	err   error
}

type call struct {
	query string
	vars  map[string]any
}

type fake struct {
	t       *testing.T
	replies []reply
	calls   []call
}

func (f *fake) Do(_ context.Context, query string, vars map[string]any, out any) error {
	f.t.Helper()
	f.calls = append(f.calls, call{query, vars})
	if len(f.replies) == 0 {
		f.t.Fatalf("unexpected query:\n%s", query)
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	if !strings.Contains(query, r.match) {
		f.t.Fatalf("query does not contain %q:\n%s", r.match, query)
	}
	if r.data != "" {
		if err := json.Unmarshal([]byte(r.data), out); err != nil {
			f.t.Fatalf("decode reply: %v", err)
		}
	}
	return r.err
}

func (f *fake) finished() {
	f.t.Helper()
	if len(f.replies) > 0 {
		f.t.Fatalf("%d replies unused", len(f.replies))
	}
}

const (
	matchDiscovery = "author: search("
	matchPage      = "search(query: $q"
	matchDetail    = "headRefOid"
	matchIssues    = "subIssuesSummary"
	budget         = `"rateLimit": {"cost": 1, "remaining": 4990, "limit": 5000, "resetAt": "2026-09-23T13:00:00Z"}`
	detailBudget   = `"rateLimit": {"cost": 2, "remaining": 4988, "limit": 5000, "resetAt": "2026-09-23T13:00:00Z"}`
)

func found(ids ...string) string {
	nodes := make([]string, len(ids))
	for i, id := range ids {
		nodes[i] = fmt.Sprintf(`{"id": %q}`, id)
	}
	return `{"pageInfo": {"hasNextPage": false}, "nodes": [` + strings.Join(nodes, ", ") + `]}`
}

func discovery(results map[string]string) string {
	parts := []string{budget}
	for _, q := range qualifiers {
		body, ok := results[q.alias]
		if !ok {
			body = found()
		}
		parts = append(parts, fmt.Sprintf("%q: %s", q.alias, body))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func details(nodes ...string) string {
	return "{" + detailBudget + `, "nodes": [` + strings.Join(nodes, ", ") + "]}"
}

func minimal(id string) string {
	return fmt.Sprintf(`{"id": %q, "number": 1, "state": "OPEN", "title": "t", "url": "https://github.com/acme/a/pull/1", `+
		`"createdAt": "2026-09-01T09:00:00Z", "repository": {"nameWithOwner": "acme/a"}}`, id)
}

func tagsByID(prs []model.PR) map[string][]model.Tag {
	out := map[string][]model.Tag{}
	for _, pr := range prs {
		out[pr.ID] = pr.Tags
	}
	return out
}

func TestFetchTagsEachPR(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{
			"author":    found("PR_A"),
			"review":    found("PR_B"),
			"requested": found("PR_B", "PR_C"),
			"mentioned": found("PR_A"),
			"commented": found("PR_C"),
		})},
		{match: matchDetail, data: details(minimal("PR_A"), minimal("PR_B"), minimal("PR_C"))},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	want := map[string][]model.Tag{
		"PR_A": {model.TagAuthor, model.TagMentioned},
		"PR_B": {model.TagReview},
		"PR_C": {model.TagTeam, model.TagCommented},
	}
	if got := tagsByID(res.PRs); !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	vars := f.calls[1].vars
	if !reflect.DeepEqual(vars["ids"], []string{"PR_A", "PR_B", "PR_C"}) || vars["login"] != "me" {
		t.Fatalf("detail vars = %v", vars)
	}
	if res.Budget.Remaining != 4988 {
		t.Fatalf("budget = %+v, want the last query's", res.Budget)
	}
}

func TestFetchFollowsPages(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{
			"commented": `{"pageInfo": {"hasNextPage": true, "endCursor": "CUR1"}, "nodes": [{"id": "PR_A"}]}`,
		})},
		{match: matchPage, data: "{" + budget + `, "search": ` + found("PR_B") + "}"},
		{match: matchDetail, data: details(minimal("PR_A"), minimal("PR_B"))},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	if vars := f.calls[1].vars; vars["q"] != "is:pr is:open archived:false commenter:@me" || vars["after"] != "CUR1" {
		t.Fatalf("page vars = %v", vars)
	}
	want := map[string][]model.Tag{"PR_A": {model.TagCommented}, "PR_B": {model.TagCommented}}
	if got := tagsByID(res.PRs); !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
}

func TestFetchStopsAtTheSearchCap(t *testing.T) {
	more := `{"pageInfo": {"hasNextPage": true, "endCursor": "again"}, "nodes": []}`
	replies := []reply{{match: matchDiscovery, data: discovery(map[string]string{"author": more})}}
	for range maxPages - 1 {
		replies = append(replies, reply{match: matchPage, data: "{" + budget + `, "search": ` + more + "}"})
	}
	f := &fake{t: t, replies: replies}
	if _, err := New(f).Fetch(context.Background(), "me", nil); err != nil {
		t.Fatal(err)
	}
	f.finished()
}

func TestFetchBatchesDetail(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"author": found("PR_C", "PR_A", "PR_B")})},
		{match: matchDetail, data: details(minimal("PR_A"), minimal("PR_B"))},
		{match: matchDetail, data: details(minimal("PR_C"))},
	}}
	fetcher := New(f)
	fetcher.prBatch = 2
	res, err := fetcher.Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	if got := f.calls[1].vars["ids"]; !reflect.DeepEqual(got, []string{"PR_A", "PR_B"}) {
		t.Fatalf("first batch = %v", got)
	}
	if got := f.calls[2].vars["ids"]; !reflect.DeepEqual(got, []string{"PR_C"}) {
		t.Fatalf("second batch = %v", got)
	}
	if len(res.PRs) != 3 {
		t.Fatalf("got %d PRs", len(res.PRs))
	}
}

func TestFetchKeepsPartialData(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"author": found("PR_A", "PR_B")})},
		{match: matchDetail, data: details(minimal("PR_A"), "null"),
			err: &gh.PartialError{Messages: []string{"SAML enforcement"}}},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PRs) != 1 || res.PRs[0].ID != "PR_A" {
		t.Fatalf("PRs = %+v", res.PRs)
	}
	if !reflect.DeepEqual(res.Warnings, []string{"SAML enforcement"}) {
		t.Fatalf("warnings = %q", res.Warnings)
	}
}

func TestFetchFailsOnError(t *testing.T) {
	boom := errors.New("boom")
	f := &fake{t: t, replies: []reply{{match: matchDiscovery, err: boom}}}
	if _, err := New(f).Fetch(context.Background(), "me", nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestFetchNothingOpen(t *testing.T) {
	f := &fake{t: t, replies: []reply{{match: matchDiscovery, data: discovery(nil)}}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil || len(res.PRs) != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
	f.finished()
}

func TestViewerCollectsTeams(t *testing.T) {
	org := func(slugs ...string) string {
		nodes := make([]string, len(slugs))
		for i, s := range slugs {
			nodes[i] = fmt.Sprintf(`{"combinedSlug": %q}`, s)
		}
		return `{"teams": {"nodes": [` + strings.Join(nodes, ", ") + `]}}`
	}
	f := &fake{t: t, replies: []reply{
		{match: "viewer { login }", data: "{" + budget + `, "viewer": {"login": "me"}}`},
		{match: "organizations(first: 100", data: "{" + budget + `, "viewer": {"organizations": {` +
			`"pageInfo": {"hasNextPage": true, "endCursor": "O1"}, "nodes": [` + org("acme/devs") + ", " + org() + `]}}}`},
		{match: "organizations(first: 100", data: "{" + budget + `, "viewer": {"organizations": {` +
			`"pageInfo": {"hasNextPage": false}, "nodes": [` + org("other/ops") + `]}}}`},
	}}
	v, meta, err := New(f).Viewer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	if want := (Viewer{Login: "me", Teams: []string{"acme/devs", "other/ops"}}); !reflect.DeepEqual(v, want) {
		t.Fatalf("viewer = %+v, want %+v", v, want)
	}
	if f.calls[1].vars["login"] != "me" || f.calls[1].vars["after"] != nil || f.calls[2].vars["after"] != "O1" {
		t.Fatalf("team vars = %v then %v", f.calls[1].vars, f.calls[2].vars)
	}
	if meta.Budget.Limit != 5000 {
		t.Fatalf("budget = %+v", meta.Budget)
	}
}

func TestFetchFailsWhenResultsAreMissing(t *testing.T) {
	partial := &gh.PartialError{Messages: []string{"timeout"}}
	cases := map[string][]reply{
		"search alias": {{match: matchDiscovery, data: `{"author": null}`, err: partial}},
		"detail nodes": {
			{match: matchDiscovery, data: discovery(map[string]string{"author": found("PR_A")})},
			{match: matchDetail, data: `{"nodes": null}`, err: partial},
		},
		"next page": {
			{match: matchDiscovery, data: discovery(map[string]string{
				"author": `{"pageInfo": {"hasNextPage": true, "endCursor": "C"}, "nodes": [{"id": "PR_A"}]}`,
			})},
			{match: matchPage, data: `{"search": null}`, err: partial},
		},
	}
	for name, replies := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fake{t: t, replies: replies}
			if _, err := New(f).Fetch(context.Background(), "me", nil); err == nil {
				t.Fatal("Fetch succeeded without the data it asked for")
			}
		})
	}
}

func TestViewerFailsWithoutLogin(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: "viewer { login }", data: `{"viewer": null}`, err: &gh.PartialError{Messages: []string{"timeout"}}},
	}}
	if _, _, err := New(f).Viewer(context.Background()); err == nil {
		t.Fatal("Viewer succeeded without a login")
	}
}

func TestFetchDropsPRsClosedSinceSearchIndexed(t *testing.T) {
	merged := strings.Replace(minimal("PR_B"), `"state": "OPEN"`, `"state": "MERGED"`, 1)
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"author": found("PR_A", "PR_B")})},
		{match: matchDetail, data: details(minimal("PR_A"), merged)},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PRs) != 1 || res.PRs[0].ID != "PR_A" {
		t.Fatalf("PRs = %+v, want only the open one", res.PRs)
	}
}

// measured live on 23 September 2026: 20 PRs a request took about 7s and
// drew intermittent 502s from github's 10s query limit; 10 took about 5s.
func TestFetchDetailsTenAtATime(t *testing.T) {
	ids := make([]string, 24)
	nodes := make([]string, 24)
	for i := range ids {
		ids[i] = fmt.Sprintf("PR_%02d", i)
		nodes[i] = minimal(ids[i])
	}
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"author": found(ids...)})},
		{match: matchDetail, data: details(nodes[:10]...)},
		{match: matchDetail, data: details(nodes[10:20]...)},
		{match: matchDetail, data: details(nodes[20:]...)},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	if len(res.PRs) != 24 {
		t.Fatalf("got %d PRs", len(res.PRs))
	}
}

func TestFetchReportsProgress(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{
			"author":      found("PR_A", "PR_B", "PR_C"),
			"issueAuthor": found("I_A"),
		})},
		{match: matchDetail, data: details(minimal("PR_A"), minimal("PR_B"))},
		{match: matchDetail, data: details(minimal("PR_C"))},
		{match: matchIssues, data: details(minimalIssue("I_A"))},
	}}
	fetcher := New(f)
	fetcher.prBatch = 2
	var got []Progress
	if _, err := fetcher.Fetch(context.Background(), "me", func(p Progress) { got = append(got, p) }); err != nil {
		t.Fatal(err)
	}
	want := []Progress{
		{Phase: "finding PRs and issues"},
		{Phase: "details", Total: 4},
		{Phase: "details", Done: 2, Total: 4},
		{Phase: "details", Done: 3, Total: 4},
		{Phase: "details", Done: 4, Total: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("progress = %+v, want %+v", got, want)
	}
}

func minimalIssue(id string) string {
	return fmt.Sprintf(`{"id": %q, "number": 1, "state": "OPEN", "title": "t", "url": "https://github.com/acme/a/issues/1", `+
		`"createdAt": "2026-09-01T09:00:00Z", "repository": {"nameWithOwner": "acme/a"}}`, id)
}

func issueTagsByID(issues []model.Issue) map[string][]model.Tag {
	out := map[string][]model.Tag{}
	for _, is := range issues {
		out[is.ID] = is.Tags
	}
	return out
}

func TestFetchFindsIssuesApartFromPRs(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{
			"author":         found("PR_A"),
			"issueAuthor":    found("I_A"),
			"issueAssigned":  found("I_A", "I_B"),
			"issueMentioned": found("I_C"),
			"issueCommented": found("I_C"),
		})},
		{match: matchDetail, data: details(minimal("PR_A"))},
		{match: matchIssues, data: details(minimalIssue("I_A"), minimalIssue("I_B"), minimalIssue("I_C"))},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	if got := tagsByID(res.PRs); !reflect.DeepEqual(got, map[string][]model.Tag{"PR_A": {model.TagAuthor}}) {
		t.Fatalf("pr tags = %v", got)
	}
	want := map[string][]model.Tag{
		"I_A": {model.TagAuthor, model.TagAssigned},
		"I_B": {model.TagAssigned},
		"I_C": {model.TagMentioned, model.TagCommented},
	}
	if got := issueTagsByID(res.Issues); !reflect.DeepEqual(got, want) {
		t.Fatalf("issue tags = %v, want %v", got, want)
	}
	vars := f.calls[2].vars
	if !reflect.DeepEqual(vars["ids"], []string{"I_A", "I_B", "I_C"}) {
		t.Fatalf("issue detail vars = %v", vars)
	}
	if _, ok := vars["login"]; ok {
		t.Fatal("issue detail takes no login")
	}
}

func TestIssuePagesKeepTheIssueScope(t *testing.T) {
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{
			"issueCommented": `{"pageInfo": {"hasNextPage": true, "endCursor": "CUR1"}, "nodes": [{"id": "I_A"}]}`,
		})},
		{match: matchPage, data: "{" + budget + `, "search": ` + found("I_B") + "}"},
		{match: matchIssues, data: details(minimalIssue("I_A"), minimalIssue("I_B"))},
	}}
	if _, err := New(f).Fetch(context.Background(), "me", nil); err != nil {
		t.Fatal(err)
	}
	f.finished()
	if q := f.calls[1].vars["q"]; q != "is:issue is:open archived:false commenter:@me" {
		t.Fatalf("page query = %v", q)
	}
}

// measured live on 24 September 2026: 25 issues a request took 1.5s for a
// point, 50 took 4.4s for two.
func TestFetchDetailsIssuesTwentyFiveAtATime(t *testing.T) {
	ids := make([]string, 30)
	nodes := make([]string, 30)
	for i := range ids {
		ids[i] = fmt.Sprintf("I_%02d", i)
		nodes[i] = minimalIssue(ids[i])
	}
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"issueAuthor": found(ids...)})},
		{match: matchIssues, data: details(nodes[:25]...)},
		{match: matchIssues, data: details(nodes[25:]...)},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.finished()
	if len(res.Issues) != 30 {
		t.Fatalf("got %d issues", len(res.Issues))
	}
}

func TestFetchDropsIssuesClosedSinceSearchIndexed(t *testing.T) {
	closed := strings.Replace(minimalIssue("I_B"), `"state": "OPEN"`, `"state": "CLOSED"`, 1)
	f := &fake{t: t, replies: []reply{
		{match: matchDiscovery, data: discovery(map[string]string{"issueAuthor": found("I_A", "I_B")})},
		{match: matchIssues, data: details(minimalIssue("I_A"), closed)},
	}}
	res, err := New(f).Fetch(context.Background(), "me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 1 || res.Issues[0].ID != "I_A" {
		t.Fatalf("issues = %+v, want only the open one", res.Issues)
	}
}
