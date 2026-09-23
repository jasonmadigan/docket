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

func TestSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "docket", "config.toml")
	want := Config{Poll: 2 * time.Minute, IgnoreActors: []string{"openshift-ci-robot", "renovate[bot]"}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `poll = "2m"`) {
		t.Fatalf("file reads:\n%s", body)
	}
}

func TestValidateRejects(t *testing.T) {
	for name, c := range map[string]Config{
		"too slow":  {Poll: 2 * time.Hour},
		"bad login": {Poll: time.Minute, IgnoreActors: []string{"not a login"}},
		"empty":     {Poll: time.Minute, IgnoreActors: []string{""}},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: %+v passed", name, c)
		}
	}
	if err := Save(filepath.Join(t.TempDir(), "c.toml"), Config{Poll: time.Second}); err == nil {
		t.Error("Save wrote an invalid config")
	}
}

func TestFormatPoll(t *testing.T) {
	for d, want := range map[time.Duration]string{30 * time.Second: "30s", time.Minute: "1m", 90 * time.Second: "1m30s", 10 * time.Minute: "10m"} {
		if got := FormatPoll(d); got != want {
			t.Errorf("FormatPoll(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestStoreSetSavesAndApplies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var applied []Config
	s := NewStore(path, Config{Poll: DefaultPoll}, func(c Config) { applied = append(applied, c) })
	want := Config{Poll: 5 * time.Minute, IgnoreActors: []string{"codecov"}}
	if err := s.Set(want); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved %+v", got)
	}
	if !reflect.DeepEqual(applied, []Config{want}) || !reflect.DeepEqual(s.Get(), want) {
		t.Fatalf("applied %+v, current %+v", applied, s.Get())
	}
	if err := s.Set(Config{Poll: time.Second}); err == nil || len(applied) != 1 {
		t.Fatalf("invalid settings: err %v, applied %d times", err, len(applied))
	}
}

func TestStorePicksUpEditsMadeElsewhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var applied []Config
	s := NewStore(path, Config{Poll: DefaultPoll}, func(c Config) { applied = append(applied, c) })
	s.reload()
	if len(applied) != 0 {
		t.Fatalf("nothing changed, applied %+v", applied)
	}
	if err := os.WriteFile(path, []byte("poll = \"2m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.reload()
	if len(applied) != 1 || applied[0].Poll != 2*time.Minute {
		t.Fatalf("applied %+v", applied)
	}
	if err := os.WriteFile(path, []byte("poll = \"soon\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	s.reload()
	if len(applied) != 1 || s.Get().Poll != 2*time.Minute {
		t.Fatalf("a broken edit was applied: %+v", applied)
	}
}
