package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonmadigan/docket/internal/engine"
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

func withArchive(t *testing.T, a *fakeArchive) *harness {
	t.Helper()
	hs := &harness{eng: &fakeEngine{states: make(chan engine.State, 1)}}
	hs.m, _ = New(hs.eng, Options{
		Open:     func(urls ...string) tea.Cmd { hs.opened = append(hs.opened, urls...); return nil },
		Location: time.UTC,
		Archive:  a,
	})
	hs.send(tea.WindowSizeMsg{Width: 140, Height: 30})
	hs.send(stateMsg(fixture.State()))
	return hs
}

func TestArchiveKeyAndUndo(t *testing.T) {
	a := &fakeArchive{}
	hs := withArchive(t, a)
	hs.press("a")
	if want := [][3]string{{"PR_2", "acme/widgets#51", "Add rate limit docs"}}; !reflect.DeepEqual(a.archived, want) {
		t.Fatalf("archived %v, want %v", a.archived, want)
	}
	if got := lastLine(hs.screen()); !strings.HasPrefix(got, "archived acme/widgets#51 · u undo") {
		t.Fatalf("footer = %q", got)
	}
	hs.press("u")
	if got := lastLine(hs.screen()); !reflect.DeepEqual(a.unarchived, []string{"PR_2"}) || !strings.HasPrefix(got, "unarchived acme/widgets#51") {
		t.Fatalf("undo: unarchived %v, footer %q", a.unarchived, got)
	}
	hs.press("u")
	if got := lastLine(hs.screen()); !strings.HasPrefix(got, "nothing to undo") {
		t.Fatalf("second undo: footer %q", got)
	}
}

func TestUnarchiveOnTheArchivedTab(t *testing.T) {
	a := &fakeArchive{}
	hs := withArchive(t, a)
	hs.press("tab", "tab")
	if got := hs.selected(); got != "acme/widgets#3" {
		t.Fatalf("archived tab selected %q", got)
	}
	hs.press("a")
	if !reflect.DeepEqual(a.unarchived, []string{"I_4"}) || len(a.archived) != 0 {
		t.Fatalf("archived %v, unarchived %v", a.archived, a.unarchived)
	}
	if got := lastLine(hs.screen()); !strings.HasPrefix(got, "unarchived acme/widgets#3") {
		t.Fatalf("footer = %q", got)
	}
}

func TestArchiveFailureShows(t *testing.T) {
	hs := withArchive(t, &fakeArchive{err: errors.New("disk full")})
	hs.press("a")
	if got := lastLine(hs.screen()); !strings.HasPrefix(got, "archive failed: disk full") {
		t.Fatalf("footer = %q", got)
	}
	hs.press("u")
	if got := lastLine(hs.screen()); !strings.HasPrefix(got, "nothing to undo") {
		t.Fatalf("a failed archive left something to undo: %q", got)
	}
}

func TestNoArchivingWithoutAStore(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("a", "u")
	if got := lastLine(hs.screen()); strings.Contains(got, "archive") || strings.Contains(got, "undo") {
		t.Fatalf("footer = %q", got)
	}
}

// review focus: a second a before the new state lands archives the same
// item again, which the store ignores, never the next row
func TestArchiveTwiceBeforeTheStateLands(t *testing.T) {
	a := &fakeArchive{}
	hs := withArchive(t, a)
	hs.press("a", "a")
	if len(a.archived) != 2 || a.archived[0] != a.archived[1] {
		t.Fatalf("archived %v", a.archived)
	}
}

func TestFlashOutlivesAnArchiveRebuild(t *testing.T) {
	hs := withArchive(t, &fakeArchive{})
	hs.press("a")
	hs.send(stateMsg(fixture.State())) // same poll time, not busy: a rebuild
	if got := lastLine(hs.screen()); !strings.HasPrefix(got, "archived acme/widgets#51") {
		t.Fatalf("a rebuild cleared the flash: %q", got)
	}
	busy := fixture.State()
	busy.Busy = true
	hs.send(stateMsg(busy))
	hs.send(stateMsg(fixture.State()))
	if got := lastLine(hs.screen()); strings.Contains(got, "archived acme") {
		t.Fatalf("a finished poll kept the flash: %q", got)
	}
}

// narrow, so the detail pane runs the full width and the meta line fits
func TestArchivedRowSaysWhen(t *testing.T) {
	hs := newHarness(t, fixture.State(), 100, 40)
	hs.press("tab", "tab")
	if screen := hs.screen(); !strings.Contains(screen, "archived 2d ago") || !strings.Contains(screen, "Archived 1") {
		t.Fatalf("screen:\n%s", screen)
	}
}
