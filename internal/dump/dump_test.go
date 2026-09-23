package dump

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/jasonmadigan/docket/internal/fixture"
	"github.com/jasonmadigan/docket/internal/model"
)

func TestText(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, fixture.State().Snapshot); err != nil {
		t.Fatal(err)
	}
	golden.RequireEqual(t, buf.String())
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
