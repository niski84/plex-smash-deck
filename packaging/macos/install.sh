#!/usr/bin/env bash
# Plex Smash Deck — macOS install helper
# Downloads the correct binary for your Mac and installs it to ~/bin.
# Usage:  bash install.sh
#   or:   bash install.sh --uninstall

set -euo pipefail

APP_NAME="plex-smash-deck"
INSTALL_DIR="$HOME/bin"
BINARY="$INSTALL_DIR/$APP_NAME"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST_ID="com.plex-smash-deck.server"
PLIST_FILE="$PLIST_DIR/$PLIST_ID.plist"
GITHUB_REPO="nicholasgasior/plex-smash-deck"  # update if repo changes

# ── helpers ──────────────────────────────────────────────────────────────────
log()  { printf '\033[0;32m▶ %s\033[0m\n' "$*"; }
warn() { printf '\033[0;33m⚠ %s\033[0m\n' "$*"; }
die()  { printf '\033[0;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ── detect arch ──────────────────────────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  GO_ARCH="amd64" ;;
  arm64)   GO_ARCH="arm64" ;;
  *)       die "Unsupported architecture: $ARCH" ;;
esac

# ── uninstall path ────────────────────────────────────────────────────────────
if [[ "${1:-}" == "--uninstall" ]]; then
  log "Stopping launchd service (if running)…"
  launchctl unload "$PLIST_FILE" 2>/dev/null || true
  rm -f "$PLIST_FILE"
  rm -f "$BINARY"
  log "Uninstalled. Data folder at ~/Library/Application Support/plex-smash-deck is NOT removed."
  exit 0
fi

# ── install ──────────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"

# Fetch latest release tag from GitHub
log "Fetching latest release info…"
LATEST=$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases/latest" \
  | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
[[ -z "$LATEST" ]] && die "Could not determine latest release. Check your internet connection."

ASSET="${APP_NAME}-darwin-${GO_ARCH}"
URL="https://github.com/$GITHUB_REPO/releases/download/$LATEST/$ASSET"

log "Downloading $APP_NAME $LATEST for darwin/$GO_ARCH…"
curl -fsSL "$URL" -o "$BINARY"
chmod +x "$BINARY"
log "Installed to $BINARY"

# ── launchd plist ─────────────────────────────────────────────────────────────
mkdir -p "$PLIST_DIR"
cat > "$PLIST_FILE" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${PLIST_ID}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${BINARY}</string>
    <string>--no-browser</string>
  </array>
  <key>RunAtLoad</key>
  <false/>
  <key>KeepAlive</key>
  <false/>
  <key>StandardOutPath</key>
  <string>${HOME}/Library/Logs/plex-smash-deck.log</string>
  <key>StandardErrorPath</key>
  <string>${HOME}/Library/Logs/plex-smash-deck.log</string>
  <key>WorkingDirectory</key>
  <string>${HOME}/Library/Application Support/plex-smash-deck</string>
</dict>
</plist>
PLIST

log "Launchd plist installed at $PLIST_FILE"

# ── ensure ~/bin is in PATH ───────────────────────────────────────────────────
SHELL_RC=""
case "$SHELL" in
  */zsh)  SHELL_RC="$HOME/.zshrc" ;;
  */bash) SHELL_RC="$HOME/.bash_profile" ;;
esac
if [[ -n "$SHELL_RC" ]] && ! grep -q "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
  echo 'export PATH="$HOME/bin:$PATH"' >> "$SHELL_RC"
  warn "Added $INSTALL_DIR to PATH in $SHELL_RC — restart your terminal or run: source $SHELL_RC"
fi

# ── create data dir ───────────────────────────────────────────────────────────
DATA_DIR="$HOME/Library/Application Support/plex-smash-deck"
mkdir -p "$DATA_DIR"

# ── done ─────────────────────────────────────────────────────────────────────
cat <<MSG

  Installation complete!

  To run Plex Smash Deck:
    $BINARY

  This opens a server and launches your browser automatically.

  To start it as a background service (no browser, no window):
    launchctl load $PLIST_FILE
  To stop the background service:
    launchctl unload $PLIST_FILE

  Config (.env file) and data go here:
    $DATA_DIR

  To uninstall:
    bash install.sh --uninstall

MSG
