package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestItemRef(t *testing.T) {
	if got := (Item{Repo: "acme/a", Number: 7}).Ref(); got != "acme/a#7" {
		t.Fatalf("Ref() = %q", got)
	}
}

// scripts read pr.id from dump --json, never pr.Item.id
func TestPRJSONStaysFlat(t *testing.T) {
	pr := PR{Item: Item{ID: "PR_1", Repo: "acme/a", Number: 7}, Draft: true}
	raw, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{`"id":"PR_1"`, `"repo":"acme/a"`, `"number":7`, `"draft":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("json lacks %s: %s", want, got)
		}
	}
	if strings.Contains(got, "Item") {
		t.Errorf("json nests Item: %s", got)
	}
}

func TestRowItemIsItsPR(t *testing.T) {
	pr := PR{Item: Item{ID: "PR_1"}}
	if got := (Row{PR: &pr}).Item().ID; got != "PR_1" {
		t.Fatalf("Item().ID = %q", got)
	}
}
