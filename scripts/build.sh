#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-$ROOT_DIR/dist}"
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

VERSION="${VERSION:-}"
if [[ -z "$VERSION" ]]; then
  VERSION="$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo "dev")"
  VERSION="${VERSION#v}"
fi

TARGET_GOOS="${GOOS:-$(detect_goos)}"
TARGET_GOARCH="${GOARCH:-$(detect_goarch)}"

if [[ "$TARGET_GOOS" == "unsupported" || "$TARGET_GOARCH" == "unsupported" ]]; then
  echo "Unsupported OS or architecture: $(uname -s) $(uname -m)" >&2
  exit 1
fi

EXT=""
if [[ "$TARGET_GOOS" == "windows" ]]; then
  EXT=".exe"
fi

mkdir -p "$DIST_DIR"
OUTPUT="$DIST_DIR/$BINARY_NAME-$TARGET_GOOS-$TARGET_GOARCH$EXT"

echo "Building $BINARY_NAME $VERSION for $TARGET_GOOS/$TARGET_GOARCH"
(
  cd "$ROOT_DIR"
  GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" go build \
    -trimpath \
    -ldflags "-s -w -X main.appVersion=$VERSION" \
    -o "$OUTPUT" \
    .
)

echo "$OUTPUT"
