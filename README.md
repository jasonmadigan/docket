# docket

[![test](https://github.com/jasonmadigan/docket/actions/workflows/test.yml/badge.svg)](https://github.com/jasonmadigan/docket/actions/workflows/test.yml)

Every open pull request and issue you're involved in, live in your terminal or a browser tab: CI, what's left before a PR can merge or an issue can close, linked issues and PRs, and how long since anyone touched it.

![docket in a terminal](docs/img/tui.png)

A PR is listed while it's open and you opened it, were asked to review it (directly or through a team), are assigned to it, were @-mentioned in it, reviewed it or commented on it. Nothing is hidden for being old: in each section the longest-neglected come first, so nothing quietly dies.

An issue is listed while it's open and you opened it, are assigned to it, were @-mentioned in it or commented on it. PRs and issues each have a tab.

## What it shows

- CI: passing, running, or failing and which checks.
- What's left: conflicts, behind base, changes requested and by whom, reviewers still awaited, unresolved threads, or ready to merge.
- Your side: a review asked of you or your team, commits pushed since your review, mentions you haven't answered, replies since your last comment.
- Linked issues, and how long since a person, not a bot, last touched the PR.
- Issues: linked PRs and whether they've merged, sub-issue progress, who's assigned, labels, and mentions or replies waiting on you.

It refreshes every minute. The header shows what it's doing while it does it, and `◆` marks PRs that changed since the last refresh until you've looked at them.

## Install

Needs Go 1.26 and the [GitHub CLI](https://cli.github.com/), logged in with `gh auth login`; docket borrows its token.

```sh
go install github.com/jasonmadigan/docket/cmd/docket@latest
```

Runs on macOS and Linux.

## Use

```sh
docket              # terminal UI
docket web --open   # the same in a browser tab, at http://127.0.0.1:7788
docket dump         # print once and exit; --json for scripts
```

| Key | |
|-|-|
| `j` `k`, arrows, wheel | move |
| click | select |
| `tab` | switch between PRs and issues |
| `enter` | open the PR or issue |
| `c` | open its first failing check |
| `i` | open its linked issues |
| `p` | open an issue's linked PRs |
| `a` | archive, or unarchive on the Archived tab |
| `u` | undo the last archive |
| `/` | filter by repo, title, author or tag; `esc` clears |
| `r` | refresh now |
| `s` | settings |
| `?` | help |
| `q` | quit |

PR refs, failing checks and linked issues are links you can click in terminals that support them, such as iTerm2. Over SSH, links are copied to your clipboard instead of opened.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/img/web-dark.png">
  <img alt="docket in a browser" src="docs/img/web-light.png">
</picture>

The web view listens on loopback only. It has no login of its own and refuses requests addressed to any hostname but `localhost`.

## Settings

Press `s` in the terminal, or use Settings in the browser.

![settings in the terminal](docs/img/tui-settings.png)

- Refresh interval: 30s to 10m from the screens, anything from 10s to 1h by hand.
- Accounts that don't count as people, such as CI bots: their comments don't count as activity or replies.

Settings live in `~/.config/docket/config.toml` (`$XDG_CONFIG_HOME` is honoured) and apply straight away. Edits by hand are picked up while docket runs.

```toml
poll = "1m"
ignore_actors = ["codecov", "openshift-ci-robot"]
```

## Archive

Press `a` in the terminal, or Archive in a row's detail in the browser, to move a PR or issue to the Archived tab. It stays there until you unarchive it the same way, or it closes; if it's reopened, it comes back. `u` undoes the last archive in the terminal. Nothing changes on GitHub.

The archive lives in `~/.config/docket/archive.toml`, beside the settings, so whatever syncs your settings between machines carries it too. docket writes it only when you archive or unarchive something.

## Rate limit

A refresh costs a few GraphQL points: one to find your PRs and issues, then roughly one per ten PRs and one per 25 issues for their detail. Two dozen PRs and a hundred issues cost about 10, out of the 5,000 an hour your token shares with everything else using it. With under a tenth left, docket waits for the reset.

## Firewalls

An outbound firewall such as Little Snitch has to let `docket` reach `api.github.com:443`, and may ask again after you rebuild it.

## More

[docs/design.md](docs/design.md) sets out the rules: who counts as a person, what's left, and when a reply counts.

## Licence

[MIT](LICENSE)
