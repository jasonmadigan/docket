package engine

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jasonmadigan/docket/internal/fetch"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

type Source interface {
	Viewer(ctx context.Context) (fetch.Viewer, fetch.Meta, error)
	Fetch(ctx context.Context, login string) (fetch.Result, error)
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
}

type Engine struct {
	src     Source
	cfg     Config
	refresh chan struct{}

	mu    sync.Mutex
	state State
	subs  map[chan State]struct{}

	// owned by the polling goroutine
	viewer         fetch.Viewer
	viewerAt       time.Time
	viewerWarnings []string
	failures       int
	limited        time.Time
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

// Refresh polls now, unless github has asked us to wait.
func (e *Engine) Refresh() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

func (e *Engine) Once(ctx context.Context) (State, error) {
	var st State
	if err := e.poll(ctx, e.cfg.Now(), &st); err != nil {
		return State{}, err
	}
	return st, nil
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
	err := e.poll(ctx, now, &st)
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

func (e *Engine) poll(ctx context.Context, now time.Time, st *State) error {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	if e.viewer.Login == "" || now.Sub(e.viewerAt) >= e.cfg.ViewerEvery {
		v, meta, err := e.src.Viewer(ctx)
		if err != nil {
			return err
		}
		e.viewer, e.viewerAt, e.viewerWarnings = v, now, meta.Warnings
		keepBudget(st, meta.Budget)
	}
	res, err := e.src.Fetch(ctx, e.viewer.Login)
	if err != nil {
		return err
	}
	keepBudget(st, res.Budget)
	st.Snapshot = model.Build(res.PRs, model.Params{
		Login: e.viewer.Login, Teams: e.viewer.Teams, Ignore: e.cfg.Ignore, Now: now,
	})
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
	next := now.Add(e.cfg.Poll)
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
	e.state = st
	for ch := range e.subs {
		select {
		case <-ch:
		default:
		}
		ch <- st
	}
}
