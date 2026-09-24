package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsBadInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown command": {[]string{"bogus"}, `unknown command "bogus"`},
		"stray argument":  {[]string{"dump", "extra"}, "unexpected arguments: extra"},
		"poll too fast":   {[]string{"dump", "-poll", "1s"}, "poll must be at least 10s"},
		"unknown flag":    {[]string{"dump", "-nope"}, "flag provided but not defined: -nope"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := run(context.Background(), c.args, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"help"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "usage: docket") {
		t.Fatalf("help = %q", out.String())
	}
	var flags bytes.Buffer
	if err := run(context.Background(), []string{"dump", "-h"}, io.Discard, &flags); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flags.String(), "-json") {
		t.Fatalf("dump -h = %q", flags.String())
	}
}

func TestRunRefusesABrokenArchive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "docket", "archive.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[[item\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"dump"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want one naming %s", err, path)
	}
}
