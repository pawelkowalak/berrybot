#!/usr/bin/env bash
# Merges ios/Info-additions.plist into a gomobile-built .app bundle,
# fixes platform metadata for Simulator, and re-signs with a dev certificate.
# Run after: gomobile build -target=iossimulator -bundleid ... .
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

# Merge custom plist additions (e.g. NSLocalNetworkUsageDescription).
/usr/libexec/PlistBuddy -c "Merge $ADDITIONS :" "$PLIST"
echo "Merged $ADDITIONS into $PLIST"

# Fix platform metadata: gomobile writes iPhoneOS even for -target=iossimulator.
PB=/usr/libexec/PlistBuddy
$PB -c "Delete :CFBundleSupportedPlatforms" "$PLIST"
$PB -c "Add :CFBundleSupportedPlatforms array" "$PLIST"
$PB -c "Add :CFBundleSupportedPlatforms:0 string iPhoneSimulator" "$PLIST"
$PB -c "Set :DTPlatformName iphonesimulator" "$PLIST"

SDK_VER=$($PB -c "Print :DTPlatformVersion" "$PLIST" 2>/dev/null || echo "")
if [[ -n "$SDK_VER" ]]; then
  $PB -c "Set :DTSDKName iphonesimulator${SDK_VER}" "$PLIST"
fi

$PB -c "Delete :UIRequiredDeviceCapabilities" "$PLIST" 2>/dev/null || true
echo "Fixed platform metadata for iPhoneSimulator"

# Remove embedded.mobileprovision (not needed on Simulator) and quarantine xattrs.
rm -f "$APP/embedded.mobileprovision"
xattr -rc "$APP" 2>/dev/null || true

# Resolve the first Apple Development signing identity.
SIGN_ID=$(security find-identity -v -p codesigning | grep "Apple Development" | head -1 | sed 's/.*"\(.*\)".*/\1/')
if [[ -z "$SIGN_ID" ]]; then
  echo "No Apple Development signing identity found; falling back to ad-hoc." >&2
  SIGN_ID="-"
fi

# Re-sign without entitlements — AMFI rejects get-task-allow unless a
# provisioning profile authorises it, and Simulator doesn't need it.
echo "Re-signing $APP with identity: $SIGN_ID"
codesign --force --sign "$SIGN_ID" "$APP"
echo "Done."
