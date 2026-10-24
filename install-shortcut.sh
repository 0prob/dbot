#!/usr/bin/env bash
# Installs a menu entry and a desktop shortcut for DuckDice Bot.
# Run from anywhere; it points the shortcut at this checkout so profiles/
# and logs/ stay in this folder. Re-run after moving the folder.
set -euo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin="$dir/duckdice-bot"
[[ -x "$bin" ]] || { echo "build first: go build -trimpath -ldflags '-s -w' -o duckdice-bot ." >&2; exit 1; }

icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
desk="$(xdg-user-dir DESKTOP 2>/dev/null || echo "$HOME/Desktop")"
mkdir -p "$icons" "$apps" "$desk"
install -m 644 "$dir/assets/icon.png" "$icons/duckdice-bot.png"

entry="$apps/duckdice-bot.desktop"
cat > "$entry" <<DESKTOP
[Desktop Entry]
Type=Application
Version=1.0
Name=DuckDice Bot
Comment=Desktop client for the DuckDice Bot API
Exec="$bin"
Path=$dir
Icon=duckdice-bot
Terminal=false
Categories=Game;
DESKTOP
chmod 755 "$entry"
install -m 755 "$entry" "$desk/duckdice-bot.desktop"

update-desktop-database "$apps" >/dev/null 2>&1 || true
gtk-update-icon-cache -q "${icons%/256x256/apps}" >/dev/null 2>&1 || true
echo "Installed: $entry"
echo "Desktop:   $desk/duckdice-bot.desktop"
