package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jasonmadigan/docket/internal/config"
	"github.com/jasonmadigan/docket/internal/dump"
	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/fetch"
	"github.com/jasonmadigan/docket/internal/gh"
	"github.com/jasonmadigan/docket/internal/tui"
)

const usage = `usage: docket [command] [flags]

commands:
  (none)  terminal UI
  dump    fetch once, print, exit
  help    show this

"docket <command> -h" lists a command's flags
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "docket:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd := "tui"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("docket "+cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	poll := flags.Duration("poll", 0, "time between refreshes (default from config, else 60s)")
	asJSON := new(bool)
	switch cmd {
	case "tui":
	case "dump":
		flags.BoolVar(asJSON, "json", false, "print JSON")
	case "help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	cfg, err := loadConfig(*poll)
	if err != nil {
		return err
	}
	client, err := gh.New()
	if err != nil {
		return err
	}
	eng := engine.New(fetch.New(client), engine.Config{Poll: cfg.Poll, Ignore: cfg.IgnoreActors})
	if cmd == "dump" {
		return printOnce(ctx, eng, stdout, stderr, *asJSON)
	}
	return serve(ctx, eng, func(ctx context.Context) error { return tui.Run(ctx, eng) })
}

func loadConfig(poll time.Duration) (config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	if poll != 0 {
		cfg.Poll = poll
		if err := cfg.Validate(); err != nil {
			return config.Config{}, err
		}
	}
	return cfg, nil
}

func printOnce(ctx context.Context, eng *engine.Engine, stdout, stderr io.Writer, asJSON bool) error {
	st, err := eng.Once(ctx)
	if err != nil {
		return err
	}
	for _, w := range st.Warnings {
		fmt.Fprintln(stderr, "warning:", w)
	}
	if asJSON {
		return dump.JSON(stdout, st.Snapshot)
	}
	return dump.Text(stdout, st.Snapshot)
}

type runner interface {
	Run(ctx context.Context) error
}

// serve runs the engine beside a view until either ends, reporting an
// engine failure over the view's own result.
func serve(ctx context.Context, eng runner, view func(context.Context) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		if err := eng.Run(ctx); err != nil {
			cancel(err)
		}
	}()
	err := view(ctx)
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return err
}
