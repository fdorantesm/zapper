# zap

`zap` is a small CLI for finding and removing common releasable development files and directories such as `node_modules`, `venv`, `.next`, `dist`, `.DS_Store`, and debug logs.

## Usage

```sh
go run src/main.go --dirs node_modules,venv,.venv,__pycache__,dist,build,coverage
```

This command runs the `zap` CLI, which scans the current directory for common releasable targets.

By default, the CLI scans for targets including:

- `node_modules`
- `venv`
- `.venv`
- `__pycache__`
- `dist`
- `build`
- `coverage`
- `.pytest_cache`
- `.mypy_cache`
- `.ruff_cache`
- `.tox`
- `.next`
- `.nuxt`
- `.turbo`
- `.cache`
- `.parcel-cache`
- `target`
- `.gradle`
- `.terraform`
- `.serverless`
- `.DS_Store`
- `Thumbs.db`
- `npm-debug.log`
- `yarn-debug.log`
- `yarn-error.log`
- `pnpm-debug.log`

Use `--dry-run` to inspect matches in the TUI without deleting anything:

```sh
go run src/main.go --dry-run
```

The TUI shows releasable space, selected space, elapsed search time, and a k9s-style table with each matching path, last modification age, and size.

TUI keys:

- `up`/`down` or `k`/`j` moves the cursor
- `c` opens the directory browser to change the scan root
- `:` toggles the regex filter (supports wildcards like `*`)
- `s` sorts the list by size descending
- `space` toggles selection
- `a` selects or clears all
- `d` or `enter` deletes selected files and directories, shows real-time progress, and removes deleted rows from the list
- `q` exits without deleting

Directory Browser keys:

- `up`/`down` or `k`/`j` moves the cursor
- `/` toggles the filter/jump input
- `enter` enters the selected directory OR jumps to the typed path (supports `..`)
- `space` selects the current directory as the new scan root
- `esc` or `c` returns to the results view

Use `--yes` to skip the interactive confirmation prompt:

```sh
go run src/main.go --yes
```

## Build

Build a binary for your current OS and architecture:

```sh
./scripts/build.sh
```

The binary is written to `dist/zap-<os>-<arch>`.

Install `zap` into a directory on your `PATH`:

```sh
curl -fsSL https://raw.githubusercontent.com/fdorantesm/zapper/main/install.sh | bash
```

Supported targets are macOS, Linux, and Windows on `amd64` and `arm64`.
