#!/bin/sh
set -e

BINARY_NAME="zap"
GITHUB_REPO="fdorantesm/zapper"

detect_platform() {
    platform="$(uname -ms)"
    case "$platform" in
        'Linux x86_64'|'Linux amd64')   echo "linux-amd64" ;;
        'Linux aarch64'|'Linux arm64')   echo "linux-arm64" ;;
        'Darwin x86_64'|'Darwin amd64')   echo "darwin-amd64" ;;
        'Darwin arm64')                   echo "darwin-arm64" ;;
        'WindowsNT x86_64'|'WindowsNT amd64') echo "windows-amd64.exe" ;;
        'WindowsNT arm64')               echo "windows-arm64.exe" ;;
        *) echo "unsupported" ;;
    esac
}

get_install_dir() {
    case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
        linux*|darwin*) echo "/usr/local/bin" ;;
        mingw*|msys*|cygwin*) echo "C:\\Windows\\System32" ;;
        *) echo "/usr/local/bin" ;;
    esac
}

get_latest_version() {
    curl --silent "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
}

install_bin() {
    target="$1"
    url="https://github.com/$GITHUB_REPO/releases/download/$VERSION/zap-$target"

    if command -v curl >/dev/null 2>&1; then
        curl -L "$url" -o "$INSTALL_DIR/$BINARY_NAME"
    elif command -v wget >/dev/null 2>&1; then
        wget -O "$INSTALL_DIR/$BINARY_NAME" "$url"
    else
        echo "Error: curl or wget is required"
        exit 1
    fi
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
}

echo "Installing $BINARY_NAME..."

TARGET="$(detect_platform)"
if [ "$TARGET" = "unsupported" ]; then
    echo "Unsupported platform: $(uname -ms)"
    exit 1
fi

VERSION="$(get_latest_version)"
if [ -z "$VERSION" ]; then
    echo "Error: Could not fetch latest version"
    exit 1
fi

INSTALL_DIR="$(get_install_dir)"

if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR"
fi

install_bin "$TARGET"

echo "$BINARY_NAME installed successfully to $INSTALL_DIR/$BINARY_NAME"