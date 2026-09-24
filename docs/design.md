# docket

Design notes, September 2026.

An always-open view of every open GitHub pull request and issue I'm involved in: state, CI, what's left, linked issues and PRs, time since anyone touched it. A terminal UI and a web page over one engine, run locally on whichever machine I'm on.

## Decisions

- Open PR or issue: listed. Closed or merged: gone.
- No stale threshold. Nothing is demoted for being old: every row shows its age, and the longest-neglected sort first so they get noticed rather than die.
- One mark, archive: local, and it lasts until I unarchive the item or it closes. No dismiss, snooze or mute. docket writes two files, settings and the archive, and only when I act.
- github.com only, every org, no allowlist.
- Read-only on GitHub. The actions are opening things in the browser and archiving them locally.
- TUI and web both ship. Each runs the engine in-process. With no state, two copies running at once can't conflict; they only double a cheap poll.
- Go, single binary `docket`, auth borrowed from `gh`.
- macOS 26 on Apple silicon is the main platform, in Terminal.app or iTerm2. Linux works too, including over SSH.
- A plain binary rather than a `gh` extension; `go install` builds one per machine.

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

An issue is listed the same way, each qualifier combined with `is:issue is:open archived:false`, and filed by the same rule.

| Tag | Meaning | Qualifier |
|-|-|-|
| `author` | I opened it | `author:@me` |
| `assigned` | assigned to me | `assignee:@me` |
| `mentioned` | @-mentioned | `mentions:@me` |
| `commented` | I commented | `commenter:@me` |

| Section | Holds |
|-|-|
| Mine | `author` |
| Assigned | `assigned` |
| Mentioned | `mentioned` |
| Participating | `commented` |

## Per PR

Row: `owner/repo#n`, title, author when not me, tags, CI, review decision, age of last human activity.

Detail (TUI pane, expandable web row): what's left, failing checks with links, reviewers and their states, linked issues with their state.

Linked issues come from `closingIssuesReferences`: closing keywords plus issues linked in the sidebar. Jira is out of scope.

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
| no red or amber line above, `mergeStateStatus: CLEAN` or `HAS_HOOKS` | ready to merge |

`draft` and `no CI` are grey notes and don't block `ready to merge`. `mergeable: UNKNOWN` shows nothing; GitHub computes it lazily and a later tick has it. A null `reviewDecision` means the repo requires no review, so no review line.

Then my side. The review lines only arise on PRs I didn't open; replies count from my last comment or review, or from opening the PR when it's mine.

| Condition | Shown |
|-|-|
| review request to me pending | review requested Nd ago |
| review request to one of my teams pending | review requested from org/team Nd ago |
| I reviewed, head moved since | you <state>, N commits since |
| I reviewed, reviewed commit no longer on the branch | rewritten since your review |
| I reviewed, head unchanged | you <state> Nd ago |
| mentioned, nothing from me since | mentioned by X Nd ago |
| others commented or reviewed after my last comment | N replies since yours; approvals and dismissals don't count |

`<state>` is approved, requested changes or commented; only submitted reviews count, so a pending draft never hides one. Deleted accounts show as ghost, as GitHub shows them. GitHub's mention event names only the person mentioned; the mentioner is whoever wrote the comment, review or PR created within 5 seconds of it. When the timeline window (last 100 items) doesn't reach my last comment, the count is shown as a floor, e.g. `3+`.

### Human activity

The latest opening, commit (committer date), force push, comment, review, review comment, review request or reopening by an actor that is neither a GitHub App (`Bot`, or a login ending `[bot]`) nor listed in `ignore_actors`. Label edits, CI runs and bot comments don't count, which rules out `updatedAt`.

## Per issue

Row: `owner/repo#n`, title, author when not me, tags, the state of its linked PRs, age of last human activity.

Detail: what's left, my side, assignees, labels, linked PRs.

Linked PRs come from `closedByPullRequestsReferences`, closed ones included so a merged fix shows; one closed without merging no longer bears on the issue and is dropped. The glyph is `●` while a linked PR is open or draft, `✓` once one has merged and none is open, `○` with none.

### What's left

What stands before it can close, every line that applies, in this order.

| Condition | Shown |
|-|-|
| linked PR merged | PR o/r#n merged |
| linked PR open | PR o/r#n open |
| linked PR draft | PR o/r#n draft |
| sub-issues, some not completed | 3 of 7 sub-issues completed |
| sub-issues, all completed | all 7 sub-issues completed |
| no assignees | unassigned |

`draft` and `unassigned` are grey notes. "Completed" is GitHub's `subIssuesSummary.completed`.

Then my side. Replies count from my last comment, or from opening it when it's mine, with the same floor as for PRs.

| Condition | Shown |
|-|-|
| assigned to me | assigned to you Nd ago |
| mentioned, nothing from me since | mentioned by X Nd ago |
| others commented after my last comment | N replies since yours |

