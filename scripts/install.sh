#!/usr/bin/env bash
set -eo pipefail

BINARY_NAME="zap"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
SRC_DIR="$ROOT_DIR/src"

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
    *) echo "unsupported" ;;
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

GOOS_VALUE="$(detect_goos)"
GOARCH_VALUE="$(detect_goarch)"

if [[ "$GOOS_VALUE" == "unsupported" || "$GOARCH_VALUE" == "unsupported" ]]; then
  echo "Unsupported: $(uname -s) $(uname -m)" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required" >&2
  exit 1
fi

if [[ ! -d "$SRC_DIR" ]]; then
  echo "Error: source not found at $SRC_DIR" >&2
  exit 1
fi

INSTALL_DIR="$(choose_install_dir)"
mkdir -p "$INSTALL_DIR"

EXT=""
[[ "$GOOS_VALUE" == "windows" ]] && EXT=".exe"
INSTALL_PATH="$INSTALL_DIR/$BINARY_NAME$EXT"

cd "$ROOT_DIR"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
VERSION=${VERSION#v}

echo "Building $BINARY_NAME $VERSION for $GOOS_VALUE/$GOARCH_VALUE..."
GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" go build \
    -trimpath \
    -ldflags "-s -w -X main.appVersion=$VERSION" \
    -o "$INSTALL_PATH" \
    ./src

chmod +x "$INSTALL_PATH" 2>/dev/null || true

if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  echo "Add to PATH: export PATH=\"$INSTALL_DIR:\$PATH\""
fi

echo "Installed $BINARY_NAME to $INSTALL_PATH"