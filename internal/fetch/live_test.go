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
	res, err := f.Fetch(ctx, v.Login, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d PRs, warnings %q, budget %+v, spent %d", len(res.PRs), res.Warnings, res.Budget, meta.Budget.Remaining-res.Budget.Remaining)
	for _, pr := range res.PRs {
		if pr.ID == "" || pr.Repo == "" || pr.URL == "" || len(pr.Tags) == 0 || pr.CreatedAt.IsZero() {
			t.Errorf("incomplete PR: %+v", pr)
		}
	}
	t.Logf("%d issues", len(res.Issues))
	for _, is := range res.Issues {
		if is.ID == "" || is.Repo == "" || is.URL == "" || len(is.Tags) == 0 || is.CreatedAt.IsZero() {
			t.Errorf("incomplete issue: %+v", is)
		}
	}
	if len(res.Issues) > 0 {
		gone, err := f.gone(ctx, []string{res.Issues[0].ID}, &res.Meta)
		if err != nil {
			t.Fatal(err)
		}
		if len(gone) != 0 {
			t.Errorf("an open issue counted as gone: %v", gone)
		}
	}
	var out bytes.Buffer
	snap := model.Build(res.PRs, res.Issues, model.Params{Login: v.Login, Teams: v.Teams, Now: time.Now()})
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
			Type    string `json:"type"`
			Message string `json:"message"`
			Path    []any  `json:"path"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return fmt.Errorf("gh api graphql: %v: %s", runErr, stderr.String())
	}
	var msgs []string
	var problems []gh.Problem
	for _, e := range resp.Errors {
		msgs = append(msgs, e.Message)
		problems = append(problems, gh.Problem{Type: e.Type, Message: e.Message, Path: e.Path})
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return fmt.Errorf("github: %s", strings.Join(msgs, "; "))
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return err
	}
	if len(msgs) > 0 {
		return &gh.PartialError{Messages: msgs, Problems: problems}
	}
	return nil
}
