#!/bin/sh
# Valet installer — downloads the latest release from GitHub.
# Usage: curl -fsSL https://raw.githubusercontent.com/peterday/valet/main/install.sh | sh

set -e

REPO="peterday/valet"
INSTALL_DIR="/usr/local/bin"
BINARY="valet"

# --- Detect platform ---

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# --- Find latest release ---

echo "Finding latest valet release..."

LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "Error: could not determine latest release."
  echo "Check https://github.com/${REPO}/releases"
  exit 1
fi

echo "Latest version: v${LATEST}"

# --- Download ---

FILENAME="valet_${LATEST}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${LATEST}/${FILENAME}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading ${URL}..."
curl -fsSL "$URL" -o "${TMPDIR}/${FILENAME}"

# --- Verify checksum ---

CHECKSUM_URL="https://github.com/${REPO}/releases/download/v${LATEST}/checksums.txt"
if curl -fsSL "$CHECKSUM_URL" -o "${TMPDIR}/checksums.txt" 2>/dev/null; then
  EXPECTED=$(grep "$FILENAME" "${TMPDIR}/checksums.txt" | awk '{print $1}')
  if [ -n "$EXPECTED" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      ACTUAL=$(sha256sum "${TMPDIR}/${FILENAME}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
      ACTUAL=$(shasum -a 256 "${TMPDIR}/${FILENAME}" | awk '{print $1}')
    fi
    if [ -n "$ACTUAL" ] && [ "$EXPECTED" != "$ACTUAL" ]; then
      echo "Checksum mismatch!"
      echo "  Expected: $EXPECTED"
      echo "  Got:      $ACTUAL"
      exit 1
    fi
    echo "Checksum verified."
  fi
fi

# --- Install ---

tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

echo ""
echo "valet v${LATEST} installed to ${INSTALL_DIR}/${BINARY}"
echo ""
echo "Get started:"
echo "  valet identity init"
echo "  cd your-project && valet init"
