package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPipedTextIsPlain(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "")
	t.Setenv("TTY_FORCE", "")
	var buf bytes.Buffer
	if _, err := textOut(&buf).Write([]byte("\x1b[31mfail\x1b[m \x1b]8;;https://example.com\x07link\x1b]8;;\x07\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); strings.Contains(got, "\x1b") || got != "fail link\n" {
		t.Fatalf("piped output = %q", got)
	}
}
