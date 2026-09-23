package model

import "fmt"

func (pr PR) Ref() string {
	return fmt.Sprintf("%s#%d", pr.Repo, pr.Number)
}

type ReviewerState struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// Reviewers lists pending requests, then submitted verdicts.
func Reviewers(pr PR) []ReviewerState {
	var out []ReviewerState
	for _, r := range pr.Requests {
		out = append(out, ReviewerState{Name: r.Name, State: "review requested"})
	}
	for _, r := range pr.Opinions {
		out = append(out, ReviewerState{Name: r.Author.Login, State: verb(r.State)})
	}
	return out
}
