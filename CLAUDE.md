# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Twin Manager: a minimalistic two-pane TUI file manager written in Go, built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea) (MVU framework) and
[Lipgloss](https://github.com/charmbracelet/lipgloss) (styling). Linux-focused (drive
detection reads `/proc/mounts`, unmounting shells out to `umount`).

## Commands

The package is split across multiple files in `package main` at the repo root, so always
build/run the package (`.`), not an individual file.

```bash
go run .              # run the app
go build .            # build the ./twin binary
go vet ./...          # static checks
gofmt -l .            # check formatting (no test suite exists in this repo)
```

There are no `_test.go` files currently, so there is no `go test` suite to run.

`go run .` takes over the terminal (alt screen + raw keyboard input) and only exits on a
key press, so it is not useful from a non-interactive shell — it will just hang. Verify
changes with `go build .` / `go vet ./...` and leave actually running the TUI to the user.

## Architecture

The app follows Bubble Tea's Model-View-Update (MVU) pattern, split across files by
responsibility rather than by feature:

- `main.go` — tiny entrypoint, but it does one load-bearing thing: it writes the Kitty
  keyboard protocol enable/disable sequences (`\x1b[>15u` / `\x1b[<u`) around the Bubble Tea
  program. That protocol is what lets the terminal report `alt+<letter>` as a distinct key
  event, so the entire `alt`-based keymap depends on it; in a terminal without Kitty protocol
  support those bindings degrade and only the F-key aliases work.
- `model.go` — the `model` struct (whole app state) and `pane` struct (one of the two
  directory panes: path, files, cursor, viewport, selection, search query). `initialModel()`
  builds the starting state; `Init()` kicks off the initial directory loads for both panes.
- `update.go` — all `Update()` logic. This is the core control flow file. The top of `Update`
  is a chain of mutually-exclusive modal states checked in order — `isCreatingFolder`,
  `isDeleting`, `isConfirmingOverwrite`, `isFavoritesOpen` (with nested `isConfirmingUnmount`
  / `isConfirmingRemoveFav` sub-states), `isPreviewing` — each intercepting key input for its
  own modal UI before falling through to normal two-pane navigation. Below that chain is a
  second switch that handles async result messages (`directoryLoadedMsg`, `fileOperationMsg`,
  etc.) that apply regardless of mode. Pane-local navigation (cursor movement, active search,
  entering directories) lives in the `pane.update()` method at the bottom of the file.
- `commands.go` — `tea.Cmd` constructors that perform actual I/O (filesystem ops, `xdg-open`,
  `umount`, clipboard) inside a returned closure and report results via a message type. Follow
  this pattern for new I/O: never mutate model state directly from a command closure — return
  a message and handle it in `Update`.
- `msg.go` — message types returned by commands (e.g. `directoryLoadedMsg`, `fileDeletedMsg`,
  `fileConflictMsg`, `previewReadyMsg`) that `Update` switches on.
- `fs.go` — filesystem helpers with no Bubble Tea dependency: `readDirectory` (sorts dirs
  first, injects a synthetic `..` entry), recursive `copyFile`/`copyDir`, `getMountedDrives`
  (parses `/proc/mounts`, filters to `/media`, `/mnt`, `/run/media`), `getStandardPaths` (home
  + XDG-style user dirs for the favorites list).
- `keys.go` — `KeyMap`/`Shortcut`: most actions have a canonical `alt+<letter>` binding plus an
  optional F-key alias, though a few are bare keys (`tab` switch pane, `insert` toggle
  selection, `ctrl+c` force quit). `GetAliasMap()` maps F-key aliases back to their canonical
  key string; `Update` resolves incoming key events through this map before switching on
  `m.keyMap.X.Key`, so a new shortcut must be added to `KeyMap` (and `GetShortcuts()`) rather
  than matched as a raw string. `GetShortcuts()` also drives the hints bar, so omitting a new
  shortcut there silently hides it from the UI.
- `view.go` — `View()` and the render helpers for the two panes, status bar, hints bar, and the
  full-screen preview/favorites overlays. Overlays replace one or both panes rather than
  layering on top.
- `styles.go` — all `lipgloss.Style` definitions used by `view.go`.
- `utils.go` — small pure helpers (`fuzzyMatch` for active search, `calculateWrappedLines` for
  preview text wrapping).

### Key conventions

