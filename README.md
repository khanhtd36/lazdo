# lazdo

Lazy Azure DevOps pull requests: the "My pull requests" page of Azure DevOps, in your terminal.

```
▾ Wait for approval (4)
    fix(export): keep large exports from stalling [required]   Nhan Nguyen  !15018  MITS11 → develop   0/2  ✓  2h ago
▾ Waiting for author (3)
    feat(download): stream exported large test data [+1 push]  Nhan Nguyen  !15050  MITS11 → develop   3/4  ✓  5m ago
▾ Assigned to me (4)
▾ Created by me (3)
```

Sections, across every project of the organization:

- **Wait for approval**: you're a reviewer, haven't voted, PR isn't a draft.
- **Waiting for author**: you voted "waiting for author" or "rejected". `[+N push]` flags pushes since your vote.
- **Assigned to me**: every other PR you review (drafts, ones you approved).
- **Created by me**.

Each row: title, draft/required badges, author, ID, repo → target branch, resolved/total
comment threads, build policy (`✓` `✗` `●` running), last update. Reviewer votes
are in the PR detail.

## Install

Linux and macOS (installs to `~/.local/bin`):

```sh
curl -fsSL https://lazdo.khanhtd36.dev/install.sh | sh
```

Windows PowerShell (installs to `%LOCALAPPDATA%\Programs\lazdo` and adds it
to your User PATH, saving the previous PATH to `%LOCALAPPDATA%\lazdo` first):

```powershell
irm https://lazdo.khanhtd36.dev/install.ps1 | iex
```

Homebrew (macOS and Linux):

```sh
brew install khanhtd36/tap/lazdo
```

With Go: `go install github.com/khanhtd36/lazdo@latest`. Binaries and
checksums: [releases](https://github.com/khanhtd36/lazdo/releases).

The scripts verify the download against the release's `checksums.txt`. Run
the same command again to update. To pick a version or folder:
`LAZDO_VERSION=0.1.0` / `LAZDO_INSTALL_DIR=<dir>` (sh), or
`$env:LAZDO_VERSION`, `$env:LAZDO_INSTALL_DIR`, and `$env:LAZDO_SKIP_PATH=1`
to leave PATH alone (PowerShell).

Uninstall:

```sh
rm ~/.local/bin/lazdo                                    # install.sh
brew uninstall lazdo                                     # Homebrew
Remove-Item -Recurse "$env:LOCALAPPDATA\Programs\lazdo"  # install.ps1, then drop it from your User PATH
```

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

### Keys

The same keys mean the same thing in every view (outside text boxes); `?`
lists everything available where you are.

| Key | Everywhere |
| --- | --- |
| `j` / `k`, arrows | move down / up (or scroll a reading pane) |
| `ctrl+d` / `ctrl+u` | half page down / up |
| `g` / `G` | top / bottom |
| `enter` | one level deeper, or run |
| `esc` | back exactly one level (filter, thread, pane, view) |
| `q` | quit (`ctrl+c` quits even while typing) |
| `1`–`9`, `[` / `]` | pages or tabs at the current level |
| `tab` / `shift+tab` | next / previous pane in split views |
| `h` / `l` | left / right: pane, folder fold, or diff side |
| `J` / `K` | next / previous group (section, activity entry, push) |
| `/` | filter a list, or find text in a reading pane |
| `n` / `N` | next / previous match (in a diff: change) |
| `y` | copy menu for the selection (URLs, names, paths, IDs) |
| `o` | open in the browser |
| `r` | refresh |
| `c` | check out the branch (asks where) |

Dashboard: `enter` opens a PR or collapses a section, `/` filters by title,
author, ID or branch.

### Projects page

`2` (or `[` / `]`) switches to the Projects page: your recent projects,
then other projects with open pull requests, then all projects A–Z. `/`
searches projects **and every repo in the organization**; `enter` on a
found repo opens its browser.

Inside a project:

- **Repos**: default branch, last push, size. `enter` opens the repo
  browser: branches (ahead/behind the default branch) │ file tree │ file
  content. Folders load as you open them; `/` in the tree finds any file in
  the repo (the full list loads in the background the first time). Files
  are syntax highlighted, markdown is rendered (`M` for raw), `/` finds
  text in the file. Switching branch keeps your open folders and file. `z`
  folds the branches away (automatic on narrow screens).
  The repo view has tabs **1 Files · 2 Commits · 3 Tags**:
  - **Commits**: the selected branch's history (branch pane on the left),
    with tag badges and change counts; more load as you scroll. `enter`
    shows the commit's diff, `T` tags it (empty message: lightweight tag),
    `c` checks it out detached.
  - **Tags**: newest version first, with the selected annotated tag's
    tagger, date and message. `enter` shows the release changes (commits
    since the previous version tag), `D` one diff of everything since it,
    `d` deletes the tag.
  - `d` in the branch pane deletes a branch (never the default; a PR using
    it is named in the question).
- **Pull requests**: everyone's active PRs; `enter` opens the PR detail.
- **Pipelines**: latest run of each; `enter` lists runs, `enter` on a run
  shows its stages, jobs and steps beside the selected step's log (`/` finds
  text in it). A running run refreshes every few seconds; a step's log
  appears once the step ends (Azure DevOps only streams live logs to the
  browser).

**Checkout** (`c`, wherever a branch is shown) asks for a folder: the working
directory if it is a clone of the repo, the last folder you used, or a new
`./<repo>` folder. An existing clone is fetched and switched, a missing or
empty folder gets a fresh clone; nothing is ever forced.

### Help

`?` lists the shortcuts for where you are, most specific first. `/` filters
them fuzzily; `enter` runs the selected one.

### Copying text

In the file content, diff and pipeline log, `V` (or dragging the mouse)
selects whole lines and `y` copies them: the original text, without line
numbers, colors or wrap breaks (logs without their timestamps). With
nothing selected, `y` opens the Copy menu (current line, whole log, file
path, URLs, commit IDs, …); on a tag it copies the tag name. Inside a comment thread, `y` copies the
comment's markdown. Shift+drag still uses the terminal's own selection.

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

Keys specific to a pull request (on top of the ones above):

| Key | Action |
| --- | --- |
| `v` | vote: approve, approve with suggestions, wait for author, reject, reset |
| `m` | complete, set/cancel auto-complete, mark as draft/publish, abandon |
| `a` | add a comment (Overview: on the PR · diff: on the line or `V` range) |
| `R` / `s` / `e` / `d` | reply / status / edit / delete on the selected thread |
| `f` | Overview: cycle the activity filter |
| `S` | Files: side-by-side ⇄ inline (default follows terminal width) |
| `u` | Files: compare all changes, since my last visit, one update, or any two |
| `V` | diff: start or clear a line range |
| `z` | Files: hide / show the file tree |

`enter` steps into a comment thread (then `j`/`k` picks a comment). Comments
are typed in a text box: `ctrl+s` posts, `ctrl+e` continues the draft in
`$EDITOR`, `esc` asks before discarding a changed draft.

## Release

Push a `v*` tag; goreleaser publishes the binaries.

## License

MIT
