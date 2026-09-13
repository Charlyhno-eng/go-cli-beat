#!/usr/bin/env bash
# install.sh — install system dependencies and compile go-cli-beat
set -euo pipefail

echo "🎛️  go-cli-beat — build"
echo ""

# ── System dependencies (ALSA + CGo) ──────────────────────────────────────────
if command -v apt-get &>/dev/null; then
    echo "→ Installing audio and decoder dependencies..."
    sudo apt-get install -y libasound2-dev gcc ffmpeg
elif command -v dnf &>/dev/null; then
    sudo dnf install -y alsa-lib-devel gcc ffmpeg
elif command -v pacman &>/dev/null; then
    sudo pacman -S --noconfirm alsa-lib gcc ffmpeg
else
    echo "⚠  Please install ALSA development files, gcc, and ffmpeg manually."
fi

echo ""
echo "→ Downloading Go dependencies..."
go mod tidy

echo ""
echo "→ Building..."
go build -o go-cli-beat .

echo ""
echo "✅  Done!"
echo ""
echo "Edit config.json to set your music directory, then run:"
echo "  ./go-cli-beat"
echo ""
echo "Or pass a path directly:"
echo "  ./go-cli-beat /path/to/your/music"
