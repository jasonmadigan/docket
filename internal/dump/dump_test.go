package dump

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/jasonmadigan/docket/internal/fixture"
	"github.com/jasonmadigan/docket/internal/model"
)

func TestText(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, fixture.State().Snapshot); err != nil {
		t.Fatal(err)
	}
	golden.RequireEqual(t, ansi.Strip(buf.String()))
}

func TestTextIsStyledAndLinked(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, fixture.State().Snapshot); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"\x1b]8;;https://github.com/acme/widgets/pull/42",
		"\x1b]8;;https://github.com/acme/widgets/issues/7",
		"\x1b]8;;https://github.com/acme/docs/pull/91",
		"\x1b]8;;https://ci.example/e2e",
		"\x1b[",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, fixture.State().Snapshot); err != nil {
		t.Fatal(err)
	}
	var back model.Snapshot
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back.PRs.Count != 5 || len(back.PRs.Sections) != 3 || back.PRs.Sections[1].Rows[0].PR.Ref() != "acme/gateway#1188" {
		t.Fatalf("round trip = %+v", back)
	}
	if back.Issues.Count != 3 || back.Issues.Sections[1].Name != "Assigned" || back.Issues.Sections[1].Rows[0].Issue.Ref() != "acme/widgets#7" {
		t.Fatalf("issues round trip = %+v", back.Issues)
	}
	if back.Archived.Count != 1 || back.Archived.Sections[0].Rows[0].Archived.IsZero() {
		t.Fatalf("archived round trip = %+v", back.Archived)
	}
}

func TestTextCountsArchivedItems(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, fixture.State().Snapshot); err != nil {
		t.Fatal(err)
	}
	out := ansi.Strip(buf.String())
	if !strings.HasSuffix(out, "\n1 archived\n") || strings.Contains(out, "acme/widgets#3 ") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestTextSkipsEmptyLists(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, model.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("an empty snapshot printed %q", buf.String())
	}
	snap := fixture.State().Snapshot
	snap.PRs = model.List{}
	buf.Reset()
	if err := Text(&buf, snap); err != nil {
		t.Fatal(err)
	}
	if out := ansi.Strip(buf.String()); strings.Contains(out, "Pull requests") || !strings.HasPrefix(out, "Issues\n") {
		t.Fatalf("issues only:\n%s", out)
	}
}
