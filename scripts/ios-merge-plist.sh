#!/usr/bin/env bash
# Merges ios/Info-additions.plist into a gomobile-built .app bundle, then re-signs.
# Modifying the bundle invalidates the code signature; Simulator (and device) require a valid signature.
# Run after: gomobile build -target=ios/arm64 -bundleid ... -o berrybot.app .
# Usage: scripts/ios-merge-plist.sh [path/to/App.app]
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
APP="${1:-$REPO_ROOT/berrybot.app}"
PLIST="$APP/Info.plist"
ADDITIONS="$REPO_ROOT/ios/Info-additions.plist"

if [[ ! -d "$APP" ]]; then
  echo "Usage: $0 [path/to/App.app]" >&2
  echo "App bundle not found: $APP" >&2
  exit 1
fi
if [[ ! -f "$PLIST" ]]; then
  echo "Info.plist not found: $PLIST" >&2
  exit 1
fi
if [[ ! -f "$ADDITIONS" ]]; then
  echo "Additions plist not found: $ADDITIONS" >&2
  exit 1
fi

/usr/libexec/PlistBuddy -c "Merge $ADDITIONS :" "$PLIST"
echo "Merged $ADDITIONS into $PLIST"

# Re-sign the bundle after modifying contents (required for Simulator and device).
echo "Re-signing $APP (ad-hoc for Simulator; use Xcode/codesign for device if needed)"
codesign -s - -f "$APP"
echo "Done."
