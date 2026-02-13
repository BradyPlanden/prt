# prt

A CLI tool for opening a GitHub pull request in a new terminal tab using git worktrees.

## Requirements

- `git`
- `gh` (GitHub CLI, authenticated)
- macOS with iTerm2 or Terminal.app for tab opening (other OSes print the path)

## Install

```bash
go install github.com/BradyPlanden/prt/cmd/prt@latest
```

## Usage

```bash
prt https://github.com/OWNER/REPO/pull/123
prt https://github.com/OWNER/REPO/pull/123 --temp
prt https://github.com/OWNER/REPO/pull/123 --no-tab
prt clean --dry-run
```

## Config

Create `~/.config/prt/config.yaml`:

```yaml
projects_dir: ~/Projects
temp_dir: /tmp/prt
temp_ttl: 24h
terminal: auto # auto | iterm2 | terminal
```

## Automatic worktree setup

When `prt` opens a PR, it automatically configures the worktree so it's ready to use:

- **Upstream tracking**: The local branch is set to track the remote PR branch, so `git pull`/`git push` work out of the box.
- **Fork remotes**: For cross-repo (fork) PRs, a remote named `prt/<owner>/<repo>` is added pointing to the fork. Existing remote URLs are never overwritten — if you've configured SSH or custom URL rewriting, your settings are preserved.
- **Per-worktree push config**: Cross-repo worktrees get `push.default=upstream` scoped to the worktree, so pushes go to the correct fork branch without affecting other worktrees.
- **Stale branch recovery**: If a local branch exists from a previous worktree that was manually removed, `prt` automatically resets it rather than failing.
- **Offline resilience**: When reusing an existing worktree, fetch failures produce a warning instead of blocking access to the local checkout.

Environment overrides:

- `PRT_PROJECTS_DIR` (default `~/Projects`)
- `PRT_TEMP_DIR` (default `/tmp/prt`)
- `PRT_TEMP_TTL` (default `24h`)
- `PRT_TERMINAL` (default `auto`; `auto | iterm2 | terminal`)
- `PRT_VERBOSE` (set to `1` to enable verbose logging)
