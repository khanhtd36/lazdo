# lazdo

Lazy Azure DevOps pull requests: the "My pull requests" page of Azure DevOps, in your terminal.

```
▾ Wait for approval (4)
    fix(export): keep large exports from stalling [required]   Nhan Nguyen  !15018  MITS11 → develop   TK JC YW   0/2  ✓  2h ago
▾ Waiting for author (3)
    feat(download): stream exported large test data [+1 push]  Nhan Nguyen  !15050  MITS11 → develop   TK! JC    3/4  ✓  5m ago
▾ Assigned to me (4)
▾ Created by me (3)
```

Sections, across every project of the organization:

- **Wait for approval**: you're a reviewer, haven't voted, PR isn't a draft.
- **Waiting for author**: you voted "waiting for author" or "rejected". `[+N push]` flags pushes since your vote.
- **Assigned to me**: every other PR you review (drafts, ones you approved).
- **Created by me**.

Each row: title, draft/required badges, author, ID, repo → target branch, reviewer votes
(`✓` approved, `~` with suggestions, `!` waiting for author, `✗` rejected), resolved/total
comment threads, build policy (`✓` `✗` `●` running), last update.

## Install

```sh
go install github.com/khanhtd36/lazdo@latest
```

Or grab a binary from [releases](https://github.com/khanhtd36/lazdo/releases).

## Auth

None of its own. lazdo borrows the token of the [Azure CLI](https://learn.microsoft.com/cli/azure/):

```sh
az login --allow-no-subscriptions
az devops configure -d organization=https://dev.azure.com/<org>   # optional, else pass --org
```

## Usage

```sh
lazdo                    # default org from `az devops configure`
lazdo --org arbinSW      # or a full https://dev.azure.com/<org> URL
lazdo --interval 30s     # auto-refresh period (default 2m)
```

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | move |
| `tab` / `shift+tab` | next / previous section |
| `enter` | collapse section, or open the PR detail |
| `o` | open PR in browser |
| `y` | copy PR URL |
| `c` | check out PR branch (run lazdo inside a clone of that repo) |
| `r` | refresh now |
| `q` | quit |

### Help

`?` lists the shortcuts for where you are, most specific first. `j`/`k`
scroll, `/` filters fuzzily as you type (`enter` keeps the filter, `esc`
clears it), and `enter` on a shortcut runs it.

### Mouse

Click selects; clicking what is already selected opens it (a PR, a comment
thread, a commit, a file's diff). Click section headers to collapse them,
tabs to switch, and the vote / complete buttons and menu items to act; a
click outside a menu closes it. In the diff, a click picks the line and the
side. The wheel scrolls whatever is under the pointer.

lazdo captures the mouse, so select text with shift+drag.

### PR detail

Opening a PR counts as a visit, exactly like opening it in the browser: its
"new since last visit" counts reset, and the activity filter "What's new"
shows what arrived since your previous visit.

Tabs: **Overview** (checks, description, activity, reviewers, tags, work
items), **Files** (file tree and syntax-highlighted diff), **Commits**
(grouped by push; `enter` diffs one commit), **Conflicts**.

Files tab:

| Key | Action |
| --- | --- |
| `j` / `k` | next / previous file (tree) or line (diff) |
| `enter`, `l`, `tab` | tree: focus the diff · diff: step into the thread on the line |
| `h` / `l` | diff side in side-by-side; `h` on the old side, or `tab`, back to the tree |
| `n` / `p` | next / previous change |
| `ctrl+d` / `ctrl+u` | half page down / up |
| `V` | start or clear a line range |
| `a` | comment on the line or range, on the cursor's side |
| `R` / `s` / `e` / `d` | reply / status / edit / delete on the line's thread |
| `S` | side-by-side ⇄ inline (default follows terminal width) |
| `u` | compare: all changes, since my last visit, one update, or any two |
| `z` | hide / show the file tree |
| `o` | open the file in the browser |

| Key | Action |
| --- | --- |
| `1`–`4`, `[` / `]` | switch tab |
| `v` | vote: approve, approve with suggestions, wait for author, reject, reset |
| `m` | complete, set/cancel auto-complete, mark as draft/publish, abandon |
| `j` / `k` | scroll (Overview), move (other tabs) |
| `J` / `K` | previous / next activity entry |
| `enter` | step into a comment thread (then `j`/`k` picks a comment); open file/commit in browser |
| `f` | cycle activity filter |
| `n` / `R` | new comment / reply |
| `s` | set thread status |
| `e` / `d` | edit / delete your comment |
| `esc` | leave thread, then back to the list |

Comments are typed in a text box: `ctrl+s` posts, `ctrl+e` continues the
draft in `$EDITOR`.

## Release

Push a `v*` tag; goreleaser publishes the binaries.

## License

MIT
