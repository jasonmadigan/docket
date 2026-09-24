// Command screenshots renders docket over made-up data for the README.
//
//	go run ./tools/screenshots tui 150 32 [keys] > shot.ansi   # then freeze
//	go run ./tools/screenshots web                            # serves 127.0.0.1:7799
//	go run ./tools/screenshots text                           # dump output
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonmadigan/docket/internal/config"
	"github.com/jasonmadigan/docket/internal/demo"
	"github.com/jasonmadigan/docket/internal/dump"
	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/tui"
	"github.com/jasonmadigan/docket/internal/web"
)

type still struct{ st engine.State }

func (s still) Current() engine.State { return s.st }

func (s still) Subscribe() (<-chan engine.State, func()) {
	ch := make(chan engine.State, 1)
	ch <- s.st
	return ch, func() {}
}

func (s still) Refresh() {}

type settings struct{ c config.Config }

func (s *settings) Get() config.Config { return s.c }

func (s *settings) Set(c config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	s.c = c
	return nil
}

func (s *settings) Path() string { return "~/.config/docket/config.toml" }

// shelf archives nothing: the demo state never changes.
type shelf struct{}

func (shelf) Archive(string, string, string) error { return nil }
func (shelf) Unarchive(string) error               { return nil }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "screenshots:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("want tui, web or text")
	}
	st := demo.State()
	prefs := &settings{c: config.Config{Poll: time.Minute, IgnoreActors: []string{"codecov", "openshift-ci-robot"}}}
	switch args[0] {
	case "tui":
		if len(args) < 3 {
			return fmt.Errorf("want tui width height [keys]")
		}
		w, _ := strconv.Atoi(args[1])
		h, _ := strconv.Atoi(args[2])
		fmt.Print(frame(st, prefs, w, h, args[3:]))
		return nil
	case "text":
		return dump.Text(os.Stdout, st.Snapshot)
	case "web":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return web.Run(ctx, still{st}, web.Options{Addr: "127.0.0.1:7799", Log: os.Stderr, Location: time.UTC, Settings: prefs})
	}
	return fmt.Errorf("unknown mode %q", args[0])
}

// frame drives the real model: size it, let it take the state, press keys.
func frame(st engine.State, prefs *settings, w, h int, keys []string) string {
	m, _ := tui.New(still{st}, tui.Options{Location: time.UTC, Settings: prefs, Archive: shelf{}})
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, msg := range drain(m.Init()) {
		model, _ = model.Update(msg)
	}
	for _, k := range keys {
		model, _ = model.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
	}
	return model.View().Content
}

// drain runs cmd and any batch it returns, collecting the messages.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, drain(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}