- File operations (copy/move) always operate from the *active* pane to the *inactive* pane's
  current path. Selection defaults to the file under the cursor when nothing is explicitly
  selected (see the repeated `getFilesFromSelected` + fallback-to-cursor pattern in `update.go`).
  Selections are cleared on copy/move/delete.
- Copy/move go through a two-phase conflict check: the command first probes for existing
  destination files without `force`, returns `fileConflictMsg` if any exist, and `Update` then
  drives an interactive y/n/A(ll)/s(kip all) loop (`processOverwriteConflicts`) that re-invokes
  the same command with `force=true` per file.
- Directory reloads pass an optional `focusPath` so the cursor can be restored to a specific
  entry after the reload (e.g. focus the newly created folder, or the parent's own path when
  navigating up via `..`).
- Modal UI state (creating folder, deleting, confirming overwrite, favorites panel, preview) is
  mutually exclusive; when adding a new modal, add a branch to the mode chain at the top of
  `Update` and make sure it returns before falling through to normal pane key handling.
- The modal flags are listed in **two** places that must be kept in sync: the mode chain at the
  top of `Update`, and the delegation guard near the bottom (`if !m.isCreatingFolder && ...`)
  that decides whether the message still reaches `pane.update()`. Note `isFavoritesOpen` is
  absent from that guard today, so favorites keys that don't `return` early (arrows, `home`,
  `end`) also move the underlying pane's cursor. Keep this in mind before "fixing" one half.
- Active search is the `default` branch of `pane.update()`: any unhandled single-rune key is
  appended to `pane.searchQuery`, which then does a prefix match first and falls back to
  `fuzzyMatch`. Consequence: a new bare single-letter keybinding at the pane level will be
  swallowed by search — new actions should go through `KeyMap` (which is resolved earlier, in
  `Update`) instead. Navigation keys and `esc` clear the query.
- Layout arithmetic is hardcoded and duplicated. `tea.WindowSizeMsg` sets
  `paneHeight = msg.Height - 6` (status bar + hints + borders) and `paneWidth = msg.Width/2 - 2`;
  `paneView` renders `p.height-2` rows; preview inner dimensions (`previewWidth-6`,
  `previewHeight-4`) are recomputed in `view.go` *and* at four scroll-clamping sites in
  `update.go`. Changing any chrome means updating all of these together.
- `Update`, `View`, and `pane.update` use **value** receivers and return the mutated copy —
  only helpers like `processOverwriteConflicts` (`*model`) and `ensureCursorInBounds` (`*pane`)
  mutate in place. Inside `Update`, taking `activePane := &m.leftPane` works because `m` is the
  local copy that gets returned.
- `m.modifierState` is currently dead state: the modifier-tracking code at the top of `Update`
  is commented out, so nothing ever sets it. `hintsView` therefore always renders the `alt`
  group with the Alt chip inactive.

## Docs and workflow

- `DOCUMENTATION.md` is a hand-written feature doc that has drifted from the code — it predates
  favorites, sync panes, and open-in-other, and it lists selection as `Alt+I`/`Ctrl+I` when the
  actual binding is `insert` (`alt+i` is Sync Panes). Treat `keys.go` as the source of truth for
  bindings, and update `DOCUMENTATION.md` when changing user-facing behavior.
- Work happens on branches named `<issue-number>-<slug>` (e.g. `15-file-operations-progress-indicator-2`)
  cut from `main` and merged back via GitHub PRs.

## CVC (Continuity/Version Context) MCP

If the `cvc` MCP server is installed and available (tools named `mcp__cvc__*`), use it to track
reasoning across the session:

- Call `mcp__cvc__get_context` for a file before editing it, to see whether it's clean or has an
  outstanding dirty/patch state from a prior session.
- After completing a non-trivial reasoning step, investigation, or fix, call
  `mcp__cvc__commit_thought` with the task, your reasoning (including what you ruled out and
  why), and the response/action taken. Do this especially when the fix wasn't obvious from
  reading the current code alone (e.g. required digging through `git log`/`git blame`/merge
  history), so the "why" is preserved for future sessions.
- Use `mcp__cvc__read_history` when picking up unfamiliar or in-progress work to see what
  reasoning already happened before re-deriving it.
- If the `cvc` MCP tools are not present in the tool list, skip this section entirely — do not
  attempt to install or configure it unprompted.
