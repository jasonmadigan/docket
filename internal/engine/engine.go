package engine

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jasonmadigan/docket/internal/fetch"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

type Source interface {
	Viewer(ctx context.Context) (fetch.Viewer, fetch.Meta, error)
	Fetch(ctx context.Context, login string, archived []string, progress func(fetch.Progress)) (fetch.Result, error)
}

type Config struct {
	Poll        time.Duration
	Ignore      []string
	Backoff     []time.Duration // wait after each consecutive failure; the last step repeats
	ViewerEvery time.Duration   // how often login and teams are refetched
	Timeout     time.Duration   // one poll; each request also has its own 30s limit
	Now         func() time.Time
}

type State struct {
	Snapshot model.Snapshot
	Loaded   bool
	Updated  time.Time
	Next     time.Time
	Budget   gh.Budget
	Err      error // the last poll's; nil once one succeeds
	Warnings []string
	Busy     bool
	Progress fetch.Progress
	Changed  []string // prs new or different in the latest poll; none after the first
	gen      int      // the archive change Snapshot was built with
}

type Engine struct {
	src     Source
	cfg     Config
	refresh chan struct{}

	mu    sync.Mutex
	state State
	subs  map[chan State]struct{}

	// the last good poll and who it was for, so an archive change can
	// rebuild the snapshot without polling
	last    fetch.Result
	lastFor fetch.Viewer
	archive map[string]time.Time
	gen     int // counts archive changes
	gone    []string
	void    []string

	// owned by the polling goroutine
	viewer         fetch.Viewer
	viewerAt       time.Time
	viewerWarnings []string
	failures       int
	limited        time.Time
	prints         map[string]string // row fingerprints from the last poll
}

func New(src Source, cfg Config) *Engine {
	if cfg.Poll <= 0 {
		cfg.Poll = time.Minute
	}
	if len(cfg.Backoff) == 0 {
		cfg.Backoff = []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute}
	}
	if cfg.ViewerEvery <= 0 {
		cfg.ViewerEvery = time.Hour
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Engine{src: src, cfg: cfg, refresh: make(chan struct{}, 1), subs: map[chan State]struct{}{}}
}

func (e *Engine) Current() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Subscribe delivers the current state at once, then each new one. A slow
// reader skips states rather than holding the engine up.
func (e *Engine) Subscribe() (<-chan State, func()) {
	ch := make(chan State, 1)
	e.mu.Lock()
	defer e.mu.Unlock()
	ch <- e.state
	e.subs[ch] = struct{}{}
	return ch, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		delete(e.subs, ch)
	}
}

// Configure changes the poll interval and the accounts that don't count as
// people, then polls so the change shows at once.
func (e *Engine) Configure(poll time.Duration, ignore []string) {
	e.mu.Lock()
	if poll > 0 {
		e.cfg.Poll = poll
	}
	e.cfg.Ignore = slices.Clone(ignore)
	e.mu.Unlock()
	e.Refresh()
}

func (e *Engine) settings() (time.Duration, []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.Poll, e.cfg.Ignore
}

// Refresh polls now, unless github has asked us to wait.
func (e *Engine) Refresh() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

func (e *Engine) Once(ctx context.Context) (State, error) {
	var st State
	if err := e.poll(ctx, e.cfg.Now(), &st, nil); err != nil {
		return State{}, err
	}
	return st, nil
}

// SetArchive swaps in the archive and, once a poll has landed, rebuilds
// the snapshot from it at once, without polling.
func (e *Engine) SetArchive(at map[string]time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.archive = maps.Clone(at)
	e.gen++
	if e.state.Loaded {
		st := e.state
		st.Changed = nil
		e.send(st)
	}
}

// Prunable lists archived ids whose archive has ended: closed, merged or
// gone from github, or reopened since. The archive drops them when next
// written.
func (e *Engine) Prunable() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := slices.Concat(e.gone, e.void)
	slices.Sort(out)
	return slices.Compact(out)
}

func (e *Engine) archivedIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Sorted(maps.Keys(e.archive))
}

// build files the last poll's items with the archive as it stands; the
// caller holds mu.
func (e *Engine) build(now time.Time) model.Snapshot {
	snap := model.Build(e.last.PRs, e.last.Issues, model.Params{
		Login: e.lastFor.Login, Teams: e.lastFor.Teams, Ignore: e.cfg.Ignore, Archive: e.archive, Now: now,
	})
	e.void = snap.Void
	return snap
}

