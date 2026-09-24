package web

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jasonmadigan/docket/internal/archive"
	"github.com/jasonmadigan/docket/internal/fixture"
)

type fakeArchive struct {
	archived   [][3]string
	unarchived []string
	err        error
}

func (f *fakeArchive) Archive(id, ref, title string) error {
	if f.err != nil {
		return f.err
	}
	f.archived = append(f.archived, [3]string{id, ref, title})
	return nil
}

func (f *fakeArchive) Unarchive(id string) error {
	if f.err != nil {
		return f.err
	}
	f.unarchived = append(f.unarchived, id)
	return nil
}

func withArchive(t *testing.T, a Archiver) http.Handler {
	t.Helper()
	srv, err := New(newFake(fixture.State()), Options{Location: time.UTC, Archive: a})
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func TestArchiveRoute(t *testing.T) {
	a := &fakeArchive{}
	h := withArchive(t, a)
	if rec := postJSON(h, "/archive", `{"id": "PR_2", "archived": true}`, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body)
	}
	if want := [][3]string{{"PR_2", "acme/widgets#51", "Add rate limit docs"}}; !reflect.DeepEqual(a.archived, want) {
		t.Fatalf("archived %v, want %v", a.archived, want)
	}
	if rec := postJSON(h, "/archive", `{"id": "I_4", "archived": true}`, nil); rec.Code != http.StatusNoContent || len(a.archived) != 1 {
		t.Fatalf("archiving an archived item: %d, archived %v", rec.Code, a.archived)
	}
	if rec := postJSON(h, "/archive", `{"id": "I_4", "archived": false}`, nil); rec.Code != http.StatusNoContent || !reflect.DeepEqual(a.unarchived, []string{"I_4"}) {
		t.Fatalf("unarchive: %d, unarchived %v", rec.Code, a.unarchived)
	}
}

func TestArchiveRouteRejects(t *testing.T) {
	cases := map[string]struct {
		archive *fakeArchive
		body    string
		code    int
		want    string
	}{
		"unknown id":    {&fakeArchive{}, `{"id": "PR_404", "archived": true}`, http.StatusBadRequest, "not in docket's lists"},
		"not archived":  {&fakeArchive{err: fmt.Errorf("PR_2: %w", archive.ErrNotArchived)}, `{"id": "PR_2", "archived": false}`, http.StatusBadRequest, "not archived"},
		"store fails":   {&fakeArchive{err: errors.New("disk full")}, `{"id": "PR_2", "archived": true}`, http.StatusInternalServerError, "disk full"},
		"unknown field": {&fakeArchive{}, `{"id": "PR_2", "archived": true, "colour": "red"}`, http.StatusBadRequest, "bad request"},
		"no id":         {&fakeArchive{}, `{"archived": true}`, http.StatusBadRequest, "bad request"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postJSON(withArchive(t, c.archive), "/archive", c.body, nil)
			if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
				t.Fatalf("got %d %s, want %d with %q", rec.Code, rec.Body, c.code, c.want)
			}
		})
	}
}

func TestArchiveRefusesOtherSites(t *testing.T) {
	for name, header := range map[string]map[string]string{
		"cross-site fetch": {"Sec-Fetch-Site": "cross-site"},
		"foreign origin":   {"Origin": "https://evil.example"},
		"form post":        {"Content-Type": "application/x-www-form-urlencoded"},
	} {
		t.Run(name, func(t *testing.T) {
			a := &fakeArchive{}
			rec := postJSON(withArchive(t, a), "/archive", `{"id": "PR_2", "archived": true}`, header)
			if rec.Code < 400 || len(a.archived) != 0 {
				t.Fatalf("got %d, archived %v", rec.Code, a.archived)
			}
		})
	}
}

func TestPageOffersArchivingOnlyWithAStore(t *testing.T) {
	body := get(t, withArchive(t, &fakeArchive{}), "localhost", "/sections").Body.String()
	for _, want := range []string{
		`<button type="button" class="archive" data-archive="PR_2" data-archived="false">Archive</button>`,
		`<button type="button" class="archive" data-archive="I_4" data-archived="true">Unarchive</button>`,
		`<div class="list" data-tab="archived" hidden>`,
		`archived 2d ago`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	h := handler(t, newFake(fixture.State()))
	if body := get(t, h, "localhost", "/sections").Body.String(); strings.Contains(body, "data-archive=") {
		t.Error("archiving offered without a store")
	}
	if code := postJSON(h, "/archive", `{"id": "PR_2", "archived": true}`, nil).Code; code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Errorf("POST /archive without a store: %d", code)
	}
}
