#!/usr/bin/env bash
# Build berrybot for iOS Simulator, patch the bundle, install, and launch.
# Requires: gomobile, a booted iOS Simulator.
# Usage: scripts/build-sim.sh
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUNDLE_ID="com.pawelkowalak.berrybot"
APP="$REPO_ROOT/berrybot.app"

echo "==> Building for iOS Simulator..."
cd "$REPO_ROOT"
gomobile build -target=iossimulator -bundleid "$BUNDLE_ID" .

echo "==> Patching bundle for Simulator..."
"$SCRIPT_DIR/ios-merge-plist.sh" "$APP"

# Read the actual bundle ID (gomobile appends the Go package name).
REAL_BUNDLE_ID=$(/usr/libexec/PlistBuddy -c "Print :CFBundleIdentifier" "$APP/Info.plist")

echo "==> Installing on booted Simulator..."
xcrun simctl install booted "$APP"

echo "==> Launching $REAL_BUNDLE_ID..."
xcrun simctl launch booted "$REAL_BUNDLE_ID"

echo "==> Done. Check the Simulator."