// Run polls until ctx ends. It returns early only when the token is
// rejected, which no amount of waiting fixes.
func (e *Engine) Run(ctx context.Context) error {
	next := e.cfg.Now()
	for {
		timer := time.NewTimer(max(next.Sub(e.cfg.Now()), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-e.refresh:
			timer.Stop()
			if e.cfg.Now().Before(e.limited) {
				continue
			}
		case <-timer.C:
		}
		var err error
		if next, err = e.tick(ctx); err != nil {
			return err
		}
	}
}

func (e *Engine) tick(ctx context.Context) (time.Time, error) {
	now := e.cfg.Now()
	st := e.Current()
	err := e.poll(ctx, now, &st, e.archivedIDs())
	switch {
	case ctx.Err() != nil:
		return now, nil
	case err == nil:
		e.failures = 0
		st.Err = nil
		st.Next = e.after(now, st.Budget)
	case errors.Is(err, gh.ErrUnauthorised):
		st.Err = err
		e.publish(st)
		return time.Time{}, err
	default:
		st.Err = err
		st.Next = e.retry(now, err, st.Budget)
	}
	e.publish(st)
	return st.Next, nil
}

func (e *Engine) poll(ctx context.Context, now time.Time, st *State, lookup []string) error {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	busy := func(p fetch.Progress) {
		s := *st
		s.Busy, s.Progress, s.Changed = true, p, nil
		e.publish(s)
	}
	if e.viewer.Login == "" || now.Sub(e.viewerAt) >= e.cfg.ViewerEvery {
		busy(fetch.Progress{Phase: "checking account"})
		v, meta, err := e.src.Viewer(ctx)
		if err != nil {
			return err
		}
		e.viewer, e.viewerAt, e.viewerWarnings = v, now, meta.Warnings
		keepBudget(st, meta.Budget)
	}
	res, err := e.src.Fetch(ctx, e.viewer.Login, lookup, busy)
	if err != nil {
		return err
	}
	keepBudget(st, res.Budget)
	e.mu.Lock()
	e.last, e.lastFor, e.gone = res, e.viewer, res.Gone
	st.Snapshot, st.gen = e.build(now), e.gen
	e.mu.Unlock()
	st.Changed = e.changed(st.Snapshot)
	st.Loaded = true
	st.Updated = now
	st.Warnings = slices.Concat(e.viewerWarnings, res.Warnings)
	return nil
}

func keepBudget(st *State, b gh.Budget) {
	if b.Limit > 0 {
		st.Budget = b
	}
}

// after waits for the reset once under a tenth of the budget is left, since
// every tool using the token shares it.
func (e *Engine) after(now time.Time, b gh.Budget) time.Time {
	poll, _ := e.settings()
	next := now.Add(poll)
	if b.Limit > 0 && b.Remaining*10 < b.Limit && b.ResetAt.After(next) {
		return b.ResetAt.Add(time.Second)
	}
	return next
}

func (e *Engine) retry(now time.Time, err error, b gh.Budget) time.Time {
	var limited *gh.RateLimitedError
	if errors.As(err, &limited) {
		until := limited.Until
		if until.IsZero() {
			until = b.ResetAt
		}
		if !until.After(now) {
			until = now.Add(e.cfg.Backoff[0])
		}
		e.limited = until
		return until
	}
	wait := e.cfg.Backoff[min(e.failures, len(e.cfg.Backoff)-1)]
	e.failures++
	return now.Add(wait)
}

func (e *Engine) publish(st State) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.send(st)
}

// send makes st current and hands it to subscribers; the caller holds mu.
// A snapshot built before the latest archive change is rebuilt first, so
// an archive made mid-poll is never undone.
func (e *Engine) send(st State) {
	if st.Loaded && st.gen != e.gen {
		st.Snapshot, st.gen = e.build(st.Snapshot.At), e.gen
	}
	e.state = st
	for ch := range e.subs {
		select {
		case <-ch:
		default:
		}
		ch <- st
	}
}

// changed lists items that are new or look different since the last poll.
// The first poll is the baseline and reports none.
func (e *Engine) changed(s model.Snapshot) []string {
	prints := map[string]string{}
	var out []string
	for _, l := range []model.List{s.PRs, s.Issues, s.Archived} {
		for _, sec := range l.Sections {
			for _, r := range sec.Rows {
				id, fp := r.Item().ID, r.Fingerprint()
				prints[id] = fp
				if e.prints != nil && e.prints[id] != fp {
					out = append(out, id)
				}
			}
		}
	}
	e.prints = prints
	return out
}
