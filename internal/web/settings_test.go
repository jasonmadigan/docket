package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jasonmadigan/docket/internal/config"
	"github.com/jasonmadigan/docket/internal/fixture"
)

type fakeSettings struct {
	cur config.Config
	set []config.Config
	err error
}

func (f *fakeSettings) Get() config.Config { return f.cur }

func (f *fakeSettings) Set(c config.Config) error {
	if f.err != nil {
		return f.err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	f.set = append(f.set, c)
	f.cur = c
	return nil
}

func (f *fakeSettings) Path() string { return "/home/someone/.config/docket/config.toml" }

func withSettings(t *testing.T, s Settings) http.Handler {
	t.Helper()
	srv, err := New(newFake(fixture.State()), Options{Location: time.UTC, Settings: s})
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func post(h http.Handler, body string, header map[string]string) *httptest.ResponseRecorder {
	return postJSON(h, "/settings", body, header)
}

func postJSON(h http.Handler, path, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = "127.0.0.1:7788"
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSettingsRoundTrip(t *testing.T) {
	s := &fakeSettings{cur: config.Config{Poll: time.Minute}}
	h := withSettings(t, s)
	var got struct {
		Poll         string   `json:"poll"`
		IgnoreActors []string `json:"ignore_actors"`
		Choices      []string `json:"choices"`
	}
	rec := get(t, h, "localhost", "/settings")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Poll != "1m" || got.IgnoreActors == nil || !reflect.DeepEqual(got.Choices, []string{"30s", "1m", "2m", "5m", "10m"}) {
		t.Fatalf("settings = %+v", got)
	}
	if rec := post(h, `{"poll": "2m", "ignore_actors": ["codecov"]}`, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	want := []config.Config{{Poll: 2 * time.Minute, IgnoreActors: []string{"codecov"}}}
	if !reflect.DeepEqual(s.set, want) {
		t.Fatalf("saved %+v", s.set)
	}
}

func TestSettingsOffersACustomInterval(t *testing.T) {
	rec := get(t, withSettings(t, &fakeSettings{cur: config.Config{Poll: 45 * time.Second}}), "localhost", "/settings")
	if !strings.Contains(rec.Body.String(), `"45s"]`) {
		t.Fatalf("choices lack the current interval: %s", rec.Body)
	}
}

func TestSettingsRejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		settings *fakeSettings
		body     string
		want     string
	}{
		"not a duration": {&fakeSettings{}, `{"poll": "soon", "ignore_actors": []}`, "refresh interval"},
		"too fast":       {&fakeSettings{}, `{"poll": "1s", "ignore_actors": []}`, "at least 10s"},
		"bad login":      {&fakeSettings{}, `{"poll": "1m", "ignore_actors": ["no spaces please"]}`, "isn't a GitHub login"},
		"unknown field":  {&fakeSettings{}, `{"poll": "1m", "colour": "red"}`, "bad request"},
		"save fails":     {&fakeSettings{err: errors.New("disk full")}, `{"poll": "1m", "ignore_actors": []}`, "disk full"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := post(withSettings(t, c.settings), c.body, nil)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
				t.Fatalf("got %d %s, want 400 with %q", rec.Code, rec.Body, c.want)
			}
		})
	}
}

func TestSettingsRefuseOtherSites(t *testing.T) {
	cases := map[string]struct {
		header map[string]string
		code   int
	}{
		"cross-site fetch": {map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		"foreign origin":   {map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		"form post":        {map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := &fakeSettings{}
			rec := post(withSettings(t, s), `{"poll": "2m", "ignore_actors": []}`, c.header)
			if rec.Code != c.code || len(s.set) != 0 {
				t.Fatalf("got %d, saved %+v", rec.Code, s.set)
			}
		})
	}
	s := &fakeSettings{}
	if rec := post(withSettings(t, s), `{"poll": "2m", "ignore_actors": []}`, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:7788"}); rec.Code != http.StatusNoContent {
		t.Fatalf("same-origin save: %d %s", rec.Code, rec.Body)
	}
}

func TestPageOffersSettingsOnlyWhenItCanSaveThem(t *testing.T) {
	body := get(t, withSettings(t, &fakeSettings{cur: config.Config{Poll: time.Minute}}), "localhost", "/").Body.String()
	for _, want := range []string{`data-open-settings`, `<dialog id="settings"`, `/home/someone/.config/docket/config.toml`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	h := handler(t, newFake(fixture.State()))
	if body := get(t, h, "localhost", "/").Body.String(); strings.Contains(body, "data-open-settings") {
		t.Error("settings offered without a store")
	}
	if code := get(t, h, "localhost", "/settings").Code; code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Errorf("GET /settings without a store: %d", code)
	}
}
