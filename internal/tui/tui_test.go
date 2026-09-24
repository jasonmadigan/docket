package tui

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/fixture"
	"github.com/jasonmadigan/docket/internal/model"
)

type fakeEngine struct {
	states    chan engine.State
	refreshed int
}

func (f *fakeEngine) Subscribe() (<-chan engine.State, func()) { return f.states, func() {} }

func (f *fakeEngine) Refresh() { f.refreshed++ }

type harness struct {
	m      Model
	eng    *fakeEngine
	opened []string
}

func newHarness(t *testing.T, st engine.State, w, h int) *harness {
	t.Helper()
	hs := &harness{eng: &fakeEngine{states: make(chan engine.State, 1)}}
	hs.m, _ = New(hs.eng, Options{
		Open: func(urls ...string) tea.Cmd {
			hs.opened = append(hs.opened, urls...)
			return nil
		},
		Location: time.UTC,
	})
	hs.send(tea.WindowSizeMsg{Width: w, Height: h})
	hs.send(stateMsg(st))
	return hs
}

func (hs *harness) send(msg tea.Msg) {
	next, _ := hs.m.Update(msg)
	hs.m = next.(Model)
}

func key(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func (hs *harness) press(keys ...string) {
	for _, k := range keys {
		hs.send(key(k))
	}
}

func (hs *harness) typing(text string) {
	for _, r := range text {
		hs.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (hs *harness) screen() string { return ansi.Strip(hs.m.View().Content) }

func (hs *harness) selected() string {
	if r := hs.m.current(); r != nil {
		return r.Item().Ref()
	}
	return ""
}

func checkSize(t *testing.T, screen string, w, h int) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	if len(lines) != h {
		t.Fatalf("%d lines, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := ansi.StringWidthWc(line); got > w {
			t.Fatalf("line %d is %d wide, over %d: %q", i, got, w, line)
		}
	}
}

// goldenText drops a screen's padding so golden files survive whitespace
// fixers; checkSize has already measured the padded screen.
func goldenText(screen string) string {
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestViewGolden(t *testing.T) {
	cases := []struct {
		name  string
		state engine.State
		w, h  int
		keys  []string
	}{
		{"wide", fixture.State(), 140, 30, nil},
		{"narrow", fixture.State(), 90, 32, nil},
		{"tiny", fixture.State(), 44, 12, nil},
		{"second row", fixture.State(), 140, 30, []string{"j"}},
		{"loading", engine.State{}, 60, 8, nil},
		{"first poll failed", engine.State{Err: errors.New("dial tcp: no such host"), Next: fixture.Now.Add(time.Minute)}, 90, 10, nil},
		{"poll failed later", fixture.Failed(), 140, 30, nil},
		{"help", fixture.Failed(), 90, 20, []string{"?"}},
		{"nothing open", engine.State{Loaded: true, Updated: fixture.Now, Next: fixture.Now.Add(time.Minute)}, 90, 10, nil},
		{"busy", busyFixture(), 140, 30, nil},
		{"issues", fixture.State(), 140, 30, []string{"tab"}},
		{"issues narrow", fixture.State(), 90, 32, []string{"tab"}},
		{"archived", fixture.State(), 140, 30, []string{"tab", "tab"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs := newHarness(t, c.state, c.w, c.h)
			hs.press(c.keys...)
			screen := hs.screen()
			checkSize(t, screen, c.w, c.h)
			golden.RequireEqual(t, goldenText(screen))
		})
	}
}

func TestTinyTerminals(t *testing.T) {
	for _, size := range [][2]int{{20, 5}, {10, 2}, {1, 1}} {
		hs := newHarness(t, fixture.State(), size[0], size[1])
		hs.press("j", "j", "?", "?", "/", "x", "enter")
		checkSize(t, hs.screen(), size[0], size[1])
	}
	hs := newHarness(t, fixture.State(), 0, 0)
	if got := hs.screen(); got != "" {
		t.Fatalf("zero-sized screen = %q", got)
	}
}

func TestWideCharactersKeepColumns(t *testing.T) {
	prs := fixture.PRs()
	prs[1].Title = "修复网关的重连逻辑 🚀 and enough trailing words to overflow any column"
	st := fixture.State()
	st.Snapshot = model.Build(prs, nil, model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: fixture.Now})
	for _, w := range []int{140, 90, 44} {
		hs := newHarness(t, st, w, 20)
		checkSize(t, hs.screen(), w, 20)
	}
}

func TestKeysOpenLinks(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	if got := hs.selected(); got != "acme/widgets#51" {
		t.Fatalf("first row = %q", got)
	}
	hs.press("enter", "j", "c", "i")
	want := []string{
		"https://github.com/acme/widgets/pull/51",
		"https://ci.example/e2e",
		"https://github.com/acme/widgets/issues/7",
	}
	if !reflect.DeepEqual(hs.opened, want) {
		t.Fatalf("opened %q, want %q", hs.opened, want)
	}
	hs.press("k", "c")
	if !strings.Contains(hs.screen(), "no failing check to open") {
		t.Fatal("no flash for a PR without failing checks")
	}
	hs.press("i")
	if !strings.Contains(hs.screen(), "no linked issues") {
		t.Fatal("no flash for a PR without linked issues")
	}
}

func TestKeysMoveWithinBounds(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("k", "up")
	if got := hs.selected(); got != "acme/widgets#51" {
		t.Fatalf("after moving up from the top: %q", got)
	}
	hs.press("down", "down", "down", "down", "down", "down")
	if got := hs.selected(); got != "acme/docs#88" {
		t.Fatalf("after moving past the bottom: %q", got)
	}
}

func TestRefreshKey(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("r")
	if hs.eng.refreshed != 1 || !strings.Contains(lastLine(hs.screen()), "refreshing") {
		t.Fatalf("refreshed %d times; screen:\n%s", hs.eng.refreshed, hs.screen())
	}
}

func TestFilter(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("/")
	hs.typing("acme/gateway")
	hs.press("enter")
	if len(hs.m.rows) != 2 {
		t.Fatalf("%d rows match acme/gateway", len(hs.m.rows))
	}
	if screen := hs.screen(); strings.Contains(screen, "acme/widgets#51") || !strings.Contains(lastLine(screen), `filter "acme/gateway": 2`) {
		t.Fatalf("screen:\n%s", screen)
	}
	hs.press("esc")
	if len(hs.m.rows) != 5 {
		t.Fatalf("esc left %d rows", len(hs.m.rows))
	}
	hs.press("/")
	hs.typing("erin")
	if len(hs.m.rows) != 1 || hs.selected() != "acme/docs#88" {
		t.Fatalf("author filter: %d rows, selected %q", len(hs.m.rows), hs.selected())
	}
	hs.press("esc", "/")
	hs.typing("acme/devs")
	if len(hs.m.rows) != 1 || hs.selected() != "acme/gateway#1188" {
		t.Fatalf("team filter: %d rows, selected %q", len(hs.m.rows), hs.selected())
	}
	hs.press("esc", "/")
	hs.typing("zzz")
	if !strings.Contains(hs.screen(), "no matches") {
		t.Fatalf("screen:\n%s", hs.screen())
	}
}

func TestSelectionSurvivesRefresh(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("j", "j", "j")
	if hs.selected() != "acme/gateway#1203" {
		t.Fatalf("selected %q", hs.selected())
	}
	prs := fixture.PRs()
	st := fixture.State()
	params := model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: fixture.Now}
	st.Snapshot = model.Build(prs[2:], nil, params)
	hs.send(stateMsg(st))
	if hs.selected() != "acme/gateway#1203" {
		t.Fatalf("selection moved to %q", hs.selected())
	}
	st.Snapshot = model.Build(prs[3:], nil, params)
	hs.send(stateMsg(st))
	if hs.selected() != "acme/docs#88" {
		t.Fatalf("after the selected PR closed, selected %q", hs.selected())
	}
}

func TestScrollFollowsCursor(t *testing.T) {
	hs := newHarness(t, fixture.State(), 90, 6)
	list := func() string {
		lines := strings.Split(hs.screen(), "\n")
		return strings.Join(lines[1:len(lines)-1], "\n")
	}
	hs.press("j", "j", "j", "j")
	if !strings.Contains(list(), "acme/docs#88") {
		t.Fatalf("bottom row out of view:\n%s", list())
	}
	hs.press("k", "k", "k", "k")
	if !strings.Contains(list(), "acme/widgets#51") {
		t.Fatalf("top row out of view:\n%s", list())
	}
}

func TestUnsafeLinksAreNotHyperlinked(t *testing.T) {
	prs := fixture.PRs()
	prs[0].Checks.Failing[0].URL = "file:///Applications/Calculator.app"
	st := fixture.State()
	st.Snapshot = model.Build(prs, nil, model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: fixture.Now})
	hs := newHarness(t, st, 140, 30)
	hs.press("j")
	if strings.Contains(hs.m.View().Content, "file:///") {
		t.Fatal("a file link reached the terminal")
	}
}