The assignment's age goes once its event leaves the timeline window. The mentioner is found as for PRs.

### Human activity

The latest opening, comment, assignment, cross-reference or reopening by a person, by the same rule as PRs.

## Archive

Any PR or issue can be archived. It leaves its section for the Archived list, `Pull requests` then `Issues`, newest archived first, and stays there until I unarchive it or it closes. A reopening newer than the archive ends it, and the item returns to its section. Nothing changes on GitHub.

Archived items are still fetched, so their rows stay whole and unarchiving shows one at once. Counts and titles leave them out.

The engine keeps the last poll's result. An archive change rebuilds the snapshot from it at once, without polling or spending budget, and a poll under way publishes with the archive as it stands, so an archive made mid-poll holds.

## Architecture

```text
cmd/docket/        flags, config, subcommands
internal/config/   config file
internal/archive/  archive file, store, lock, watcher
internal/gh/       GraphQL transport: auth from gh, rate-limit accounting
internal/fetch/    discovery and detail queries, API responses into model.PR and model.Issue
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

1. Discovery: one GraphQL request, a `search` alias per qualifier, PRs and issues alike, ids only. Pages past 100 are followed.
2. Detail for every discovered PR and issue, through `nodes(ids:)`: PRs 10 a request, issues 25.
3. Archived items discovery didn't return are looked up, `id` and `state` only, 100 a request. Closed, merged, or an id GitHub answers `NOT_FOUND` for (deleted, or no longer visible) has ended its archive, and the entry goes at the next archive or unarchive; left alone, it would raise a warning every poll for an entry no screen can unarchive. A null node for any other reason, such as an org enforcing SAML, is left alone. `dump` skips this step.
4. `model` builds a snapshot from detail and tags; the engine publishes it to subscribers.

No change detection. Refetching everything is cheap at this volume, and several changes don't reliably bump `updatedAt` (checks finishing, base branch moving, threads resolved, linked issues closed). A PR or issue that drops out of discovery (closed, merged, no longer involving me) is gone from the next snapshot, and detail drops any whose state isn't open, since search lags. A GraphQL error that comes back without data is a failed poll, never an empty list.

At startup and hourly: `viewer { login }` and my team memberships, to name the team a request went to.

Every query selects `rateLimit { cost remaining limit resetAt }`, and both views show the remaining budget. Measured on 23 September: a poll of 24 PRs costs 4 to 5 points and takes about 15s, so about 300 points an hour at the 60s default, from the 5,000 an hour the token shares with everything else using it. Detail goes 10 PRs a request: 20 took about 7s and drew intermittent 502s from GitHub's query time limit. Each request times out at 30s, a whole poll at 2 minutes. `mergeStateStatus` needs no preview header. Issue detail is lighter: measured on 24 September, 25 issues a request took 1.5s for 1 point and 50 took 4.4s for 2. With 102 open issues involving me beside 22 PRs, a poll costs about 10 points and takes about 25s, so about 600 points an hour at the 1m default.

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
| `docket dump` | one fetch, print PRs then issues, leaving archived ones out bar a count, exit; coloured and hyperlinked in a terminal, plain when piped |
| `docket help` | usage |

Flags: `--poll` everywhere, `--addr` and `--open` on `web`, `--json` on `dump`.

`dump --json` prints the snapshot: `login` and `at`, then `prs`, `issues` and `archived`, each a `count` and its `sections`, whose rows hold a `pr` or an `issue`; an archived row also says when it was archived.


### TUI

Full screen. A header bar carries my login, a tab per list with its count (`PRs 22 │ Issues 102 │ Archived 3`), the section counts of the tab showing, and on the right either a spinner with the poll's step (`pulling data · details 10/24`) or when it last updated and next will; red, with the last success kept, after a failure. Below it, the tab's list under section badges, and a rounded detail pane to the right (below on narrow terminals) titled with the PR or issue and showing its link. A footer carries messages and errors, else key hints, with the budget on the right. PRs new or changed since the last poll carry a `◆` until visited; the clock ticking ages on doesn't count as change. `a` archives the row under the cursor, or on the Archived tab unarchives it, and the footer offers `u` to undo the last archive. A finished poll clears such messages; an archive's rebuild doesn't. Columns drop as width shrinks; title and tags go last. Issues have no review column. The header drops the section counts first, then the tabs. Refs, checks and issues are underlined OSC 8 hyperlinks.

| Key | Action |
|-|-|
| `j` `k`, arrows, wheel | move |
| `tab` `shift+tab`, or a tab's name in the header | next or previous tab |
| click | select a row or tab; work the settings panel |
| `enter` | open PR or issue |
| `c` | open first failing check |
| `i` | open linked issues |
| `p` | open linked PRs |
| `a` | archive; unarchive on the Archived tab |
| `u` | undo the last archive |
| `r` | refresh now |
| `s`, or `settings` in the header | settings |
| `/` | filter by text |
| `esc` | clear the filter, close help |
| `?` | help |
| `q` | quit |

Opening runs `open` (macOS) or `xdg-open`, and only for http and https links. Over SSH, where neither reaches the screen in front of you, it copies the URL with OSC 52 instead. Colours follow the terminal's reported background.

### Web

- Listens on `127.0.0.1:7788`. Any other address needs an explicit `--addr` and prints a warning: there's no auth, and private repo titles are on the page.
- Rejects requests whose `Host` is a name other than `localhost`, which blocks DNS rebinding; IP literals pass, so an explicit `--addr` still works from the LAN.
- `Content-Security-Policy: default-src 'self'`; styles and script are separate embedded files.
- Server-rendered lists, one per tab: Pull requests, Issues and Archived. A tab bar switches between them and the URL fragment (`#issues`) keeps the choice across reloads and refreshes. Rows link to the PR or issue, failing checks, linked issues and linked PRs, for http and https links only.
- An Archive button in each row's detail, Unarchive on the Archived tab. `POST /archive` takes `{"id", "archived"}` as JSON only and refuses requests a browser marks cross-site or from another origin, as `/settings` does. It archives only what the page lists and unarchives only what the archive holds; the event stream then brings the new state. A failure shows for a few seconds at the foot of the page.
- Live: `/events` (SSE) signals a new snapshot, and a few lines of inline JS fetch `/sections` and swap it in. Scroll position and expanded rows survive.
- Header: login, the tab bar with a count per tab, a pill per section of the tab showing, and a spinner with the poll's step while one runs; each step arrives over the event stream.
- Rows that changed in the latest poll flash and fade.
- Settings in a dialog, outside the swapped region so a refresh never closes it. `POST /settings` takes JSON only and refuses requests a browser marks cross-site or from another origin, so no other page can change them.
- Tab title carries the count of PRs and issues: `docket (124)`.
- Light and dark via `prefers-color-scheme`.
- `--open` launches the browser. On another host, `ssh -L 7788:127.0.0.1:7788` reaches it.

