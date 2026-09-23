package fetch

import (
	"fmt"
	"strings"

	"github.com/jasonmadigan/docket/internal/model"
)

const scope = "is:pr is:open archived:false "

type qualifier struct {
	alias string
	query string
	tag   model.Tag // empty for requested, which splits into review and team
}

var qualifiers = []qualifier{
	{"author", "author:@me", model.TagAuthor},
	{"review", "user-review-requested:@me", model.TagReview},
	{"requested", "review-requested:@me", ""},
	{"assigned", "assignee:@me", model.TagAssigned},
	{"mentioned", "mentions:@me", model.TagMentioned},
	{"reviewed", "reviewed-by:@me", model.TagReviewed},
	{"commented", "commenter:@me", model.TagCommented},
}

const (
	rateLimit      = "rateLimit { cost remaining limit resetAt }"
	searchFields   = "pageInfo { hasNextPage endCursor } nodes { ... on PullRequest { id } }"
	reviewerFields = "__typename ... on User { login } ... on Team { combinedSlug } ... on Bot { login } ... on Mannequin { login }"
)

var discoveryQuery = func() string {
	var b strings.Builder
	b.WriteString("query {\n  " + rateLimit + "\n")
	for _, q := range qualifiers {
		fmt.Fprintf(&b, "  %s: search(query: %q, type: ISSUE, first: 100) { %s }\n", q.alias, scope+q.query, searchFields)
	}
	b.WriteString("}")
	return b.String()
}()

const pageQuery = `query($q: String!, $after: String) {
  ` + rateLimit + `
  search(query: $q, type: ISSUE, first: 100, after: $after) { ` + searchFields + ` }
}`

const detailQuery = `query($ids: [ID!]!, $login: String!) {
  ` + rateLimit + `
  nodes(ids: $ids) {
    ... on PullRequest {
      id number title url state isDraft createdAt
      author { __typename login }
      repository { nameWithOwner }
      headRefOid mergeable mergeStateStatus reviewDecision
      reviewRequests(first: 20) { nodes { requestedReviewer { ` + reviewerFields + ` } } }
      latestOpinionatedReviews(first: 20) { nodes { author { __typename login } state submittedAt commit { oid } } }
      myReviews: reviews(last: 1, author: $login, states: [APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED]) { nodes { author { __typename login } state submittedAt commit { oid } } }
      reviewThreads(first: 100) { pageInfo { hasNextPage } nodes { isResolved } }
      commits(last: 100) { totalCount nodes { commit { oid committedDate author { name user { login } } } } }
      head: commits(last: 1) { nodes { commit { statusCheckRollup { state contexts(first: 100) {
        checkRunCountsByState { state count }
        statusContextCountsByState { state count }
        nodes { __typename ... on CheckRun { name conclusion detailsUrl } ... on StatusContext { context state targetUrl } }
      } } } } }
      closingIssuesReferences(first: 10) { nodes { number title url state repository { nameWithOwner } } }
      timelineItems(last: 100, itemTypes: [ISSUE_COMMENT, PULL_REQUEST_REVIEW, HEAD_REF_FORCE_PUSHED_EVENT, REVIEW_REQUESTED_EVENT, MENTIONED_EVENT]) {
        pageInfo { hasPreviousPage }
        nodes {
          __typename
          ... on IssueComment { author { __typename login } createdAt }
          ... on PullRequestReview { author { __typename login } submittedAt state }
          ... on HeadRefForcePushedEvent { actor { __typename login } createdAt }
          ... on ReviewRequestedEvent { actor { __typename login } createdAt requestedReviewer { ` + reviewerFields + ` } }
          ... on MentionedEvent { actor { __typename login } createdAt }
        }
      }
    }
  }
}`

const viewerQuery = `query {
  ` + rateLimit + `
  viewer { login }
}`

const teamsQuery = `query($login: String!, $after: String) {
  ` + rateLimit + `
  viewer { organizations(first: 100, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes { teams(first: 100, userLogins: [$login]) { nodes { combinedSlug } } }
  } }
}`
