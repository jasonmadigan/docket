package fetch

import (
	"fmt"
	"strings"

	"github.com/jasonmadigan/docket/internal/model"
)

const (
	prScope    = "is:pr is:open archived:false "
	issueScope = "is:issue is:open archived:false "
)

type qualifier struct {
	alias string
	query string
	tag   model.Tag // empty for requested, which splits into review and team
	issue bool
}

// search is the qualifier as github's search box takes it.
func (q qualifier) search() string {
	if q.issue {
		return issueScope + q.query
	}
	return prScope + q.query
}

var qualifiers = []qualifier{
	{alias: "author", query: "author:@me", tag: model.TagAuthor},
	{alias: "review", query: "user-review-requested:@me", tag: model.TagReview},
	{alias: "requested", query: "review-requested:@me"},
	{alias: "assigned", query: "assignee:@me", tag: model.TagAssigned},
	{alias: "mentioned", query: "mentions:@me", tag: model.TagMentioned},
	{alias: "reviewed", query: "reviewed-by:@me", tag: model.TagReviewed},
	{alias: "commented", query: "commenter:@me", tag: model.TagCommented},
	{alias: "issueAuthor", query: "author:@me", tag: model.TagAuthor, issue: true},
	{alias: "issueAssigned", query: "assignee:@me", tag: model.TagAssigned, issue: true},
	{alias: "issueMentioned", query: "mentions:@me", tag: model.TagMentioned, issue: true},
	{alias: "issueCommented", query: "commenter:@me", tag: model.TagCommented, issue: true},
}

const (
	rateLimit      = "rateLimit { cost remaining limit resetAt }"
	searchFields   = "pageInfo { hasNextPage endCursor } nodes { ... on PullRequest { id } ... on Issue { id } }"
	reviewerFields = "__typename ... on User { login } ... on Team { combinedSlug } ... on Bot { login } ... on Mannequin { login }"
	assigneeFields = "__typename ... on User { login } ... on Bot { login } ... on Mannequin { login } ... on Organization { login }"
)

var discoveryQuery = func() string {
	var b strings.Builder
	b.WriteString("query {\n  " + rateLimit + "\n")
	for _, q := range qualifiers {
		fmt.Fprintf(&b, "  %s: search(query: %q, type: ISSUE, first: 100) { %s }\n", q.alias, q.search(), searchFields)
	}
	b.WriteString("}")
	return b.String()
}()

const pageQuery = `query($q: String!, $after: String) {
  ` + rateLimit + `
  search(query: $q, type: ISSUE, first: 100, after: $after) { ` + searchFields + ` }
}`

const prDetailQuery = `query($ids: [ID!]!, $login: String!) {
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
      timelineItems(last: 100, itemTypes: [ISSUE_COMMENT, PULL_REQUEST_REVIEW, HEAD_REF_FORCE_PUSHED_EVENT, REVIEW_REQUESTED_EVENT, MENTIONED_EVENT, REOPENED_EVENT]) {
        pageInfo { hasPreviousPage }
        nodes {
          __typename
          ... on IssueComment { author { __typename login } createdAt }
          ... on PullRequestReview { author { __typename login } submittedAt state }
          ... on HeadRefForcePushedEvent { actor { __typename login } createdAt }
          ... on ReviewRequestedEvent { actor { __typename login } createdAt requestedReviewer { ` + reviewerFields + ` } }
          ... on MentionedEvent { actor { __typename login } createdAt }
          ... on ReopenedEvent { actor { __typename login } createdAt }
        }
      }
    }
  }
}`

const issueDetailQuery = `query($ids: [ID!]!) {
  ` + rateLimit + `
  nodes(ids: $ids) {
    ... on Issue {
      id number title url state createdAt
      author { __typename login }
      repository { nameWithOwner }
      assignees(first: 10) { nodes { login } }
      labels(first: 20) { nodes { name } }
      subIssuesSummary { total completed }
      closedByPullRequestsReferences(first: 10, includeClosedPrs: true) { nodes { number title url state isDraft repository { nameWithOwner } } }
      timelineItems(last: 100, itemTypes: [ISSUE_COMMENT, MENTIONED_EVENT, ASSIGNED_EVENT, CROSS_REFERENCED_EVENT, REOPENED_EVENT]) {
        pageInfo { hasPreviousPage }
        nodes {
          __typename
          ... on IssueComment { author { __typename login } createdAt }
          ... on MentionedEvent { actor { __typename login } createdAt }
          ... on AssignedEvent { actor { __typename login } createdAt assignee { ` + assigneeFields + ` } }
          ... on CrossReferencedEvent { actor { __typename login } createdAt }
          ... on ReopenedEvent { actor { __typename login } createdAt }
        }
      }
    }
  }
}`

const stateQuery = `query($ids: [ID!]!) {
  ` + rateLimit + `
  nodes(ids: $ids) { ... on Issue { id state } ... on PullRequest { id state } }
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