## Config

Optional `~/.config/docket/config.toml` (`$XDG_CONFIG_HOME` honoured). The settings screens in both views write it, atomically, and a running docket checks it every couple of seconds so edits by hand or by another copy apply without a restart. `--poll` overrides it at start.

```toml
poll = "1m"
ignore_actors = ["codecov", "openshift-ci-robot"]
```

`poll` runs from 10s to 1h; the screens offer 30s, 1m, 2m, 5m and 10m. `ignore_actors` must be GitHub logins.

The archive is `archive.toml` beside it, written only by archiving and unarchiving, never in the background, so hosts sharing the file through sync never race each other's polls. Each write takes a host-local `flock` on the directory, rereads the file, applies the change, drops entries whose archive has ended, and replaces the file in one step. A file that doesn't parse is never replaced, and at startup docket exits naming it. Hand edits apply within a couple of seconds, but the next write rewrites the file: the entries stay, and only docket's own header comment does.

```toml
[[item]]
id = "I_kwDOAbc123"
ref = "Kuadrant/docs#88"
title = "document dns policy"
at = 2026-09-24T14:02:00Z
```

`id` is the node id, which survives renames and transfers; `ref` and `title` are only for reading the file. Deleting an entry unarchives it.

## Install

`go install github.com/jasonmadigan/docket/cmd/docket@latest`, or `go install ./cmd/docket` from a checkout.

## Testing

- `model`: table tests over hand-built `model.PR` values.
- `fetch`: fake transport fed synthetic JSON shaped like API responses. No recorded private-repo data in the tree.
- `engine`: fake fetcher. Snapshot fan-out, backoff, partial errors, budget stretching.
- `fetch` live test behind `-tags live`, using gh's login; `DOCKET_LIVE_VIA=gh` sends the queries through the gh CLI for when a firewall holds the test binary. Asserts the queries run and logs their cost.
- `web`: `httptest` and golden HTML, including the `Host` check.
- `tui`: `teatest` golden frames from a fixed snapshot.
- `archive`: round trip, invalid files, two stores sharing a file, concurrent writers, an unparseable file never replaced, ended entries dropped, watching.


## Alternatives considered

| Option | Why not |
|-|-|
| gh-dash, one section per qualifier | TUI only. Its PR columns (updatedAt, createdAt, repo, author, labels, assignees, title, base, reviewStatus, state, ci, lines, numComments) have no linked issues, unresolved threads or human activity age, and a PR matching several qualifiers shows in several sections |
| Python and Textual, `textual serve` for the browser | one UI for both, but the web view is a terminal emulated in a tab: no native links, find or scrolling. Needs Python on every host |
| Bun and TypeScript, Ink for the TUI | Ink is weaker than Bubble Tea at dense, keyboard-driven tables |

## Out of scope

Write actions on GitHub (merge, approve, re-run), desktop notifications, Jira, GitHub Enterprise, several accounts, snooze, history.
