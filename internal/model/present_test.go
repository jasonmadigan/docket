package model

import (
	"reflect"
	"testing"
)

func TestRef(t *testing.T) {
	if got := (PR{Repo: "acme/a", Number: 7}).Ref(); got != "acme/a#7" {
		t.Fatalf("Ref() = %q", got)
	}
}

func TestReviewers(t *testing.T) {
	pr := PR{
		Requests: []Reviewer{{Name: "bob"}, {Name: "acme/devs", Team: true}},
		Opinions: []Review{
			{Author: user("carol"), State: "APPROVED"},
			{Author: user("dave"), State: "CHANGES_REQUESTED"},
		},
	}
	want := []ReviewerState{
		{Name: "bob", State: "review requested"},
		{Name: "acme/devs", State: "review requested"},
		{Name: "carol", State: "approved"},
		{Name: "dave", State: "requested changes"},
	}
	if got := Reviewers(pr); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
