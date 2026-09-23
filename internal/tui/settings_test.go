package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonmadigan/docket/internal/config"
	"github.com/jasonmadigan/docket/internal/engine"
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

func withSettings(t *testing.T, s *fakeSettings, w, h int) *harness {
	t.Helper()
	hs := &harness{eng: &fakeEngine{states: make(chan engine.State, 1)}}
	hs.m, _ = New(hs.eng, Options{
		Open:     func(urls ...string) tea.Cmd { hs.opened = append(hs.opened, urls...); return nil },
		Location: time.UTC,
		Settings: s,
	})
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	hs.send(stateMsg(fixture.State()))
	return hs
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// at reads the screen from column x on line y.
func at(screen string, x, y int) string {
	return string([]rune(strings.Split(screen, "\n")[y])[x:])
}

func TestSettingsPanelShowsCurrentValues(t *testing.T) {
	hs := withSettings(t, &fakeSettings{cur: config.Config{Poll: time.Minute, IgnoreActors: []string{"codecov"}}}, 120, 30)
	hs.press("s")
	screen := hs.screen()
	for _, want := range []string{"╭─ Settings ", "Refresh every", "‹ 1m ›", "codecov", "/home/someone/.config/docket/config.toml"} {
		if !strings.Contains(screen, want) {
			t.Errorf("settings lack %q:\n%s", want, screen)
		}
	}
}

func TestSettingsChangeAndSave(t *testing.T) {
	s := &fakeSettings{cur: config.Config{Poll: time.Minute, IgnoreActors: []string{"codecov"}}}
	hs := withSettings(t, s, 120, 30)
	hs.press("s", "right", "tab")
	hs.typing(", renovate[bot]")
	hs.press("enter")
	want := []config.Config{{Poll: 2 * time.Minute, IgnoreActors: []string{"codecov", "renovate[bot]"}}}
	if !reflect.DeepEqual(s.set, want) {
		t.Fatalf("saved %+v, want %+v", s.set, want)
	}
	if hs.m.editing || !strings.Contains(lastLine(hs.screen()), "settings saved") {
		t.Fatalf("panel still open or no confirmation:\n%s", hs.screen())
	}
}

func TestSettingsCancelKeepsOldValues(t *testing.T) {
	s := &fakeSettings{cur: config.Config{Poll: time.Minute}}
	hs := withSettings(t, s, 120, 30)
	hs.press("s", "right", "esc")
	if len(s.set) != 0 || hs.m.editing {
		t.Fatalf("cancel saved %+v, editing %v", s.set, hs.m.editing)
	}
}

func TestSettingsShowErrors(t *testing.T) {
	s := &fakeSettings{cur: config.Config{Poll: time.Minute}, err: errors.New("disk full")}
	hs := withSettings(t, s, 120, 30)
	hs.press("s", "enter")
	if !hs.m.editing || !strings.Contains(hs.screen(), "disk full") {
		t.Fatalf("error not shown:\n%s", hs.screen())
	}
	s.err = nil
	hs.press("tab")
	hs.typing("bad_login!")
	hs.press("enter")
	if !hs.m.editing || !strings.Contains(hs.screen(), "isn't a GitHub login") {
		t.Fatalf("bad login accepted:\n%s", hs.screen())
	}
}

func TestSettingsAreClickable(t *testing.T) {
	s := &fakeSettings{cur: config.Config{Poll: time.Minute}}
	hs := withSettings(t, s, 120, 30)
	head := hs.m.headerButton()
	if got := at(hs.screen(), head.x, head.y); !strings.HasPrefix(got, "settings") {
		t.Fatalf("header button zone reads %q", got)
	}
	hs.send(click(head.x, head.y))
	if !hs.m.editing {
		t.Fatal("clicking settings didn't open them")
	}
	f := hs.m.form()
	screen := hs.screen()
	for name, c := range map[string]struct {
		z    zone
		want string
	}{"prev": {f.prev, "‹"}, "next": {f.next, "›"}, "save": {f.save, "[ Save ]"}, "cancel": {f.cancel, "[ Cancel ]"}} {
		if got := at(screen, c.z.x, c.z.y); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s zone reads %q, want %q", name, got, c.want)
		}
	}
	hs.send(click(f.next.x, f.next.y))
	f = hs.m.form()
	hs.send(click(f.save.x, f.save.y))
	if len(s.set) != 1 || s.set[0].Poll != 2*time.Minute {
		t.Fatalf("saved %+v", s.set)
	}
}

func TestClickAndWheelMoveTheSelection(t *testing.T) {
	hs := withSettings(t, &fakeSettings{cur: config.Config{Poll: time.Minute}}, 140, 30)
	lines := strings.Split(hs.screen(), "\n")
	for y, line := range lines {
		if strings.Contains(line, "acme/gateway#1203") && !strings.Contains(line, "╭") {
			hs.send(click(10, y))
			break
		}
	}
	if hs.selected() != "acme/gateway#1203" {
		t.Fatalf("click selected %q", hs.selected())
	}
	hs.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if hs.selected() != "acme/docs#88" {
		t.Fatalf("wheel moved to %q", hs.selected())
	}
	hs.send(click(10, 1)) // a section badge selects nothing
	if hs.selected() != "acme/docs#88" {
		t.Fatalf("clicking a header moved to %q", hs.selected())
	}
}

func TestNoSettingsWithoutAStore(t *testing.T) {
	hs := newHarness(t, fixture.State(), 120, 30)
	hs.press("s")
	if hs.m.editing || strings.Contains(firstLine(hs.screen()), "settings") {
		t.Fatalf("settings offered without a store:\n%s", hs.screen())
	}
}
