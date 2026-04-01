#!/bin/sh
# Valet installer — downloads the latest release from GitHub.
# Usage: curl -fsSL https://raw.githubusercontent.com/peterday/valet/main/install.sh | sh
#
# Installs to ~/.valet/bin/valet (no sudo required).

set -e

REPO="peterday/valet"
INSTALL_DIR="${HOME}/.valet/bin"
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

mkdir -p "$INSTALL_DIR"
tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"
mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

# On macOS, ad-hoc sign the binary to prevent Gatekeeper from killing it.
if [ "$OS" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
  codesign -s - -f "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
fi

echo ""
echo "valet v${LATEST} installed to ${INSTALL_DIR}/${BINARY}"

# --- Add to PATH if needed ---

if ! echo "$PATH" | grep -q "${INSTALL_DIR}"; then
  SHELL_NAME="$(basename "$SHELL")"
  PATH_LINE='export PATH="$HOME/.valet/bin:$PATH"'

  case "$SHELL_NAME" in
    zsh)  PROFILE="${HOME}/.zshrc" ;;
    bash)
      if [ -f "${HOME}/.bash_profile" ]; then
        PROFILE="${HOME}/.bash_profile"
      else
        PROFILE="${HOME}/.bashrc"
      fi
      ;;
    fish)
      PROFILE="${HOME}/.config/fish/config.fish"
      PATH_LINE="set -gx PATH ${INSTALL_DIR} \$PATH"
      ;;
    *)    PROFILE="" ;;
  esac

  if [ -n "$PROFILE" ]; then
    # Check if already in the profile.
    if ! grep -q '.valet/bin' "$PROFILE" 2>/dev/null; then
      printf "Add valet to your PATH in %s? [Y/n] " "$(basename "$PROFILE")"
      read -r ANSWER
      case "$ANSWER" in
        n|N|no|No) ;;
        *)
          echo "" >> "$PROFILE"
          echo "# valet" >> "$PROFILE"
          echo "$PATH_LINE" >> "$PROFILE"
          echo "Added to $(basename "$PROFILE"). Restart your shell or run:"
          echo "  source $PROFILE"
          ;;
      esac
    fi
  else
    echo ""
    echo "Add to your shell profile:"
    echo "  $PATH_LINE"
  fi
fi

echo ""
echo "Get started:"
echo "  valet identity init"
echo "  cd your-project && valet init"
