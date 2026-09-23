# Repository instructions

docket shows every open GitHub pull request the viewer is involved in, as a terminal UI (`docket`), a localhost web page (`docket web`) and a one-shot print (`docket dump`). Go, one binary, auth borrowed from the `gh` CLI.

`docs/design.md` is the source of truth for behaviour. Change it in the same commit as any behaviour it describes.

## Layout

```text
cmd/docket/          subcommands, flags, wiring
internal/config/     config file, settings store, watcher for outside edits
internal/gh/         GraphQL over go-gh: error mapping, rate-limit budget
internal/fetch/      discovery and detail queries; API responses into model.PR
internal/model/      pure rules: activity, what's left, my side, sections
internal/engine/     polling, backoff, rate limits, progress, change tracking, fan-out
internal/tui/        Bubble Tea v2 view
internal/web/        net/http, html/template, SSE, embedded static files
internal/browser/    opening links, SSH detection
internal/dump/       text and JSON output
internal/fixture/    fixed state for tests
internal/demo/       made-up state for screenshots
tools/screenshots/   renders the real views over the demo state
```

Dependencies point down: views on `engine` and `model`, `engine` on `fetch` and `model`, `fetch` on `gh` and `model`. `model` does no I/O and knows nothing of GraphQL.

## Commands

```sh
gofmt -l .        # must print nothing
go vet ./...
go test -race ./...
go test -tags live -run Live -v ./internal/fetch/   # real API; needs gh auth login
go install ./cmd/docket
```

`go build ./cmd/docket` drops a binary in the tree; use `go build ./...` to check compilation. When a firewall holds a fresh binary's connections, `DOCKET_LIVE_VIA=gh` sends the live test's queries through the `gh` CLI.

## Tests

- Test first, and watch it fail.
- Goldens live in each package's `testdata/`. Regenerate with `go test ./internal/<pkg>/ -run <Test> -update`, then read the diff. Where a golden disagrees with the design, fix the code.
- Goldens end with a newline and carry no trailing whitespace; `internal/fixture` checks every package.
- Test data is synthetic. Never commit responses recorded from real repositories.

## Easy to break

- TUI widths: measure with the model's `method` (wcwidth until the terminal reports mode 2027, graphemes after) through `m.fit`, `m.pad` and `m.method`. `ansi.StringWidth`, `lipgloss.Width` and `lipgloss.JoinHorizontal` measure graphemes and misalign emoji rows.
- Links: open, copy or hyperlink only http and https (`browser.Valid`, `webOnly`). Check URLs come from third-party apps.
- Web: every route passes `guard` (Host `localhost` or an IP literal, CSP `default-src 'self'`, no inline script). Routes that change state take JSON only and refuse cross-site requests.
- GraphQL: an error without data is a failed poll, never an empty list; partial data becomes warnings. Detail goes 10 PRs a request, since 20 drew 502s from GitHub's query time limit.
- Engine: subscribers get only the latest state, and busy states carry no `Changed`. Row fingerprints must ignore the clock.
- The only thing written to disk is settings, `~/.config/docket/config.toml`, atomically through `config.Store`.

## Style

- Comments only where the code can't say it: terse, lower case except a leading exported identifier.
- British spelling in anything a person reads. No emoji; UI glyphs are `✓ ✗ ● ○ · ◆ ▌ │ ─ ╭ ╮ ╰ ╯ ‹ › ← → …`.
- Every file ends with a newline.
- Conventional commits, signed off for the DCO (`git commit -s`), without co-author or "generated with" trailers.

## Screenshots

```sh
go install github.com/charmbracelet/freeze@latest
go run ./tools/screenshots tui 150 32 > tui.ansi     # extra args are keys to press first, e.g. s
freeze -l ansi --window --font.file <monospace .ttf> -o docs/img/tui.png tui.ansi
go run ./tools/screenshots web                        # demo page on 127.0.0.1:7799
```

`freeze` needs a monospace font file, or it falls back to a proportional one. Take the web shots with a headless browser at 2x, in dark and light.
