package model

import (
	"reflect"
	"testing"
)

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

func TestReviewersNameDeletedAccounts(t *testing.T) {
	got := Reviewers(PR{Opinions: []Review{{State: "APPROVED"}}})
	if want := []ReviewerState{{Name: "ghost", State: "approved"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