func TestProgramRendersAndQuits(t *testing.T) {
	eng := &fakeEngine{states: make(chan engine.State, 1)}
	eng.states <- fixture.State()
	m, _ := New(eng, Options{Open: func(...string) tea.Cmd { return nil }, Location: time.UTC})
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 30))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Envoy"))
	}, teatest.WithDuration(3*time.Second))
	tm.Send(key("q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func lastLine(screen string) string {
	lines := strings.Split(screen, "\n")
	return strings.TrimRight(lines[len(lines)-1], " ")
}

func TestFooterDropsBudgetBeforeCuttingWords(t *testing.T) {
	hs := newHarness(t, fixture.State(), 44, 12)
	if got := lastLine(hs.screen()); strings.Contains(got, "budget") && !strings.Contains(got, "budget 4812/5000") {
		t.Fatalf("footer cut the budget: %q", got)
	}
	hs = newHarness(t, fixture.Failed(), 60, 12)
	if got := lastLine(hs.screen()); !strings.HasSuffix(got, "…") || strings.Contains(got, "budget") {
		t.Fatalf("footer = %q", got)
	}
}

func TestNarrowDetailTakesSpareRows(t *testing.T) {
	hs := newHarness(t, fixture.State(), 90, 32)
	if screen := hs.screen(); !strings.Contains(screen, "dave approved") {
		t.Fatalf("detail clipped while the list had room:\n%s", screen)
	}
}

func TestNothingOpenHasNoDetailPane(t *testing.T) {
	hs := newHarness(t, engine.State{Loaded: true, Updated: fixture.Now, Next: fixture.Now.Add(time.Minute)}, 90, 10)
	if screen := hs.screen(); strings.Contains(screen, "─") {
		t.Fatalf("empty detail pane drawn:\n%s", screen)
	}
}

func firstLine(screen string) string {
	return strings.TrimRight(strings.Split(screen, "\n")[0], " ")
}

func TestErrorKeepsLastSuccess(t *testing.T) {
	hs := newHarness(t, fixture.Failed(), 140, 30)
	screen := hs.screen()
	if got := firstLine(screen); !strings.HasSuffix(got, "✗ updated 12:00 · retry 12:02") {
		t.Fatalf("header = %q", got)
	}
	if got := lastLine(screen); !strings.HasPrefix(got, "fetch failed: Post") {
		t.Fatalf("footer = %q", got)
	}
}

func TestFlashClearsWhenAPollFinishes(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("r")
	busy := fixture.State()
	busy.Busy = true
	hs.send(stateMsg(busy))
	if got := lastLine(hs.screen()); !strings.Contains(got, "refreshing") {
		t.Fatalf("status while polling = %q", got)
	}
	hs.send(stateMsg(fixture.State()))
	if got := lastLine(hs.screen()); strings.Contains(got, "refreshing") || !strings.HasPrefix(got, "tab issues · j/k move") {
		t.Fatalf("status = %q", got)
	}
}

func TestFlashNeverHidesErrors(t *testing.T) {
	hs := newHarness(t, fixture.Failed(), 140, 30)
	hs.press("c")
	got := lastLine(hs.screen())
	if !strings.Contains(got, "no failing check to open") || !strings.Contains(got, "fetch failed") {
		t.Fatalf("status = %q", got)
	}
}

func TestEmojiRowsMatchTheRenderer(t *testing.T) {
	prs := fixture.PRs()
	prs[0].Title = "⚠️ fix the warning banner"
	prs[1].Title = "\U0001f469‍\U0001f4bb pairing notes"
	st := fixture.State()
	st.Snapshot = model.Build(prs, nil, model.Params{Login: "me", Teams: []string{"acme/devs"}, Now: fixture.Now})
	exact := func(t *testing.T, screen string, width func(string) int, w int) {
		t.Helper()
		lines := strings.Split(screen, "\n")
		for i, line := range lines[:len(lines)-1] {
			if got := width(line); got != w {
				t.Fatalf("line %d is %d cells, want %d: %q", i, got, w, line)
			}
		}
	}
	hs := newHarness(t, st, 90, 20)
	hs.press("j")
	exact(t, hs.screen(), ansi.StringWidthWc, 90)
	hs.send(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
	exact(t, hs.screen(), ansi.StringWidth, 90)
}

func TestHeaderShowsCountsAndActivity(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	got := firstLine(hs.screen())
	for _, want := range []string{" docket ", "me · PRs 5 │ Issues 3", "Archived 1", "Mine 2", "Requested 2", "Mentioned 1", "updated 12:00 · next 12:01"} {
		if !strings.Contains(got, want) {
			t.Errorf("header lacks %q: %q", want, got)
		}
	}
	busy := fixture.State()
	busy.Busy = true
	busy.Progress.Phase, busy.Progress.Done, busy.Progress.Total = "details", 10, 24
	hs.send(stateMsg(busy))
	if got := firstLine(hs.screen()); !strings.HasSuffix(got, "pulling data · details 10/24") {
		t.Fatalf("busy header = %q", got)
	}
}

func TestFirstLoadShowsWhatItIsDoing(t *testing.T) {
	st := engine.State{Busy: true}
	st.Progress.Phase = "finding PRs"
	hs := newHarness(t, st, 90, 12)
	if !strings.Contains(hs.screen(), "pulling data · finding PRs") {
		t.Fatalf("screen:\n%s", hs.screen())
	}
}

func TestSpinnerRunsOnlyWhileBusy(t *testing.T) {
	busy := fixture.State()
	busy.Busy = true
	hs := newHarness(t, busy, 140, 30)
	if !hs.m.spinning {
		t.Fatal("no spinner while busy")
	}
	hs.send(stateMsg(fixture.State()))
	hs.send(spinner.TickMsg{})
	if hs.m.spinning {
		t.Fatal("spinner kept going after the poll finished")
	}
}

func TestChangedRowsAreMarkedUntilVisited(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	later := fixture.State()
	later.Updated = fixture.Now.Add(time.Minute)
	later.Changed = []string{"PR_3"}
	hs.send(stateMsg(later))
	row := func() string {
		for _, line := range strings.Split(hs.screen(), "\n") {
			if strings.Contains(line, "acme/gateway#1203") && !strings.Contains(line, "╭") {
				return line
			}
		}
		return ""
	}
	if got := row(); !strings.HasPrefix(got, "◆ ") {
		t.Fatalf("changed row not marked: %q", got)
	}
	hs.press("j", "j", "j")
	if got := row(); !strings.HasPrefix(got, "▌ ") {
		t.Fatalf("selected row = %q", got)
	}
	hs.press("j")
	if got := row(); !strings.HasPrefix(got, "  ") {
		t.Fatalf("visited row still marked: %q", got)
	}
	busy := later
	busy.Busy = true
	hs.send(stateMsg(busy))
	if got := row(); !strings.HasPrefix(got, "  ") {
		t.Fatalf("a busy state re-marked a visited row: %q", got)
	}
}

func TestDetailShowsTheLink(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	screen := hs.screen()
	if !strings.Contains(screen, "╭─ acme/widgets#51 ") || !strings.Contains(screen, "github.com/acme/widgets/pull/51") {
		t.Fatalf("screen:\n%s", screen)
	}
	if raw := hs.m.View().Content; !strings.Contains(raw, "\x1b]8;;https://github.com/acme/widgets/pull/51") {
		t.Fatal("the PR link isn't a terminal hyperlink")
	}
}

func busyFixture() engine.State {
	st := fixture.State()
	st.Busy = true
	st.Progress.Phase, st.Progress.Done, st.Progress.Total = "details", 10, 24
	return st
}

func TestTabSwitchesBetweenLists(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("j")
	if got := hs.selected(); got != "acme/widgets#42" {
		t.Fatalf("PR selection = %q", got)
	}
	hs.press("tab")
	if got := hs.selected(); got != "acme/gateway#1180" {
		t.Fatalf("first issue = %q", got)
	}
	for _, want := range []string{"Mine 1", "Assigned 1", "Mentioned 1", "Document weighted backends"} {
		if !strings.Contains(hs.screen(), want) {
			t.Errorf("issues tab lacks %q", want)
		}
	}
	hs.press("tab")
	if got := hs.selected(); got != "acme/widgets#3" {
		t.Fatalf("archived tab = %q", got)
	}
	hs.press("tab")
	if got := hs.selected(); got != "acme/widgets#42" {
		t.Fatalf("back on PRs, selected %q, want where it was left", got)
	}
	hs.press("shift+tab")
	if got := hs.selected(); got != "acme/widgets#3" {
		t.Fatalf("shift+tab from PRs selected %q", got)
	}
}

func TestClickingATabSwitches(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	line := firstLine(hs.screen())
	i := strings.Index(line, "Issues 3")
	if i < 0 {
		t.Fatalf("header = %q", line)
	}
	hs.send(click(ansi.StringWidth(line[:i])+2, 0))
	if hs.m.tab != tabIssues {
		t.Fatalf("clicked Issues, tab = %d", hs.m.tab)
	}
	line = firstLine(hs.screen())
	hs.send(click(ansi.StringWidth(line[:strings.Index(line, "PRs 5")]), 0))
	if hs.m.tab != tabPRs {
		t.Fatalf("clicked PRs, tab = %d", hs.m.tab)
	}
}

func TestIssueKeys(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("p")
	if !strings.Contains(hs.screen(), "no linked PRs") {
		t.Fatal("no flash for p on a PR")
	}
	hs.press("tab", "j", "p")
	if got := hs.selected(); got != "acme/widgets#7" {
		t.Fatalf("selected %q", got)
	}
	if want := []string{"https://github.com/acme/widgets/pull/42"}; !reflect.DeepEqual(hs.opened, want) {
		t.Fatalf("opened %q, want %q", hs.opened, want)
	}
	hs.press("k", "p")
	if !strings.Contains(hs.screen(), "no linked PRs") {
		t.Fatal("no flash for an issue without linked PRs")
	}
	hs.press("c")
	if !strings.Contains(hs.screen(), "no failing check to open") {
		t.Fatal("no flash for c on an issue")
	}
	hs.press("i")
	if !strings.Contains(hs.screen(), "no linked issues") {
		t.Fatal("no flash for i on an issue")
	}
	hs.press("enter")
	if got := hs.opened[len(hs.opened)-1]; got != "https://github.com/acme/gateway/issues/1180" {
		t.Fatalf("enter opened %q", got)
	}
}

func TestIssueDetailPane(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("tab", "j")
	screen := hs.screen()
	for _, want := range []string{
		"╭─ acme/widgets#7 ", "github.com/acme/widgets/issues/7", "What's left", "PR acme/widgets#42 open",
		"assigned to you 6d ago", "Assignees", "Labels", "bug", "Linked PRs",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("detail lacks %q:\n%s", want, screen)
		}
	}
}

func TestFilterAppliesToTheTabShowing(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	hs.press("tab", "/")
	hs.typing("bug")
	hs.press("enter")
	if len(hs.m.rows) != 1 || hs.selected() != "acme/widgets#7" {
		t.Fatalf("label filter: %d rows, selected %q", len(hs.m.rows), hs.selected())
	}
	hs.press("shift+tab")
	if !strings.Contains(hs.screen(), "no matches") {
		t.Fatalf("PRs under the same filter:\n%s", hs.screen())
	}
}

func TestWindowTitleCountsBothLists(t *testing.T) {
	hs := newHarness(t, fixture.State(), 140, 30)
	if got := hs.m.View().WindowTitle; got != "docket (8)" {
		t.Fatalf("title = %q", got)
	}
}
