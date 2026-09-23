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

type Transport interface {
	Do(ctx context.Context, query string, vars map[string]any, out any) error
}

type Fetcher struct {
	t     Transport
	batch int
}

func New(t Transport) *Fetcher {
	return &Fetcher{t: t, batch: 20}
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
	PRs []model.PR
	Meta
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

func (f *Fetcher) Fetch(ctx context.Context, login string) (Result, error) {
	var res Result
	tags, err := f.discover(ctx, &res.Meta)
	if err != nil {
		return Result{}, fmt.Errorf("discovery: %w", err)
	}
	if res.PRs, err = f.details(ctx, tags, login, &res.Meta); err != nil {
		return Result{}, fmt.Errorf("details: %w", err)
	}
	return res, nil
}

func (f *Fetcher) discover(ctx context.Context, meta *Meta) (map[string][]model.Tag, error) {
	var resp map[string]json.RawMessage
	if err := f.do(ctx, discoveryQuery, nil, &resp, meta); err != nil {
		return nil, err
	}
	var budget gh.Budget
	if raw, ok := resp["rateLimit"]; ok {
		if err := json.Unmarshal(raw, &budget); err != nil {
			return nil, err
		}
	}
	meta.saw(budget)
	found := map[string]map[string]bool{}
	for _, q := range qualifiers {
		raw, ok := resp[q.alias]
		if !ok || string(raw) == "null" {
			return nil, fmt.Errorf("%s: missing from the response", q.alias)
		}
		var sr searchResult
		if err := json.Unmarshal(raw, &sr); err != nil {
			return nil, fmt.Errorf("%s: %w", q.alias, err)
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
			if sr, err = f.page(ctx, scope+q.query, sr.PageInfo.EndCursor, meta); err != nil {
				return nil, fmt.Errorf("%s: %w", q.alias, err)
			}
		}
		found[q.alias] = ids
	}
	tags := map[string][]model.Tag{}
	for _, q := range qualifiers {
		for id := range found[q.alias] {
			switch {
			case q.tag != "":
				tags[id] = append(tags[id], q.tag)
			case !found["review"][id]:
				tags[id] = append(tags[id], model.TagTeam)
			}
		}
	}
	return tags, nil
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

func (f *Fetcher) details(ctx context.Context, tags map[string][]model.Tag, login string, meta *Meta) ([]model.PR, error) {
	var prs []model.PR
	for ids := range slices.Chunk(slices.Sorted(maps.Keys(tags)), f.batch) {
		var resp struct {
			RateLimit gh.Budget      `json:"rateLimit"`
			Nodes     []*pullRequest `json:"nodes"`
		}
		if err := f.do(ctx, detailQuery, map[string]any{"ids": ids, "login": login}, &resp, meta); err != nil {
			return nil, err
		}
		if resp.Nodes == nil {
			return nil, errors.New("nodes missing from the response")
		}
		meta.saw(resp.RateLimit)
		for _, n := range resp.Nodes {
			if n != nil && n.ID != "" {
				prs = append(prs, n.model(tags[n.ID]))
			}
		}
	}
	return prs, nil
}
