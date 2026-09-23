package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jasonmadigan/docket/internal/fetch"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/model"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type outcome struct {
	res fetch.Result
	err error
}

// gated hands out one fetch result per give, so each poll is observed.
type gated struct {
	results chan outcome
}

func (g *gated) Viewer(context.Context) (fetch.Viewer, fetch.Meta, error) {
	return fetch.Viewer{Login: "me"}, fetch.Meta{}, nil
}

func (g *gated) Fetch(ctx context.Context, _ string, progress func(fetch.Progress)) (fetch.Result, error) {
	progress(fetch.Progress{Phase: "details", Done: 1, Total: 2})
	select {
	case o := <-g.results:
		return o.res, o.err
	case <-ctx.Done():
		return fetch.Result{}, ctx.Err()
	}
}

func (g *gated) give(t *testing.T, o outcome) {
	t.Helper()
	select {
	case g.results <- o:
	case <-time.After(2 * time.Second):
		t.Fatal("engine never fetched")
	}
}

func (g *gated) refusedFor(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case g.results <- ok():
		t.Fatal("engine fetched when it should have waited")
	case <-time.After(d):
	}
}

func mine(id string) model.PR {
	return model.PR{ID: id, Repo: "acme/a", Author: model.Actor{Login: "me"}, CreatedAt: now.Add(-time.Hour), Tags: []model.Tag{model.TagAuthor}}
}

func ok(prs ...model.PR) outcome {
	return outcome{res: fetch.Result{PRs: prs, Meta: fetch.Meta{
		Budget: gh.Budget{Remaining: 4000, Limit: 5000, ResetAt: now.Add(time.Hour)},
	}}}
}

func failed(err error) outcome { return outcome{err: err} }

type running struct {
	*Engine
	src    *gated
	states <-chan State
	done   chan error
}

func start(t *testing.T, cfg Config) running {
	t.Helper()
	cfg.Now = func() time.Time { return now }
	src := &gated{results: make(chan outcome)}
	e := New(src, cfg)
	states, unsubscribe := e.Subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	r := running{Engine: e, src: src, states: states, done: make(chan error, 1)}
	go func() { r.done <- e.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		unsubscribe()
	})
	return r
}

func (r running) next(t *testing.T, want func(State) bool) State {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case s := <-r.states:
			if want(s) {
				return s
			}
		case <-timeout:
			t.Fatal("timed out waiting for state")
		}
	}
}

func loaded(s State) bool { return s.Loaded && s.Err == nil }
func erred(s State) bool  { return s.Err != nil }

func TestRunPublishesSnapshots(t *testing.T) {
	r := start(t, Config{Poll: time.Hour})
	r.src.give(t, ok(mine("a")))
	s := r.next(t, loaded)
	if s.Snapshot.Count != 1 || !s.Updated.Equal(now) || !s.Next.Equal(now.Add(time.Hour)) {
		t.Fatalf("state = %+v", s)
	}
	if s.Budget.Remaining != 4000 || !reflect.DeepEqual(r.Current(), s) {
		t.Fatalf("budget = %+v, current = %+v", s.Budget, r.Current())
	}
}

func TestFirstPollFailing(t *testing.T) {
	r := start(t, Config{Poll: time.Hour, Backoff: []time.Duration{time.Millisecond}})
	r.src.give(t, failed(errors.New("offline")))
	s := r.next(t, erred)
	if s.Loaded || !s.Next.Equal(now.Add(time.Millisecond)) {
		t.Fatalf("state = %+v", s)
	}
	r.src.give(t, ok(mine("a")))
	r.next(t, loaded)
}

func TestFailureKeepsSnapshotAndBacksOff(t *testing.T) {
	r := start(t, Config{Poll: time.Millisecond, Backoff: []time.Duration{time.Millisecond, 2 * time.Millisecond}})
	r.src.give(t, ok(mine("a")))
	r.next(t, loaded)
	r.src.give(t, failed(errors.New("boom")))
	s := r.next(t, erred)
	if s.Snapshot.Count != 1 || !s.Next.Equal(now.Add(time.Millisecond)) {
		t.Fatalf("first failure = %+v", s)
	}
	r.src.give(t, failed(errors.New("boom")))
	if s := r.next(t, erred); !s.Next.Equal(now.Add(2 * time.Millisecond)) {
		t.Fatalf("second failure = %+v", s)
	}
	r.src.give(t, failed(errors.New("boom")))
	if s := r.next(t, erred); !s.Next.Equal(now.Add(2 * time.Millisecond)) {
		t.Fatalf("backoff should stay at its last step: %+v", s)
	}
	r.src.give(t, ok(mine("a"), mine("b")))
	if s := r.next(t, loaded); s.Snapshot.Count != 2 {
		t.Fatalf("recovered = %+v", s)
	}
}

func TestRunStopsWhenUnauthorised(t *testing.T) {
	r := start(t, Config{Poll: time.Hour})
	r.src.give(t, failed(gh.ErrUnauthorised))
	select {
	case err := <-r.done:
		if !errors.Is(err, gh.ErrUnauthorised) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run kept going")
	}
	if !errors.Is(r.Current().Err, gh.ErrUnauthorised) {
		t.Fatalf("state = %+v", r.Current())
	}
}

func TestRefreshPollsNow(t *testing.T) {
	r := start(t, Config{Poll: time.Hour})
	r.src.give(t, ok(mine("a")))
	r.next(t, loaded)
	r.Refresh()
	r.src.give(t, ok(mine("a"), mine("b")))
	if s := r.next(t, func(s State) bool { return s.Snapshot.Count == 2 }); s.Err != nil {
		t.Fatalf("state = %+v", s)
	}
}

