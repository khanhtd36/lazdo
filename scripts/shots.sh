#!/bin/sh
# Regenerates docs/screenshots from made-up data: TestShots writes each
# screen as an HTML page, then a headless Edge or Chrome photographs it.
# Run from the repo root. Needs the Cascadia Mono font for lines to join.
set -e
out="$(pwd)/docs/screenshots"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

SHOTS_DIR="$tmp" go test ./internal/ui -run '^TestShots$' -count=1 >/dev/null

browser="${BROWSER:-}"
if [ -z "$browser" ]; then
	for b in \
		"/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe" \
		"/c/Program Files/Google/Chrome/Application/chrome.exe" \
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
		microsoft-edge google-chrome chromium; do
		if [ -x "$b" ] || command -v "$b" >/dev/null 2>&1; then
			browser="$b"
			break
		fi
	done
fi
[ -n "$browser" ] || { echo "no Edge or Chrome found; set BROWSER" >&2; exit 1; }

# Windows browsers want Windows paths.
path() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else echo "$1"; fi; }

for page in "$tmp"/*.html; do
	name="$(basename "$page" .html)"
	"$browser" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
		--window-size=1290,420 --screenshot="$(path "$out/$name.png")" "file:///$(path "$page")" >/dev/null 2>&1
	echo "$out/$name.png"
done
