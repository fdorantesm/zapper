# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`go-mop` is a Go CLI called **zap** — a TUI tool for finding and removing common development cache/build directories (node_modules, .venv, dist, etc.). The module is named `go-zap`.

- Single file: `src/main.go` contains the entire application
- TUI built with [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea)
- No test files exist

## Build

```sh
./scripts/build.sh                    # Single platform binary
# Cross-compile manually:
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.appVersion=dev" -o dist/zap ./src
```

**Multi-platform build** (requires Docker):
```sh
goreleaser build --clean --snapshot  # Builds all 6 binaries to dist/
```

Outputs: `dist/zap_linux_amd64_v1/zap`, `dist/zap_darwin_arm64_v8.0/zap`, etc.

For tagged releases, create a git tag: `git tag v1.0.0 && git push --tags`

## Run

```sh
go run src/main.go --dry-run                    # Preview what would be deleted
go run src/main.go --yes                        # Delete all found without prompting
go run src/main.go --root ~/project --dirs node_modules,dist
```

## Architecture

`src/main.go` is organized as:

- **Types**: `tuiModel` (main state), `directoryInfo`, message types (`scanResultMsg`, `deleteResultMsg`, `directoryDeletedMsg`, `fadeOutTickMsg`)
- **Commands**: `scanDirectoriesCmd`, `deleteNextCmd`, `fadeOutTickCmd` — tea_cmds that run background work
- **View**: Single `View()` method that renders loading, browse modal, or main table based on state
- **Update**: Large `Update()` method handling keyboard navigation, filtering, selection, deletion with fade-out animation
- **Helpers**: Directory scanning (`findDirectories`), size calculation, path formatting, lipgloss styling

The TUI has two modes: results table (default) and directory browser (triggered by `c` key).