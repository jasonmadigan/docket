# docket

Design, approved 23 September 2026.

An always-open view of every open GitHub pull request I'm involved in: state, CI, what's left, linked issues, time since anyone touched it. A terminal UI and a web page over one engine, run locally on whichever machine I'm on.

## Decisions

- Open PR: listed. Closed or merged: gone.
- No stale threshold. Nothing is demoted for being old: every row shows its age, and the longest-neglected sort first so they get noticed rather than die.
- No dismiss, snooze, mute or other marks. Nothing persisted, nothing to sync between machines.
- github.com only, every org, no allowlist.
- Read-only. The only action is opening things in the browser.
- TUI and web both ship. Each runs the engine in-process. With no state, two copies running at once can't conflict; they only double a cheap poll.
- Go, single binary `docket`, auth borrowed from `gh`.
- macOS 26 on Apple silicon is the main platform, in Terminal.app or iTerm2. Linux works too, including over SSH.
- Not a `gh` extension. Extensions live under `~/.local/share/gh`, which home-config syncs, so a per-arch binary would reach hosts it can't run on.

## Involvement

A PR is listed when any qualifier below matches, each combined with `is:pr is:open archived:false`. It carries every tag that matches. Team @-mentions aren't included.

| Tag | Meaning | Qualifier |
|-|-|-|
| `author` | I opened it | `author:@me` |
| `review` | review requested from me | `user-review-requested:@me` |
| `team` | review requested from one of my teams | `review-requested:@me`, less `review` |
| `assigned` | assigned to me | `assignee:@me` |
| `mentioned` | @-mentioned | `mentions:@me` |
| `reviewed` | I submitted a review | `reviewed-by:@me` |
| `commented` | I commented | `commenter:@me` |

| Section | Holds |
|-|-|
| Mine | `author` |
| Requested | `review`, `team`, `assigned` |
| Mentioned | `mentioned` |
| Participating | `reviewed`, `commented` |

A PR sits in the first section it matches. Empty sections are hidden. Within a section, oldest last human activity first.

## Per PR

Row: `owner/repo#n`, title, author when not me, tags, CI, review decision, age of last human activity.

Detail (TUI pane, expandable web row): what's left, failing checks with links, reviewers and their states, linked issues with their state.

Linked issues come from `closingIssuesReferences`: closing keywords plus issues linked in the sidebar. None of the 23 PRs open today cites a Jira key, so Jira is out.

### What's left

Every line that applies, in this order. CI lines read the head commit's check rollup.

| Condition | Shown |
|-|-|
| `isDraft` | draft |
| `mergeable: CONFLICTING` | conflicts |
| `mergeStateStatus: BEHIND` | behind base |
| rollup `FAILURE` or `ERROR` | CI failing: up to three check names, then `+N` |
| rollup `PENDING` or `EXPECTED` | CI running |
| no checks | no CI |
| `reviewDecision: CHANGES_REQUESTED` | changes requested by X |
| `reviewDecision: REVIEW_REQUIRED`, requests pending | awaiting X |
| `reviewDecision: REVIEW_REQUIRED`, no requests, some approvals | needs more approvals |
| `reviewDecision: REVIEW_REQUIRED`, no requests, no approvals | no reviewer |
| unresolved review threads | N unresolved threads |
| none of the above, `mergeStateStatus: CLEAN` | ready to merge |

`mergeable: UNKNOWN` shows nothing; GitHub computes it lazily and a later tick has it. A null `reviewDecision` means the repo requires no review, so no review line.

Then my side. The review lines only arise on PRs I didn't open; replies count from my last comment or review, or from opening the PR when it's mine.

| Condition | Shown |
|-|-|
| review request to me pending | review requested Nd ago |
| review request to one of my teams pending | review requested from org/team Nd ago |
| I reviewed, head moved since | you <state>, N commits since |
| I reviewed, reviewed commit no longer on the branch | rewritten since your review |
| I reviewed, head unchanged | you <state> Nd ago |
| mentioned, nothing from me since | mentioned by X Nd ago |
| others commented after my last comment | N replies since yours |

`<state>` is approved, requested changes or commented. GitHub's mention event names only the person mentioned; the mentioner is whoever wrote the comment, review or PR created within 5 seconds of it. When the timeline window (last 100 items) doesn't reach my last comment, the count is shown as a floor, e.g. `3+`.

### Human activity

The latest opening, commit (committer date), force push, comment, review, review comment or review request by an actor that is neither a GitHub App (`Bot`, or a login ending `[bot]`) nor listed in `ignore_actors`. Label edits, CI runs and bot comments don't count, which rules out `updatedAt`.

## Architecture

```text
cmd/docket/        flags, config, subcommands
internal/config/   config file
internal/gh/       GraphQL transport: auth from gh, rate-limit accounting
internal/fetch/    discovery and detail queries, API responses into model.PR
internal/model/    pure: tags, sections, what's left, activity age
internal/engine/   poll loop, snapshot fan-out
internal/tui/      Bubble Tea view
internal/web/      net/http, html/template, SSE, embedded assets
internal/browser/  opening links, SSH detection
internal/dump/     text and JSON output
internal/fixture/  fixed state for view tests
```

Dependencies point down only: views on `engine` and `model`, `engine` on `fetch` and `model`, `fetch` on `gh` and `model`. `model` does no I/O and knows nothing of GraphQL.

Libraries: `github.com/cli/go-gh/v2` (auth, GraphQL client), Bubble Tea, Bubbles, Lip Gloss, `github.com/BurntSushi/toml`. The web view uses the standard library only: no JS framework, no build step. Module path `github.com/jasonmadigan/docket`.

