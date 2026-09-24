package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

// search returns at most 1,000 results: ten pages of 100.
const maxPages = 10

// lookupBatch is how many archived ids gone() resolves a request.
const lookupBatch = 100

type Transport interface {
	Do(ctx context.Context, query string, vars map[string]any, out any) error
}

type Fetcher struct {
	t          Transport
	prBatch    int
	issueBatch int
}

// New batches detail ten prs a request: twenty took about 7s live and drew
// intermittent 502s from github's query time limit. Issues are lighter: 25
// took 1.5s for a point, 50 took 4.4s for two.
func New(t Transport) *Fetcher {
	return &Fetcher{t: t, prBatch: 10, issueBatch: 25}
}

type Viewer struct {
	Login string
	Teams []string
}

type Meta struct {
	Budget   gh.Budget
	Warnings []string
}

type Result struct {
	PRs    []model.PR
	Issues []model.Issue
	Gone   []string // archived items closed, merged, or no longer there
	Meta
}

// Progress says how far a fetch has got, for views to show.
type Progress struct {
	Phase string `json:"phase"`
	Done  int    `json:"done,omitempty"`
	Total int    `json:"total,omitempty"`
}

// counter reports detail done across both kinds.
type counter struct {
	done, total int
	report      func(Progress)
}

func (c *counter) add(n int) {
	c.done += n
	c.report(Progress{Phase: "details", Done: c.done, Total: c.total})
}

func (m *Meta) saw(b gh.Budget) {
	if b.Limit > 0 {
		m.Budget = b
	}
}

// do keeps partial data, recording its errors as warnings.
func (f *Fetcher) do(ctx context.Context, query string, vars map[string]any, out any, meta *Meta) error {
	err := f.t.Do(ctx, query, vars, out)
	var partial *gh.PartialError
	if errors.As(err, &partial) {
		for _, msg := range partial.Messages {
			if !slices.Contains(meta.Warnings, msg) {
				meta.Warnings = append(meta.Warnings, msg)
			}
		}
		return nil
	}
	return err
}

func (f *Fetcher) Viewer(ctx context.Context) (Viewer, Meta, error) {
	var meta Meta
	var who struct {
		RateLimit gh.Budget `json:"rateLimit"`
		Viewer    struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := f.do(ctx, viewerQuery, nil, &who, &meta); err != nil {
		return Viewer{}, Meta{}, fmt.Errorf("viewer: %w", err)
	}
	if who.Viewer.Login == "" {
		return Viewer{}, Meta{}, errors.New("viewer: no login in the response")
	}
	meta.saw(who.RateLimit)
	v := Viewer{Login: who.Viewer.Login}
	var after any
	for {
		var page struct {
			RateLimit gh.Budget `json:"rateLimit"`
			Viewer    struct {
				Organizations struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []struct {
						Teams struct {
							Nodes []struct {
								CombinedSlug string `json:"combinedSlug"`
							} `json:"nodes"`
						} `json:"teams"`
					} `json:"nodes"`
				} `json:"organizations"`
			} `json:"viewer"`
		}
		if err := f.do(ctx, teamsQuery, map[string]any{"login": v.Login, "after": after}, &page, &meta); err != nil {
			return Viewer{}, Meta{}, fmt.Errorf("teams: %w", err)
		}
		meta.saw(page.RateLimit)
		orgs := page.Viewer.Organizations
		for _, org := range orgs.Nodes {
			for _, t := range org.Teams.Nodes {
				v.Teams = append(v.Teams, t.CombinedSlug)
			}
		}
		if !orgs.PageInfo.HasNextPage {
			return v, meta, nil
		}
		after = orgs.PageInfo.EndCursor
	}
}

func (f *Fetcher) Fetch(ctx context.Context, login string, archived []string, progress func(Progress)) (Result, error) {
	if progress == nil {
		progress = func(Progress) {}
	}
	var res Result
	progress(Progress{Phase: "finding PRs and issues"})
	prTags, issueTags, err := f.discover(ctx, &res.Meta)
	if err != nil {
		return Result{}, fmt.Errorf("discovery: %w", err)
	}
	c := &counter{total: len(prTags) + len(issueTags), report: progress}
	progress(Progress{Phase: "details", Total: c.total})
	if res.PRs, err = f.prDetails(ctx, prTags, login, &res.Meta, c); err != nil {
		return Result{}, fmt.Errorf("details: %w", err)
	}
	if res.Issues, err = f.issueDetails(ctx, issueTags, &res.Meta, c); err != nil {
		return Result{}, fmt.Errorf("issue details: %w", err)
	}
	var missing []string
	for _, id := range archived {
		_, pr := prTags[id]
		_, is := issueTags[id]
		if !pr && !is {
			missing = append(missing, id)
		}
	}
	if res.Gone, err = f.gone(ctx, missing, &res.Meta); err != nil {
		return Result{}, fmt.Errorf("archived: %w", err)
	}
	return res, nil
}

