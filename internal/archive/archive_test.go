package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 24, 14, 2, 0, 0, time.UTC)

func newStore(t *testing.T, path string, ended ...string) (*Store, *[][]Entry) {
	t.Helper()
	var applied [][]Entry
	s := NewStore(path, nil, func(es []Entry) { applied = append(applied, es) }, func() []string { return ended })
	s.now = func() time.Time { return now }
	return s, &applied
}

func ids(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	es, err := Load(filepath.Join(t.TempDir(), "archive.toml"))
	if err != nil || es != nil {
		t.Fatalf("got %v, %v", es, err)
	}
}

func TestPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got, _ := Path(); got != "/tmp/xdg/docket/archive.toml" {
		t.Fatalf("Path() = %q", got)
	}
}

func TestSaveThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	want := []Entry{
		{ID: "I_1", Ref: "acme/a#1", Title: "one", At: now},
		{ID: "PR_2", Ref: "acme/a#2", Title: "two", At: now.Add(time.Hour)},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(raw), "# archived in docket") {
		t.Fatalf("file lacks its header: %v\n%s", err, raw)
	}
	got, err := Load(path)
	if err != nil || !same(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"unknown key":  "[[item]]\nid = \"I_1\"\nat = 2026-09-24T14:02:00Z\ncolour = \"red\"\n",
		"missing id":   "[[item]]\nat = 2026-09-24T14:02:00Z\n",
		"missing at":   "[[item]]\nid = \"I_1\"\n",
		"listed twice": "[[item]]\nid = \"I_1\"\nat = 2026-09-24T14:02:00Z\n[[item]]\nid = \"I_1\"\nat = 2026-09-24T15:02:00Z\n",
		"not toml":     "[[item\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "archive.toml")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("err = %v, want one naming %s", err, path)
			}
		})
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	s, applied := newStore(t, path)
	if err := s.Archive("I_1", "acme/a#1", "one"); err != nil {
		t.Fatal(err)
	}
	want := []Entry{{ID: "I_1", Ref: "acme/a#1", Title: "one", At: now}}
	if got, _ := Load(path); !same(got, want) {
		t.Fatalf("file = %+v", got)
	}
	if len(*applied) != 1 || !same((*applied)[0], want) || !same(s.Entries(), want) {
		t.Fatalf("applied %+v, entries %+v", *applied, s.Entries())
	}
	if err := s.Unarchive("I_1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); len(got) != 0 {
		t.Fatalf("file after unarchiving = %+v", got)
	}
	if err := s.Unarchive("I_1"); !errors.Is(err, ErrNotArchived) {
		t.Fatalf("unarchiving twice: err = %v", err)
	}
}

func TestArchiveTwiceKeepsTheFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	s, _ := newStore(t, path)
	if err := s.Archive("I_1", "acme/a#1", "one"); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now.Add(time.Hour) }
	if err := s.Archive("I_1", "acme/a#1", "one"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); len(got) != 1 || !got[0].At.Equal(now) {
		t.Fatalf("file = %+v", got)
	}
}

func TestStoresSharingAFileLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	a, _ := newStore(t, path)
	b, applied := newStore(t, path)
	if err := a.Archive("I_1", "acme/a#1", "one"); err != nil {
		t.Fatal(err)
	}
	if err := b.Archive("I_2", "acme/a#2", "two"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); !reflect.DeepEqual(ids(got), []string{"I_1", "I_2"}) {
		t.Fatalf("file = %+v", got)
	}
	if last := (*applied)[len(*applied)-1]; !reflect.DeepEqual(ids(last), []string{"I_1", "I_2"}) {
		t.Fatalf("b applied %+v, want both", last)
	}
}

func TestConcurrentWritersLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	stores := []*Store{NewStore(path, nil, func([]Entry) {}, nil), NewStore(path, nil, func([]Entry) {}, nil)}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stores[i%2].Archive(fmt.Sprintf("I_%d", i), "", ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got, _ := Load(path); len(got) != 20 {
		t.Fatalf("%d entries, want 20", len(got))
	}
}

func TestNeverReplacesAFileThatDoesNotParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	broken := []byte("[[item\nid = \"I_1\"\n")
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	s, applied := newStore(t, path)
	if err := s.Archive("I_2", "acme/a#2", "two"); err == nil {
		t.Fatal("archived into a file that doesn't parse")
	}
	if raw, _ := os.ReadFile(path); string(raw) != string(broken) || len(*applied) != 0 {
		t.Fatalf("file became %q, applied %+v", raw, *applied)
	}
}

func TestWritesDropEndedArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	if err := Save(path, []Entry{{ID: "I_1", At: now}, {ID: "I_2", At: now}}); err != nil {
		t.Fatal(err)
	}
	s, _ := newStore(t, path, "I_1")
	if err := s.Archive("I_3", "", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); !reflect.DeepEqual(ids(got), []string{"I_2", "I_3"}) {
		t.Fatalf("file = %+v", got)
	}
}

func TestWatchPicksUpEditsMadeElsewhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	s, applied := newStore(t, path)
	s.reload()
	if len(*applied) != 0 {
		t.Fatalf("nothing changed, applied %+v", *applied)
	}
	if err := Save(path, []Entry{{ID: "I_1", At: now}}); err != nil {
		t.Fatal(err)
	}
	s.reload()
	if len(*applied) != 1 || len((*applied)[0]) != 1 {
		t.Fatalf("applied %+v", *applied)
	}
	if err := os.WriteFile(path, []byte("[[item\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	s.reload()
	if len(*applied) != 1 || len(s.Entries()) != 1 {
		t.Fatalf("a broken edit was applied: %+v", *applied)
	}
}

// a hand-edited file keeps its entries through docket's next write, though
// only docket's own header comment survives it
func TestWriteKeepsTheEntriesOfAHandEditedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	hand := "# my notes\n\n[[item]]\n  id    = \"I_1\"   # the flaky one\n  at    = 2026-09-20T08:00:00Z\n\n\n" +
		"[[item]]\nid=\"I_2\"\nat=2026-09-21T08:00:00Z\nref=\"acme/a#2\"\n"
	if err := os.WriteFile(path, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := newStore(t, path)
	if err := s.Archive("I_3", "acme/a#3", "three"); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(ids(got), []string{"I_1", "I_2", "I_3"}) || got[1].Ref != "acme/a#2" {
		t.Fatalf("entries = %+v, %v", got, err)
	}
	if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "my notes") {
		t.Fatalf("the file was not rewritten:\n%s", raw)
	}
}

func TestArchiveCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh", "docket", "archive.toml")
	s, _ := newStore(t, path)
	if err := s.Archive("I_1", "acme/a#1", "one"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(path); len(got) != 1 {
		t.Fatalf("file = %+v", got)
	}
}

func TestTitlesSurviveTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.toml")
	title := "quote \" backslash \\ newline \n tab \t emoji 🚀 and [[item]] id = \"x\""
	s, _ := newStore(t, path)
	if err := s.Archive("I_1", "acme/a#1", title); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err != nil || len(got) != 1 || got[0].Title != title {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTimes(t *testing.T) {
	if got := Times([]Entry{{ID: "I_1", At: now}}); !reflect.DeepEqual(got, map[string]time.Time{"I_1": now}) {
		t.Fatalf("Times = %v", got)
	}
}