func TestRateLimitIsHonoured(t *testing.T) {
	r := start(t, Config{Poll: time.Millisecond})
	r.src.give(t, failed(&gh.RateLimitedError{Until: now.Add(time.Hour)}))
	if s := r.next(t, erred); !s.Next.Equal(now.Add(time.Hour)) {
		t.Fatalf("state = %+v", s)
	}
	r.Refresh()
	r.src.refusedFor(t, 100*time.Millisecond)
}

func TestUnknownResetFallsBack(t *testing.T) {
	r := start(t, Config{Poll: time.Millisecond, Backoff: []time.Duration{time.Minute}})
	r.src.give(t, failed(&gh.RateLimitedError{}))
	if s := r.next(t, erred); !s.Next.Equal(now.Add(time.Minute)) {
		t.Fatalf("without a budget, want the first backoff step: %+v", s)
	}

	r = start(t, Config{Poll: time.Hour})
	r.src.give(t, ok(mine("a")))
	r.next(t, loaded)
	r.Refresh()
	r.src.give(t, failed(&gh.RateLimitedError{}))
	if s := r.next(t, erred); !s.Next.Equal(now.Add(time.Hour)) {
		t.Fatalf("with a budget, want its reset: %+v", s)
	}
}

func TestLowBudgetWaitsForReset(t *testing.T) {
	r := start(t, Config{Poll: time.Minute})
	low := ok(mine("a"))
	low.res.Budget = gh.Budget{Remaining: 100, Limit: 5000, ResetAt: now.Add(30 * time.Minute)}
	r.src.give(t, low)
	if s := r.next(t, loaded); !s.Next.Equal(now.Add(30*time.Minute + time.Second)) {
		t.Fatalf("state = %+v", s)
	}
}

type static struct{ viewers int }

func (s *static) Viewer(context.Context) (fetch.Viewer, fetch.Meta, error) {
	s.viewers++
	return fetch.Viewer{Login: "me"}, fetch.Meta{Warnings: []string{"teams hidden"}}, nil
}

func (s *static) Fetch(context.Context, string, func(fetch.Progress)) (fetch.Result, error) {
	return fetch.Result{PRs: []model.PR{mine("a")}, Meta: fetch.Meta{Warnings: []string{"saml"}}}, nil
}

func TestOnceRefreshesViewerHourly(t *testing.T) {
	src := &static{}
	clock := now
	e := New(src, Config{Now: func() time.Time { return clock }})
	steps := []struct {
		advance time.Duration
		viewers int
	}{{0, 1}, {30 * time.Minute, 1}, {31 * time.Minute, 2}}
	for _, step := range steps {
		clock = clock.Add(step.advance)
		st, err := e.Once(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if src.viewers != step.viewers {
			t.Fatalf("after %v: %d viewer fetches, want %d", clock.Sub(now), src.viewers, step.viewers)
		}
		if !reflect.DeepEqual(st.Warnings, []string{"teams hidden", "saml"}) || st.Snapshot.Count != 1 {
			t.Fatalf("state = %+v", st)
		}
	}
}

func TestSubscribersGetOnlyTheLatest(t *testing.T) {
	e := New(&static{}, Config{})
	states, unsubscribe := e.Subscribe()
	<-states
	e.publish(State{Updated: now})
	e.publish(State{Updated: now.Add(time.Minute)})
	if s := <-states; !s.Updated.Equal(now.Add(time.Minute)) {
		t.Fatalf("got %v, want the newest", s.Updated)
	}
	select {
	case s := <-states:
		t.Fatalf("stale state delivered: %+v", s)
	default:
	}
	unsubscribe()
	e.publish(State{})
}

func TestPollTimeoutLeavesRoomForManyBatches(t *testing.T) {
	if got := New(&static{}, Config{}).cfg.Timeout; got < 2*time.Minute {
		t.Fatalf("poll timeout %v: a live poll of 24 PRs takes about 15s, and each request has its own 30s limit", got)
	}
}

func TestPollPublishesProgress(t *testing.T) {
	r := start(t, Config{Poll: time.Hour})
	s := r.next(t, func(s State) bool { return s.Busy && s.Progress.Phase == "details" })
	if s.Progress.Done != 1 || s.Progress.Total != 2 || s.Changed != nil {
		t.Fatalf("busy state = %+v", s)
	}
	r.src.give(t, ok(mine("a")))
	if s := r.next(t, loaded); s.Busy || s.Progress != (fetch.Progress{}) {
		t.Fatalf("finished state still busy: %+v", s)
	}
}

func TestChangedMarksNewAndDifferentPRs(t *testing.T) {
	r := start(t, Config{Poll: time.Hour})
	r.src.give(t, ok(mine("a"), mine("b")))
	if s := r.next(t, loaded); len(s.Changed) != 0 {
		t.Fatalf("first poll is the baseline, changed = %v", s.Changed)
	}
	b := mine("b")
	b.Checks = model.Checks{State: "FAILURE"}
	r.Refresh()
	r.src.give(t, ok(mine("a"), b, mine("c")))
	s := r.next(t, func(s State) bool { return s.Loaded && !s.Busy && s.Snapshot.Count == 3 })
	if !reflect.DeepEqual(s.Changed, []string{"b", "c"}) {
		t.Fatalf("changed = %v, want [b c]", s.Changed)
	}
	r.Refresh()
	r.src.give(t, ok(mine("a"), b, mine("c")))
	s = r.next(t, func(s State) bool { return s.Loaded && !s.Busy && s.Updated.Equal(now) && len(s.Changed) == 0 })
	if s.Snapshot.Count != 3 {
		t.Fatalf("state = %+v", s)
	}
}
