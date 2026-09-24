package fetch

import (
	"strings"
	"time"

	"github.com/jasonmadigan/docket/internal/model"
)

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type searchResult struct {
	PageInfo pageInfo `json:"pageInfo"`
	Nodes    []struct {
		ID string `json:"id"`
	} `json:"nodes"`
}

type actor struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

// model is the zero actor for deleted accounts, which github returns as
// null.
func (a *actor) model() model.Actor {
	if a == nil {
		return model.Actor{}
	}
	return model.Actor{Login: a.Login, Bot: a.Typename == "Bot" || strings.HasSuffix(a.Login, "[bot]")}
}

type reviewer struct {
	Typename     string `json:"__typename"`
	Login        string `json:"login"`
	CombinedSlug string `json:"combinedSlug"`
}

func (r *reviewer) name() string {
	if r.Typename == "Team" {
		return r.CombinedSlug
	}
	return r.Login
}

type review struct {
	Author      *actor     `json:"author"`
	State       string     `json:"state"`
	SubmittedAt *time.Time `json:"submittedAt"`
	Commit      *struct {
		OID string `json:"oid"`
	} `json:"commit"`
}

// model skips pending reviews, which have no submission time.
func (r review) model() (model.Review, bool) {
	if r.SubmittedAt == nil {
		return model.Review{}, false
	}
	out := model.Review{Author: r.Author.model(), State: r.State, At: *r.SubmittedAt}
	if r.Commit != nil {
		out.Commit = r.Commit.OID
	}
	return out, true
}

type count struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type checkContext struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
	Context    string `json:"context"`
	State      string `json:"state"`
	TargetURL  string `json:"targetUrl"`
}

type rollup struct {
	State    string `json:"state"`
	Contexts struct {
		CheckRunCountsByState      []count        `json:"checkRunCountsByState"`
		StatusContextCountsByState []count        `json:"statusContextCountsByState"`
		Nodes                      []checkContext `json:"nodes"`
	} `json:"contexts"`
}

var (
	failedRuns     = map[string]bool{"FAILURE": true, "TIMED_OUT": true, "CANCELLED": true, "ACTION_REQUIRED": true, "STARTUP_FAILURE": true}
	failedStatuses = map[string]bool{"FAILURE": true, "ERROR": true}
)

func (r *rollup) model() model.Checks {
	if r == nil {
		return model.Checks{}
	}
	c := model.Checks{State: r.State}
	for _, n := range r.Contexts.CheckRunCountsByState {
		if failedRuns[n.State] {
			c.FailingCount += n.Count
		}
	}
	for _, n := range r.Contexts.StatusContextCountsByState {
		if failedStatuses[n.State] {
			c.FailingCount += n.Count
		}
	}
	for _, n := range r.Contexts.Nodes {
		switch {
		case n.Typename == "CheckRun" && failedRuns[n.Conclusion]:
			c.Failing = append(c.Failing, model.Check{Name: n.Name, URL: n.DetailsURL})
		case n.Typename == "StatusContext" && failedStatuses[n.State]:
			c.Failing = append(c.Failing, model.Check{Name: n.Context, URL: n.TargetURL})
		}
	}
	return c
}

type timelineItem struct {
	Typename          string     `json:"__typename"`
	Author            *actor     `json:"author"`
	Actor             *actor     `json:"actor"`
	CreatedAt         *time.Time `json:"createdAt"`
	SubmittedAt       *time.Time `json:"submittedAt"`
	State             string     `json:"state"`
	RequestedReviewer *reviewer  `json:"requestedReviewer"`
	Assignee          *actor     `json:"assignee"`
}

func (it timelineItem) model() (model.Event, bool) {
	var e model.Event
	at := it.CreatedAt
	switch it.Typename {
	case "IssueComment":
		e = model.Event{Kind: model.EventComment, Actor: it.Author.model()}
	case "PullRequestReview":
		e = model.Event{Kind: model.EventReview, Actor: it.Author.model(), State: it.State}
		at = it.SubmittedAt
	case "HeadRefForcePushedEvent":
		e = model.Event{Kind: model.EventForcePush, Actor: it.Actor.model()}
	case "ReviewRequestedEvent":
		e = model.Event{Kind: model.EventReviewRequested, Actor: it.Actor.model()}
		if it.RequestedReviewer != nil {
			e.Target = it.RequestedReviewer.name()
		}
	case "MentionedEvent":
		e = model.Event{Kind: model.EventMentioned, Actor: it.Actor.model()}
	case "AssignedEvent":
		e = model.Event{Kind: model.EventAssigned, Actor: it.Actor.model(), Target: it.Assignee.model().Login}
	case "CrossReferencedEvent":
		e = model.Event{Kind: model.EventReferenced, Actor: it.Actor.model()}
	case "ReopenedEvent":
		e = model.Event{Kind: model.EventReopened, Actor: it.Actor.model()}
	default:
		return model.Event{}, false
	}
	if at == nil {
		return model.Event{}, false
	}
	e.At = *at
	return e, true
}

type timeline struct {
	PageInfo struct {
		HasPreviousPage bool `json:"hasPreviousPage"`
	} `json:"pageInfo"`
	Nodes []timelineItem `json:"nodes"`
}

func (t timeline) events() []model.Event {
	var out []model.Event
	for _, it := range t.Nodes {
		if e, ok := it.model(); ok {
			out = append(out, e)
		}
	}
	return out
}

