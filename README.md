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

Environment overrides:

- `PRT_PROJECTS_DIR`
- `PRT_TEMP_DIR`
- `PRT_TEMP_TTL`
- `PRT_TERMINAL`
- `PRT_VERBOSE`
