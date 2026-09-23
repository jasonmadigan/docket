package model

import (
	"fmt"
	"time"
)

const day = 24 * time.Hour

// Age renders d compactly: 45s, 12m, 3h, 5d, 3w, 4mo, 2y.
func Age(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", d/time.Second)
	case d < time.Hour:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d < day:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d < 14*day:
		return fmt.Sprintf("%dd", d/day)
	case d < 60*day:
		return fmt.Sprintf("%dw", d/(7*day))
	case d < 365*day:
		return fmt.Sprintf("%dmo", d/(30*day))
	}
	return fmt.Sprintf("%dy", d/(365*day))
}