type pullRequest struct {
	ID         string    `json:"id"`
	Number     int       `json:"number"`
	State      string    `json:"state"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	IsDraft    bool      `json:"isDraft"`
	CreatedAt  time.Time `json:"createdAt"`
	Author     *actor    `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	HeadRefOID       string `json:"headRefOid"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
	ReviewDecision   string `json:"reviewDecision"`
	ReviewRequests   struct {
		Nodes []struct {
			RequestedReviewer *reviewer `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	LatestOpinionatedReviews struct {
		Nodes []review `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	MyReviews struct {
		Nodes []review `json:"nodes"`
	} `json:"myReviews"`
	ReviewThreads struct {
		PageInfo pageInfo `json:"pageInfo"`
		Nodes    []struct {
			IsResolved bool `json:"isResolved"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	Commits struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Commit struct {
				OID           string    `json:"oid"`
				CommittedDate time.Time `json:"committedDate"`
				Author        struct {
					Name string `json:"name"`
					User *struct {
						Login string `json:"login"`
					} `json:"user"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Head struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *rollup `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"head"`
	ClosingIssuesReferences struct {
		Nodes []struct {
			Number     int    `json:"number"`
			Title      string `json:"title"`
			URL        string `json:"url"`
			State      string `json:"state"`
			Repository struct {
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"repository"`
		} `json:"nodes"`
	} `json:"closingIssuesReferences"`
	TimelineItems timeline `json:"timelineItems"`
}

func (p *pullRequest) model(tags []model.Tag) model.PR {
	pr := model.PR{
		Item: model.Item{
			ID:                p.ID,
			Repo:              p.Repository.NameWithOwner,
			Number:            p.Number,
			Title:             p.Title,
			URL:               p.URL,
			Author:            p.Author.model(),
			CreatedAt:         p.CreatedAt,
			Tags:              tags,
			Timeline:          p.TimelineItems.events(),
			TimelineTruncated: p.TimelineItems.PageInfo.HasPreviousPage,
		},
		Draft:          p.IsDraft,
		Mergeable:      p.Mergeable,
		MergeState:     p.MergeStateStatus,
		ReviewDecision: p.ReviewDecision,
		HeadOID:        p.HeadRefOID,
		MoreThreads:    p.ReviewThreads.PageInfo.HasNextPage,
		CommitCount:    p.Commits.TotalCount,
	}
	for _, n := range p.ReviewRequests.Nodes {
		if r := n.RequestedReviewer; r != nil {
			pr.Requests = append(pr.Requests, model.Reviewer{Name: r.name(), Team: r.Typename == "Team"})
		}
	}
	for _, r := range p.LatestOpinionatedReviews.Nodes {
		if rv, ok := r.model(); ok {
			pr.Opinions = append(pr.Opinions, rv)
		}
	}
	if len(p.MyReviews.Nodes) > 0 {
		if rv, ok := p.MyReviews.Nodes[0].model(); ok {
			pr.MyReview = &rv
		}
	}
	for _, t := range p.ReviewThreads.Nodes {
		if !t.IsResolved {
			pr.UnresolvedThreads++
		}
	}
	for _, n := range p.Commits.Nodes {
		c := n.Commit
		author := model.Actor{Login: c.Author.Name}
		if c.Author.User != nil {
			author.Login = c.Author.User.Login
		}
		author.Bot = strings.HasSuffix(author.Login, "[bot]") || strings.HasSuffix(c.Author.Name, "[bot]")
		pr.Commits = append(pr.Commits, model.Commit{OID: c.OID, At: c.CommittedDate, Author: author})
	}
	if len(p.Head.Nodes) > 0 {
		pr.Checks = p.Head.Nodes[0].Commit.StatusCheckRollup.model()
	}
	for _, i := range p.ClosingIssuesReferences.Nodes {
		pr.Issues = append(pr.Issues, model.IssueRef{
			Repo: i.Repository.NameWithOwner, Number: i.Number, Title: i.Title, URL: i.URL, State: i.State,
		})
	}
	return pr
}

type linkedPR struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	IsDraft    bool   `json:"isDraft"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type issue struct {
	ID         string    `json:"id"`
	Number     int       `json:"number"`
	State      string    `json:"state"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	Author     *actor    `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees struct {
		Nodes []*actor `json:"nodes"`
	} `json:"assignees"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	SubIssuesSummary *struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
	} `json:"subIssuesSummary"`
	ClosedByPullRequestsReferences struct {
		Nodes []linkedPR `json:"nodes"`
	} `json:"closedByPullRequestsReferences"`
	TimelineItems timeline `json:"timelineItems"`
}

func (i *issue) model(tags []model.Tag) model.Issue {
	is := model.Issue{Item: model.Item{
		ID: i.ID, Repo: i.Repository.NameWithOwner, Number: i.Number, Title: i.Title, URL: i.URL,
		Author: i.Author.model(), CreatedAt: i.CreatedAt, Tags: tags,
		Timeline: i.TimelineItems.events(), TimelineTruncated: i.TimelineItems.PageInfo.HasPreviousPage,
	}}
	for _, a := range i.Assignees.Nodes {
		if a != nil {
			is.Assignees = append(is.Assignees, a.model())
		}
	}
	for _, l := range i.Labels.Nodes {
		is.Labels = append(is.Labels, l.Name)
	}
	if s := i.SubIssuesSummary; s != nil {
		is.SubIssues = model.SubIssues{Total: s.Total, Completed: s.Completed}
	}
	for _, pr := range i.ClosedByPullRequestsReferences.Nodes {
		if pr.State == "CLOSED" {
			continue // closed unmerged, it no longer bears on the issue
		}
		is.PRs = append(is.PRs, model.PRRef{
			Repo: pr.Repository.NameWithOwner, Number: pr.Number, Title: pr.Title, URL: pr.URL,
			State: pr.State, Draft: pr.IsDraft,
		})
	}
	return is
}
