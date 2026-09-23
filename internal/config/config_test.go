package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, Config{Poll: DefaultPoll}) {
		t.Fatalf("got %+v", cfg)
	}
}

func TestLoadReadsValues(t *testing.T) {
	cfg, err := Load(write(t, "poll = \"90s\"\nignore_actors = [\"openshift-ci-robot\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Poll: 90 * time.Second, IgnoreActors: []string{"openshift-ci-robot"}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"unknown key":  {"pol = \"60s\"\n", "unknown keys: pol"},
		"bad duration": {"poll = \"soon\"\n", "poll: time: invalid duration"},
		"too fast":     {"poll = \"1s\"\n", "poll must be at least 10s, got 1s"},
		"not toml":     {"poll = \n", "config.toml:"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	got, err := Path()
	if err != nil || got != "/x/docket/config.toml" {
		t.Fatalf("Path() = %q, %v", got, err)
	}
}

func TestPathDefaultsToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")
	got, err := Path()
	if err != nil || got != "/home/someone/.config/docket/config.toml" {
		t.Fatalf("Path() = %q, %v", got, err)
	}
}
