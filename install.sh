#!/usr/bin/env sh
set -eu

REPO="Verzional/agy-statusline"
BINARY="agy-statusline"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  darwin|linux) ;;
  *)
    printf 'Unsupported OS: %s\n' "$OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    printf 'Unsupported architecture: %s\n' "$ARCH" >&2
    exit 1
    ;;
esac

TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
if [ -z "$TAG" ]; then
  printf 'Failed to resolve latest release tag for %s\n' "$REPO" >&2
  exit 1
fi

ARCHIVE="${BINARY}_${TAG}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

printf 'Downloading %s (%s/%s)...\n' "$TAG" "$OS" "$ARCH"
curl -fsSL "$URL" -o "${TMP_DIR}/${ARCHIVE}"
tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR"

mkdir -p "$INSTALL_DIR"
install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"

# Update legacy symlink if present
LEGACY_LINK="$HOME/.gemini/antigravity-cli/statusline.sh"
if [ -L "$LEGACY_LINK" ]; then
  ln -sf "${INSTALL_DIR}/${BINARY}" "$LEGACY_LINK"
fi

printf 'Installed %s to %s/%s\n' "$BINARY" "$INSTALL_DIR" "$BINARY"
