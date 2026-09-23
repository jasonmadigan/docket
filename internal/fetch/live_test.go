//go:build live

package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jasonmadigan/docket/internal/dump"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

// go test -tags live -run Live -v ./internal/fetch/
//
// DOCKET_LIVE_VIA=gh sends the queries through the gh cli instead, for when
// a firewall holds this test binary's own connections.
func TestLive(t *testing.T) {
	var transport Transport = ghCLI{}
	if os.Getenv("DOCKET_LIVE_VIA") != "gh" {
		client, err := gh.New()
		if err != nil {
			t.Fatal(err)
		}
		transport = client
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := New(transport)
	v, meta, err := f.Viewer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("viewer %s, %d teams, budget %+v", v.Login, len(v.Teams), meta.Budget)
	res, err := f.Fetch(ctx, v.Login, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d PRs, warnings %q, budget %+v, spent %d", len(res.PRs), res.Warnings, res.Budget, meta.Budget.Remaining-res.Budget.Remaining)
	for _, pr := range res.PRs {
		if pr.ID == "" || pr.Repo == "" || pr.URL == "" || len(pr.Tags) == 0 || pr.CreatedAt.IsZero() {
			t.Errorf("incomplete PR: %+v", pr)
		}
	}
	var out bytes.Buffer
	snap := model.Build(res.PRs, model.Params{Login: v.Login, Teams: v.Teams, Now: time.Now()})
	if err := dump.Text(&out, snap); err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot:\n%s", out.String())
}

// ghCLI runs queries through gh api graphql, mapping errors as gh.Client
// does.
type ghCLI struct{}

func (ghCLI) Do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "graphql", "--input", "-")
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return fmt.Errorf("gh api graphql: %v: %s", runErr, stderr.String())
	}
	var msgs []string
	for _, e := range resp.Errors {
		msgs = append(msgs, e.Message)
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return fmt.Errorf("github: %s", strings.Join(msgs, "; "))
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return err
	}
	if len(msgs) > 0 {
		return &gh.PartialError{Messages: msgs}
	}
	return nil
}
