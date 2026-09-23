# docket

Every open GitHub pull request you're involved in, live in a terminal or a browser tab: CI, what's left, linked issues, and how long since anyone touched it.

A PR is listed while it's open and you opened it, were asked to review it (directly or through a team), are assigned to it, were @-mentioned in it, reviewed it or commented on it. Nothing is hidden for being old: the longest-neglected sort first in each section.

## Install

Needs Go 1.26 and [`gh`](https://cli.github.com/), logged in (`gh auth login`); docket borrows its token.

```sh
go install ./cmd/docket
```

An outbound firewall such as Little Snitch must let `docket` reach `api.github.com:443`; a rebuilt binary may need allowing again.

## Use

```sh
docket              # terminal UI
docket web --open   # http://127.0.0.1:7788
docket dump --json  # one-off, for scripts
```

In the terminal the header shows what docket is doing, and `◆` marks PRs that changed since the last refresh until you look at them. `j`/`k` move, `enter` opens the PR, `c` its first failing check, `i` its linked issues, `r` refreshes, `/` filters, `?` shows help and warnings, `q` quits. Over SSH, links are copied to the clipboard instead of opened.

The web view has no auth and listens on loopback unless `--addr` says otherwise.

## Config

Optional, in `~/.config/docket/config.toml`:

```toml
poll = "60s"                           # at least 10s
ignore_actors = ["openshift-ci-robot"] # accounts that don't count as people
```

`--poll` overrides `poll`.

## Rate limit

A refresh costs about 1 GraphQL point plus 1 per 10 PRs, out of the 5,000 an hour your token shares with everything else. With under a tenth left, docket waits for the reset.

See `docs/design.md` for how it decides what's left.
