package dump

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jasonmadigan/docket/internal/model"
)

func JSON(w io.Writer, s model.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// Text prints a line per pr, then its tags and status beneath.
func Text(w io.Writer, s model.Snapshot) error {
	var b strings.Builder
	for i, sec := range s.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s (%d)\n", sec.Name, len(sec.Rows))
		for _, r := range sec.Rows {
			fmt.Fprintf(&b, "  %-7s %4s  %s  %s\n", r.CI, s.Since(r.Activity), r.PR.Ref(), r.PR.Title)
			facts := []string{strings.Join(r.Labels(), ", ")}
			for _, l := range slices.Concat(r.Left, r.Mine) {
				facts = append(facts, l.Text)
			}
			fmt.Fprintf(&b, "  %-7s %4s  %s\n", "", "", strings.Join(facts, " · "))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