// gone looks up archived items discovery no longer finds. Closed, merged
// and ids github can't resolve (deleted, or no longer visible) have ended
// their archive. A null node for any other reason, such as an org
// enforcing SAML, is left alone, its message kept as a warning.
func (f *Fetcher) gone(ctx context.Context, ids []string, meta *Meta) ([]string, error) {
	var out []string
	for batch := range slices.Chunk(ids, lookupBatch) {
		var resp struct {
			RateLimit gh.Budget `json:"rateLimit"`
			Nodes     []*struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"nodes"`
		}
		err := f.t.Do(ctx, stateQuery, map[string]any{"ids": batch}, &resp)
		var partial *gh.PartialError
		switch {
		case errors.As(err, &partial):
			for _, p := range partial.Problems {
				if i, ok := nodeIndex(p.Path); ok && p.Type == "NOT_FOUND" && i < len(batch) {
					out = append(out, batch[i])
				} else if !slices.Contains(meta.Warnings, p.Message) {
					meta.Warnings = append(meta.Warnings, p.Message)
				}
			}
		case err != nil:
			return nil, err
		}
		if resp.Nodes == nil {
			return nil, errors.New("nodes missing from the response")
		}
		meta.saw(resp.RateLimit)
		for _, n := range resp.Nodes {
			if n != nil && (n.State == "CLOSED" || n.State == "MERGED") {
				out = append(out, n.ID)
			}
		}
	}
	return out, nil
}

// nodeIndex reads which of nodes(ids:) an error's path points at.
func nodeIndex(path []any) (int, bool) {
	if len(path) < 2 || path[0] != "nodes" {
		return 0, false
	}
	switch i := path[1].(type) {
	case float64:
		return int(i), true
	case int:
		return i, true
	}
	return 0, false
}

func (f *Fetcher) discover(ctx context.Context, meta *Meta) (prs, issues map[string][]model.Tag, err error) {
	var resp map[string]json.RawMessage
	if err := f.do(ctx, discoveryQuery, nil, &resp, meta); err != nil {
		return nil, nil, err
	}
	var budget gh.Budget
	if raw, ok := resp["rateLimit"]; ok {
		if err := json.Unmarshal(raw, &budget); err != nil {
			return nil, nil, err
		}
	}
	meta.saw(budget)
	found := map[string]map[string]bool{}
	for _, q := range qualifiers {
		raw, ok := resp[q.alias]
		if !ok || string(raw) == "null" {
			return nil, nil, fmt.Errorf("%s: missing from the response", q.alias)
		}
		var sr searchResult
		if err := json.Unmarshal(raw, &sr); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", q.alias, err)
		}
		ids := map[string]bool{}
		for page := 1; ; page++ {
			for _, n := range sr.Nodes {
				if n.ID != "" {
					ids[n.ID] = true
				}
			}
			if !sr.PageInfo.HasNextPage || page == maxPages {
				break
			}
			var err error
			if sr, err = f.page(ctx, q.search(), sr.PageInfo.EndCursor, meta); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", q.alias, err)
			}
		}
		found[q.alias] = ids
	}
	prs, issues = map[string][]model.Tag{}, map[string][]model.Tag{}
	for _, q := range qualifiers {
		tags := prs
		if q.issue {
			tags = issues
		}
		for id := range found[q.alias] {
			switch {
			case q.tag != "":
				tags[id] = append(tags[id], q.tag)
			case !found["review"][id]:
				tags[id] = append(tags[id], model.TagTeam)
			}
		}
	}
	return prs, issues, nil
}

func (f *Fetcher) page(ctx context.Context, query, after string, meta *Meta) (searchResult, error) {
	var resp struct {
		RateLimit gh.Budget     `json:"rateLimit"`
		Search    *searchResult `json:"search"`
	}
	if err := f.do(ctx, pageQuery, map[string]any{"q": query, "after": after}, &resp, meta); err != nil {
		return searchResult{}, err
	}
	if resp.Search == nil {
		return searchResult{}, errors.New("search missing from the response")
	}
	meta.saw(resp.RateLimit)
	return *resp.Search, nil
}

// batch fetches one batch of detail through query. A response without
// nodes fails, since it would read as everything having closed.
func batch[N any](ctx context.Context, f *Fetcher, query string, vars map[string]any, meta *Meta) ([]*N, error) {
	var resp struct {
		RateLimit gh.Budget `json:"rateLimit"`
		Nodes     []*N      `json:"nodes"`
	}
	if err := f.do(ctx, query, vars, &resp, meta); err != nil {
		return nil, err
	}
	if resp.Nodes == nil {
		return nil, errors.New("nodes missing from the response")
	}
	meta.saw(resp.RateLimit)
	return resp.Nodes, nil
}

func (f *Fetcher) prDetails(ctx context.Context, tags map[string][]model.Tag, login string, meta *Meta, c *counter) ([]model.PR, error) {
	var prs []model.PR
	for ids := range slices.Chunk(slices.Sorted(maps.Keys(tags)), f.prBatch) {
		nodes, err := batch[pullRequest](ctx, f, prDetailQuery, map[string]any{"ids": ids, "login": login}, meta)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			// search lags, so a pr merged a moment ago can still be listed
			if n != nil && n.ID != "" && n.State == "OPEN" {
				prs = append(prs, n.model(tags[n.ID]))
			}
		}
		c.add(len(ids))
	}
	return prs, nil
}

func (f *Fetcher) issueDetails(ctx context.Context, tags map[string][]model.Tag, meta *Meta, c *counter) ([]model.Issue, error) {
	var issues []model.Issue
	for ids := range slices.Chunk(slices.Sorted(maps.Keys(tags)), f.issueBatch) {
		nodes, err := batch[issue](ctx, f, issueDetailQuery, map[string]any{"ids": ids}, meta)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			// search lags, so an issue closed a moment ago can still be listed
			if n != nil && n.ID != "" && n.State == "OPEN" {
				issues = append(issues, n.model(tags[n.ID]))
			}
		}
		c.add(len(ids))
	}
	return issues, nil
}