### Data flow

Each tick:

1. Discovery: one GraphQL request, a `search` alias per qualifier, ids only. Pages past 100 are followed.
2. Detail for every discovered PR, through `nodes(ids:)`, 10 per request.
3. `model` builds a snapshot from detail and tags; the engine publishes it to subscribers.

No change detection. Refetching everything is cheap at this volume, and several changes don't reliably bump `updatedAt` (checks finishing, base branch moving, threads resolved, linked issues closed). A PR that drops out of discovery (closed, merged, no longer involving me) is gone from the next snapshot.

At startup and hourly: `viewer { login }` and my team memberships, to name the team a request went to.

Every query selects `rateLimit { cost remaining limit resetAt }`, and both views show the remaining budget. Measured on 23 September: a poll of 24 PRs costs 4 to 5 points and takes about 15s, so about 300 points an hour at the 60s default, from the 5,000 shared by everything using my token, agents included. Detail goes 10 PRs a request: 20 took about 7s and drew intermittent 502s from GitHub's query time limit. Each request times out at 30s, a whole poll at 2 minutes. `mergeStateStatus` needs no preview header.

### Errors

| Case | Behaviour |
|-|-|
| `gh` not logged in | exit, saying to run `gh auth login` |
| poll fails (network, 5xx) | keep the last snapshot, show the error and time of last success; retry after 1m, 2m, 4m, capped at 10m; normal cadence after a success |
| secondary rate limit | wait out `Retry-After` |
| under 10% of budget left | stretch the interval to land after `resetAt` |
| partial GraphQL errors, e.g. an org enforcing SAML that the token isn't authorised for | keep what came back; show GitHub's messages as warnings (they don't name the org) |
| search index lag | a new PR can take a minute to show; accepted |

## Commands

| Command | Does |
|-|-|
| `docket` | TUI |
| `docket web` | web view |
| `docket dump` | one fetch, print the snapshot, exit |
| `docket help` | usage |

Flags: `--poll` everywhere, `--addr` and `--open` on `web`, `--json` on `dump`.

`dump` is how build step 1 gets checked, before either view exists.

### TUI

Full screen: one list under section headers, detail pane to the right (below on narrow terminals). Status line: last refresh, next refresh, remaining budget, errors. Columns drop as width shrinks; title and tags go last. Titles are OSC 8 hyperlinks.

| Key | Action |
|-|-|
| `j` `k`, arrows | move |
| `enter` | open PR |
| `c` | open first failing check |
| `i` | open linked issues |
| `r` | refresh now |
| `/` | filter by text |
| `esc` | clear the filter, close help |
| `?` | help |
| `q` | quit |

Opening runs `open` (macOS) or `xdg-open`, and only for http and https links. Over SSH, where neither reaches my screen, it copies the URL with OSC 52 instead. Colours follow the terminal's reported background.

### Web

- Listens on `127.0.0.1:7788`. Any other address needs an explicit `--addr` and prints a warning: there's no auth, and private repo titles are on the page.
- Rejects requests whose `Host` is a name other than `localhost`, which blocks DNS rebinding; IP literals pass, so an explicit `--addr` still works from the LAN.
- `Content-Security-Policy: default-src 'self'`; styles and script are separate embedded files.
- Server-rendered sections. Rows link to the PR, failing checks and linked issues.
- Live: `/events` (SSE) signals a new snapshot, and a few lines of inline JS fetch `/sections` and swap it in. Scroll position and expanded rows survive.
- Tab title carries the count: `docket (23)`.
- Light and dark via `prefers-color-scheme`.
- `--open` launches the browser. On another host, `ssh -L 7788:127.0.0.1:7788` reaches it.

## Config

Optional `~/.config/docket/config.toml`. home-config syncs it, which suits preferences. Flags override it.

```toml
poll = "60s"
ignore_actors = []
```

## Install

`go install ./cmd/docket` on each host. The binary lands in `~/go/bin`, which home-config doesn't sync. Nothing gets built inside the repo, since `~/Work` syncs between hosts: `go build ./...`, never `go build ./cmd/docket`.

## Testing

- `model`: table tests over hand-built `model.PR` values.
- `fetch`: fake transport fed synthetic JSON shaped like API responses. No recorded private-repo data in the tree.
- `engine`: fake fetcher. Snapshot fan-out, backoff, partial errors, budget stretching.
- `fetch` live test behind `-tags live`, on my gh auth. Asserts the queries run and logs their cost.
- `web`: `httptest` and golden HTML, including the `Host` check.
- `tui`: `teatest` golden frames from a fixed snapshot.

## Build order

1. `gh`, `fetch`, `model`, `docket dump`, checked against the live API.
2. `engine`: polling, backoff, budget.
3. TUI.
4. Web.

## Alternatives considered

| Option | Why not |
|-|-|
| gh-dash, one section per qualifier | TUI only. Its PR columns (updatedAt, createdAt, repo, author, labels, assignees, title, base, reviewStatus, state, ci, lines, numComments) have no linked issues, unresolved threads or human activity age, and a PR matching several qualifiers shows in several sections |
| Python and Textual, `textual serve` for the browser | one UI for both, but the web view is a terminal emulated in a tab: no native links, find or scrolling. Needs Python on every host |
| Bun and TypeScript, Ink for the TUI | Ink is weaker than Bubble Tea at dense, keyboard-driven tables |

## Out of scope

Write actions (merge, approve, re-run), desktop notifications, Jira, GitHub Enterprise, several accounts, marks, history.
