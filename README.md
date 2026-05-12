# zap

`zap` is a small CLI for finding and removing common development environment directories such as `node_modules`, `venv`, `.venv`, and `__pycache__`.

## Usage

```sh
go run . --dirs node_modules,venv,.venv,__pycache__
```

By default, the CLI scans the current directory for:

- `node_modules`
- `venv`
- `.venv`
- `__pycache__`

Use `--dry-run` to inspect matches in the TUI without deleting anything:

```sh
go run . --dry-run
```

The TUI shows releasable space, selected space, elapsed search time, and a k9s-style table with each matching path, last modification age, and size.

TUI keys:

- `up`/`down` or `k`/`j` moves the cursor
- `space` toggles selection
- `a` selects or clears all
- `d` or `enter` deletes selected directories, shows progress, and removes deleted rows from the list
- `q` exits without deleting

Use `--yes` to skip the interactive confirmation prompt:

```sh
go run . --yes
```

## Build

Build a binary for your current OS and architecture:

```sh
./scripts/build.sh
```

The binary is written to `dist/zap-<os>-<arch>`.

Install `zap` into a directory on your `PATH`:

```sh
./scripts/install.sh
```

Supported targets are macOS, Linux, and Windows on `amd64` and `arm64`.
