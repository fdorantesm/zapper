#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_SCRIPT="$ROOT_DIR/scripts/build.sh"
BINARY_NAME="zap"

detect_goos() {
  case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
    linux*) echo "linux" ;;
    darwin*) echo "darwin" ;;
    mingw*|msys*|cygwin*) echo "windows" ;;
    *) echo "unsupported" ;;
  esac
}

detect_goarch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    armv7l) echo "arm" ;;
    *) echo "unsupported" ;;
  esac
}

path_contains() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

choose_install_dir() {
  if [[ -n "${MOP_INSTALL_DIR:-}" ]]; then
    echo "$MOP_INSTALL_DIR"
    return
  fi

  if [[ "$(detect_goos)" == "windows" ]]; then
    echo "$HOME/bin"
    return
  fi

  if [[ -d "/usr/local/bin" && -w "/usr/local/bin" ]]; then
    echo "/usr/local/bin"
    return
  fi

  echo "$HOME/.local/bin"
}

add_install_dir_to_path() {
  local install_dir="$1"

  if path_contains "$install_dir"; then
    return
  fi

  local shell_profile="$HOME/.profile"
  if [[ -n "${ZSH_VERSION:-}" ]]; then
    shell_profile="$HOME/.zshrc"
  elif [[ -n "${BASH_VERSION:-}" ]]; then
    shell_profile="$HOME/.bashrc"
  fi

  printf '\nexport PATH="%s:$PATH"\n' "$install_dir" >> "$shell_profile"
  echo "Added $install_dir to PATH in $shell_profile. Restart your shell or run:"
  echo "export PATH=\"$install_dir:\$PATH\""

  if [[ "$(detect_goos)" == "windows" ]] && command -v powershell.exe >/dev/null 2>&1 && command -v cygpath >/dev/null 2>&1; then
    local win_install_dir
    win_install_dir="$(cygpath -w "$install_dir")"
    WIN_INSTALL_DIR="$win_install_dir" powershell.exe -NoProfile -Command '
      $dir = $env:WIN_INSTALL_DIR
      $path = [Environment]::GetEnvironmentVariable("Path", "User")
      if (($path -split ";") -notcontains $dir) {
        [Environment]::SetEnvironmentVariable("Path", "$dir;$path", "User")
      }
    ' >/dev/null
    echo "Updated the Windows user PATH. Open a new terminal for it to take effect."
  fi
}

GOOS_VALUE="$(detect_goos)"
GOARCH_VALUE="$(detect_goarch)"

if [[ "$GOOS_VALUE" == "unsupported" || "$GOARCH_VALUE" == "unsupported" ]]; then
  echo "Unsupported OS or architecture: $(uname -s) $(uname -m)" >&2
  exit 1
fi

EXT=""
if [[ "$GOOS_VALUE" == "windows" ]]; then
  EXT=".exe"
fi

echo "Detected $GOOS_VALUE/$GOARCH_VALUE"
ARTIFACT="$(GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" "$BUILD_SCRIPT" | tail -n 1)"
INSTALL_DIR="$(choose_install_dir)"
mkdir -p "$INSTALL_DIR"

INSTALL_PATH="$INSTALL_DIR/$BINARY_NAME$EXT"
cp "$ARTIFACT" "$INSTALL_PATH"
chmod +x "$INSTALL_PATH" 2>/dev/null || true

add_install_dir_to_path "$INSTALL_DIR"

echo "Installed $BINARY_NAME to $INSTALL_PATH"
echo "Run: $BINARY_NAME --dry-run"
