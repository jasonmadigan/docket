package model

import (
	"testing"
	"time"
)

func TestAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{45 * time.Second, "45s"},
		{12 * time.Minute, "12m"},
		{3 * time.Hour, "3h"},
		{24 * time.Hour, "1d"},
		{13 * day, "13d"},
		{14 * day, "2w"},
		{59 * day, "8w"},
		{60 * day, "2mo"},
		{364 * day, "12mo"},
		{365 * day, "1y"},
		{2960 * day, "8y"},
	}
	for _, c := range cases {
		if got := Age(c.d); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
