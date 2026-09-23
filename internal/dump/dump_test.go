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
	if back.Count != 5 || len(back.Sections) != 3 || back.Sections[1].Rows[0].PR.Ref() != "acme/gateway#1188" {
		t.Fatalf("round trip = %+v", back)
	}
}
