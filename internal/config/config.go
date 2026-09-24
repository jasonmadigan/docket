package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	DefaultPoll = 60 * time.Second
	MinPoll     = 10 * time.Second
	MaxPoll     = time.Hour
)

// PollChoices are what the settings screens offer.
var PollChoices = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}

var login = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$`)

type Config struct {
	Poll         time.Duration
	IgnoreActors []string
}

type file struct {
	Poll         string   `toml:"poll"`
	IgnoreActors []string `toml:"ignore_actors"`
}

// Dir is where docket keeps its files: $XDG_CONFIG_HOME/docket, else
// ~/.config/docket.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "docket"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "docket"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads path; a missing file means defaults.
func Load(path string) (Config, error) {
	cfg := Config{Poll: DefaultPoll}
	var f file
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return Config{}, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	if f.Poll != "" {
		if cfg.Poll, err = time.ParseDuration(f.Poll); err != nil {
			return Config{}, fmt.Errorf("%s: poll: %w", path, err)
		}
	}
	cfg.IgnoreActors = f.IgnoreActors
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Poll < MinPoll {
		return fmt.Errorf("poll must be at least %s, got %s", MinPoll, c.Poll)
	}
	if c.Poll > MaxPoll {
		return fmt.Errorf("poll must be at most %s, got %s", MaxPoll, c.Poll)
	}
	for _, a := range c.IgnoreActors {
		if !login.MatchString(a) {
			return fmt.Errorf("%q isn't a GitHub login", a)
		}
	}
	return nil
}

// FormatPoll writes d the way a person would: 30s, 1m, 1m30s.
func FormatPoll(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	return strings.TrimPrefix(s, "0h")
}

// Save writes c to path, creating its directory. It replaces the file in
// one step, so a reader never sees half of it.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(file{Poll: FormatPoll(c.Poll), IgnoreActors: c.IgnoreActors}); err != nil {
		return err
	}
	return WriteAtomic(path, buf.Bytes())
}

// WriteAtomic replaces path with data in one step, so a reader never sees
// half a file, creating its directory as needed.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Store holds the settings in use, saves changes to disk and hands them to
// apply.
type Store struct {
	path  string
	apply func(Config)

	mu   sync.Mutex
	cur  Config
	seen time.Time // file mtime as of the last load or save
}

func NewStore(path string, cur Config, apply func(Config)) *Store {
	return &Store{path: path, apply: apply, cur: cur, seen: ModTime(path)}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cur
	c.IgnoreActors = slices.Clone(c.IgnoreActors)
	return c
}

func (s *Store) Set(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := Save(s.path, c); err != nil {
		return err
	}
	s.cur, s.seen = c, ModTime(s.path)
	s.apply(c)
	return nil
}

// Watch applies edits made elsewhere, by hand or by another docket, until
// ctx ends.
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
	mod := ModTime(s.path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if mod.Equal(s.seen) {
		return
	}
	s.seen = mod
	c, err := Load(s.path)
	if err != nil {
		return // a broken edit waits for the next one
	}
	s.cur = c
	s.apply(c)
}

// ModTime is path's modification time, zero when it can't be read.
func ModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
