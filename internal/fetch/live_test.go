//go:build live

package fetch

import (
	"context"
	"testing"
	"time"

	"github.com/jasonmadigan/docket/internal/gh"
)

// go test -tags live -run Live -v ./internal/fetch/
func TestLive(t *testing.T) {
	client, err := gh.New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := New(client)
	v, meta, err := f.Viewer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("viewer %s, %d teams, budget %+v", v.Login, len(v.Teams), meta.Budget)
	res, err := f.Fetch(ctx, v.Login)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d PRs, warnings %q, budget %+v, spent %d", len(res.PRs), res.Warnings, res.Budget, meta.Budget.Remaining-res.Budget.Remaining)
	for _, pr := range res.PRs {
		if pr.ID == "" || pr.Repo == "" || pr.URL == "" || len(pr.Tags) == 0 {
			t.Errorf("incomplete PR: %+v", pr)
		}
	}
}
