package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	DefaultPoll = 60 * time.Second
	MinPoll     = 10 * time.Second
)

type Config struct {
	Poll         time.Duration
	IgnoreActors []string
}

type file struct {
	Poll         string   `toml:"poll"`
	IgnoreActors []string `toml:"ignore_actors"`
}

func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "docket", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "docket", "config.toml"), nil
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
	return nil
}
