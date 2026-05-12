#!/usr/bin/env bash
set -eo pipefail

# This script installs zap by building it from source.
# It can be run locally or via:
# curl -fsSL https://raw.githubusercontent.com/fdorantesm/go-zapper/main/scripts/install.sh | bash

BINARY_NAME="zap"
REPO_URL="https://github.com/fdorantesm/go-zapper"

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
  if [[ -n "${ZAP_INSTALL_DIR:-}" ]]; then
    echo "$ZAP_INSTALL_DIR"
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

  local shell_profile=""
  if [[ -n "${ZSH_VERSION:-}" ]]; then
    shell_profile="$HOME/.zshrc"
  elif [[ -n "${BASH_VERSION:-}" ]]; then
    shell_profile="$HOME/.bashrc"
  else
    shell_profile="$HOME/.profile"
  fi

  if [[ -f "$shell_profile" ]]; then
    printf '\nexport PATH="%s:$PATH"\n' "$install_dir" >> "$shell_profile"
    echo "Added $install_dir to PATH in $shell_profile."
  fi
  
  echo "Restart your shell or run:"
  echo "export PATH=\"$install_dir:\$PATH\""
}

GOOS_VALUE="$(detect_goos)"
GOARCH_VALUE="$(detect_goarch)"

if [[ "$GOOS_VALUE" == "unsupported" || "$GOARCH_VALUE" == "unsupported" ]]; then
  echo "Unsupported OS or architecture: $(uname -s) $(uname -m)" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is not installed. Please install Go to build $BINARY_NAME." >&2
  exit 1
fi

if ! command -v git >/dev/null 2>&1; then
  echo "Error: git is not installed. Please install git to clone the repository." >&2
  exit 1
fi

echo "Detected $GOOS_VALUE/$GOARCH_VALUE"

# Create a temporary directory for building
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Cloning $REPO_URL..."
git clone --depth 1 "$REPO_URL" "$TMP_DIR/repo" >/dev/null 2>&1

echo "Building $BINARY_NAME..."
INSTALL_DIR="$(choose_install_dir)"
mkdir -p "$INSTALL_DIR"

EXT=""
[[ "$GOOS_VALUE" == "windows" ]] && EXT=".exe"
INSTALL_PATH="$INSTALL_DIR/$BINARY_NAME$EXT"

cd "$TMP_DIR/repo"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
VERSION=${VERSION#v}

GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" go build \
    -trimpath \
    -ldflags "-s -w -X main.appVersion=$VERSION" \
    -o "$INSTALL_PATH" \
    ./src

chmod +x "$INSTALL_PATH" 2>/dev/null || true

add_install_dir_to_path "$INSTALL_DIR"

echo "Successfully installed $BINARY_NAME to $INSTALL_PATH"
echo "Run: $BINARY_NAME --dry-run"

