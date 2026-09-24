package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/fixture"
	"github.com/jasonmadigan/docket/internal/model"
)

type fakeEngine struct {
	mu     sync.Mutex
	state  engine.State
	states chan engine.State
	live   int
}

func newFake(st engine.State) *fakeEngine {
	return &fakeEngine{state: st, states: make(chan engine.State, 1)}
}

func (f *fakeEngine) Current() engine.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeEngine) Subscribe() (<-chan engine.State, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live++
	return f.states, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.live--
	}
}

func (f *fakeEngine) subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}

func handler(t *testing.T, eng Engine) http.Handler {
	t.Helper()
	s, err := New(eng, Options{Location: time.UTC, KeepAlive: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

func get(t *testing.T, h http.Handler, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSectionsGolden(t *testing.T) {
	rec := get(t, handler(t, newFake(fixture.State())), "127.0.0.1:7788", "/sections")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	golden.RequireEqual(t, rec.Body.String()+"\n")
}

func TestPageWrapsSections(t *testing.T) {
	body := get(t, handler(t, newFake(fixture.State())), "localhost:7788", "/").Body.String()
	for _, want := range []string{
		"<title>docket (8)</title>",
		`<link rel="stylesheet" href="/static/style.css">`,
		`<script src="/static/app.js" defer></script>`,
		`<main id="sections"><div class="docket" data-title="docket (8)">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestHostGuard(t *testing.T) {
	h := handler(t, newFake(fixture.State()))
	cases := map[string]int{
		"localhost:7788":         http.StatusOK,
		"LOCALHOST":              http.StatusOK,
		"127.0.0.1:7788":         http.StatusOK,
		"[::1]:7788":             http.StatusOK,
		"192.168.1.20:7788":      http.StatusOK,
		"evil.example:7788":      http.StatusForbidden,
		"localhost.evil.example": http.StatusForbidden,
		"":                       http.StatusForbidden,
	}
	for host, want := range cases {
		if got := get(t, h, host, "/").Code; got != want {
			t.Errorf("Host %q: status %d, want %d", host, got, want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := get(t, handler(t, newFake(fixture.State())), "localhost", "/")
	for k, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestStaticAssets(t *testing.T) {
	h := handler(t, newFake(fixture.State()))
	for path, kind := range map[string]string{"/static/app.js": "javascript", "/static/style.css": "text/css", "/static/favicon.svg": "image/svg+xml"} {
		rec := get(t, h, "localhost", path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), kind) {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if code := get(t, h, "localhost", "/static/").Code; code != http.StatusNotFound {
		t.Errorf("directory listing: status %d", code)
	}
}

func TestUnsafeLinksNeutralised(t *testing.T) {
	prs := fixture.PRs()
	prs[0].Checks.Failing[0].URL = "javascript:alert(1)"
	st := fixture.State()
	st.Snapshot = model.Build(prs, nil, model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: fixture.Now})
	body := get(t, handler(t, newFake(st)), "localhost", "/sections").Body.String()
	if strings.Contains(body, "javascript:") || !strings.Contains(body, "<li>e2e</li>") {
		t.Fatal("a javascript: link survived rendering, or took the check name with it")
	}
}

func TestLoadingAndEmpty(t *testing.T) {
	body := get(t, handler(t, newFake(engine.State{})), "localhost", "/sections").Body.String()
	if !strings.Contains(body, "pulling data") || strings.Contains(body, "<section>") {
		t.Fatalf("loading page:\n%s", body)
	}
	body = get(t, handler(t, newFake(engine.State{Loaded: true, Updated: fixture.Now})), "localhost", "/sections").Body.String()
	if !strings.Contains(body, "No open PRs involve you.") {
		t.Fatalf("empty page:\n%s", body)
	}
}

func TestEventsAnnounceStatesAndCleanUp(t *testing.T) {
	eng := newFake(fixture.State())
	srv := httptest.NewServer(handler(t, eng))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	lines := make(chan string, 100)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	expect := func(want string) {
		t.Helper()
		timeout := time.After(2 * time.Second)
		for {
			select {
			case line := <-lines:
				if line == want {
					return
				}
			case <-timeout:
				t.Fatalf("no %q line", want)
			}
		}
	}
	eng.states <- fixture.State()
	expect("event: snapshot")
	expect(": keepalive")
	cancel()
	resp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for eng.subscribers() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a closed tab left its subscription behind")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunServesUntilCancelled(t *testing.T) {
	r, w := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, newFake(fixture.State()), Options{Addr: "127.0.0.1:0", Log: w, Location: time.UTC})
	}()
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, r) }()
	link := strings.TrimSpace(strings.TrimPrefix(line, "docket: serving "))
	resp, err := http.Get(link + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestOnlyWebLinksAreRendered(t *testing.T) {
	prs := fixture.PRs()
	prs[0].Checks.Failing[0].URL = "mailto:someone@example.com"
	prs[0].Issues[0].URL = "/relative"
	prs[1].URL = "ftp://example.com/pr"
	issues := fixture.Issues()
	issues[0].PRs[0].URL = "javascript:alert(1)"
	st := fixture.State()
	st.Snapshot = model.Build(prs, issues, model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: fixture.Now})
	body := get(t, handler(t, newFake(st)), "localhost", "/sections").Body.String()
	for _, bad := range []string{`href="mailto:`, `href="/relative"`, `href="ftp:`, `href="javascript:`, `href=""`} {
		if strings.Contains(body, bad) {
			t.Errorf("rendered %s", bad)
		}
	}
	for _, text := range []string{"acme/widgets#7", "acme/widgets#51", "CI failing: e2e", "<li>acme/widgets#42 Fix reconcile loop"} {
		if !strings.Contains(body, text) {
			t.Errorf("lost the text %q along with its link", text)
		}
	}
}

func TestHeaderShowsCountsAndProgress(t *testing.T) {
	body := get(t, handler(t, newFake(fixture.State())), "localhost", "/sections").Body.String()
	for _, want := range []string{
		`<p class="who"><b>me</b></p>`,
		`<a href="#prs" data-tab="prs" aria-current="page">Pull requests <b>5</b></a>`,
		`<a href="#issues" data-tab="issues">Issues <b>3</b></a>`,
		`<span class="pill">Mine <b>2</b></span>`,
		`<span class="pill">Requested <b>2</b></span>`,
		`<span class="pill">Assigned <b>1</b></span>`,
		`<p class="activity">updated 12:00 · next 12:01 · budget 4812/5000</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	busy := fixture.State()
	busy.Busy = true
	busy.Progress.Phase, busy.Progress.Done, busy.Progress.Total = "details", 10, 24
	body = get(t, handler(t, newFake(busy)), "localhost", "/sections").Body.String()
	if !strings.Contains(body, `<p class="activity busy"><span class="spinner" aria-hidden="true"></span>pulling data · details 10/24`) {
		t.Fatalf("busy page:\n%s", body)
	}
}

func TestChangedRowsAreMarked(t *testing.T) {
	st := fixture.State()
	st.Changed = []string{"PR_3"}
	body := get(t, handler(t, newFake(st)), "localhost", "/sections").Body.String()
	if !strings.Contains(body, `<details class="pr changed" data-id="PR_3">`) || strings.Count(body, "pr changed") != 1 {
		t.Fatalf("page:\n%s", body)
	}
}

func TestIssuesRenderInTheirOwnTab(t *testing.T) {
	body := get(t, handler(t, newFake(fixture.State())), "localhost", "/sections").Body.String()
	for _, want := range []string{
		`<div class="list" data-tab="prs">`,
		`<div class="list" data-tab="issues" hidden>`,
		`<details class="issue" data-id="I_1">`,
		`<span class="fix fix-open" title="linked PR open" aria-label="linked PR open"></span>`,
		`<h3>Linked PRs</h3>`,
		`>acme/widgets#42</a> Fix reconcile loop when the gateway disappears <span class="muted">open</span>`,
		`<h3>Assignees</h3>`,
		`<p class="labels"><span class="tag">bug</span></p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Count(body, `<details class="pr`) != 5 || strings.Count(body, `<details class="issue`) != 3 {
		t.Fatalf("rows by kind wrong:\n%s", body)
	}
}
