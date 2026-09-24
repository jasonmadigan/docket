// Package archive keeps the items archived in docket: hidden here,
// untouched on github.
package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/jasonmadigan/docket/internal/config"
)

// Entry is one archived item. Ref and Title are for people reading the
// file; views show live data.
type Entry struct {
	ID    string    `toml:"id"`
	Ref   string    `toml:"ref"`
	Title string    `toml:"title"`
	At    time.Time `toml:"at"`
}

var ErrNotArchived = errors.New("not archived")

type file struct {
	Item []Entry `toml:"item"`
}

const header = "# archived in docket; delete an entry to unarchive it\n\n"

func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "archive.toml"), nil
}

// Load reads path; a missing file archives nothing.
func Load(path string) ([]Entry, error) {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	seen := map[string]bool{}
	for i, e := range f.Item {
		switch {
		case e.ID == "":
			return nil, fmt.Errorf("%s: item %d has no id", path, i+1)
		case e.At.IsZero():
			return nil, fmt.Errorf("%s: %s has no at", path, e.ID)
		case seen[e.ID]:
			return nil, fmt.Errorf("%s: %s is listed twice", path, e.ID)
		}
		seen[e.ID] = true
	}
	return f.Item, nil
}

// Save writes entries to path, replacing it in one step.
func Save(path string, entries []Entry) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(file{Item: entries}); err != nil {
		return err
	}
	return config.WriteAtomic(path, buf.Bytes())
}

// Times maps each archived id to when it was archived, as the engine takes
// it.
func Times(entries []Entry) map[string]time.Time {
	out := make(map[string]time.Time, len(entries))
	for _, e := range entries {
		out[e.ID] = e.At
	}
	return out
}

// Store holds the archive in use, saves changes and hands them to apply.
// It writes only when someone archives or unarchives, never in the
// background, where hosts sharing the file by sync would race.
type Store struct {
	path  string
	apply func([]Entry)
	ended func() []string // ids whose archive has ended: closed, merged, gone or reopened
	now   func() time.Time

	mu   sync.Mutex
	cur  []Entry
	seen time.Time // file mtime as of the last load or save
}

func NewStore(path string, cur []Entry, apply func([]Entry), ended func() []string) *Store {
	if ended == nil {
		ended = func() []string { return nil }
	}
	return &Store{path: path, apply: apply, ended: ended, now: time.Now, cur: cur, seen: config.ModTime(path)}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.cur)
}

// Archive adds id, unless it is archived already.
func (s *Store) Archive(id, ref, title string) error {
	return s.change(func(es []Entry) ([]Entry, error) {
		if slices.ContainsFunc(es, func(e Entry) bool { return e.ID == id }) {
			return es, nil
		}
		return append(es, Entry{ID: id, Ref: ref, Title: title, At: s.now().UTC().Truncate(time.Second)}), nil
	})
}

func (s *Store) Unarchive(id string) error {
	return s.change(func(es []Entry) ([]Entry, error) {
		i := slices.IndexFunc(es, func(e Entry) bool { return e.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("%s: %w", id, ErrNotArchived)
		}
		return slices.Delete(es, i, i+1), nil
	})
}

// change edits the file as it stands on disk, holding a lock every docket
// on this host takes, so none loses another's change. Entries whose
// archive has ended go at the same time.
func (s *Store) change(edit func([]Entry) ([]Entry, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := lockDir(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer unlock()
	cur, err := Load(s.path)
	if err != nil {
		return err
	}
	ended := s.ended()
	next, err := edit(slices.DeleteFunc(slices.Clone(cur), func(e Entry) bool { return slices.Contains(ended, e.ID) }))
	if err != nil {
		return err
	}
	if !same(next, cur) {
		if err := Save(s.path, next); err != nil {
			return err
		}
	}
	s.cur, s.seen = next, config.ModTime(s.path)
	s.apply(slices.Clone(next))
	return nil
}

func same(a, b []Entry) bool {
	return slices.EqualFunc(a, b, func(x, y Entry) bool {
		return x.ID == y.ID && x.Ref == y.Ref && x.Title == y.Title && x.At.Equal(y.At)
	})
}

// Watch applies edits made elsewhere, by hand, by another docket or by
// sync, until ctx ends.
func (s *Store) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reload()
		}
	}
}

func (s *Store) reload() {
	mod := config.ModTime(s.path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if mod.Equal(s.seen) {
		return
	}
	s.seen = mod
	es, err := Load(s.path)
	if err != nil {
		return // a broken edit waits for the next one
	}
	s.cur = es
	s.apply(slices.Clone(es))
}
