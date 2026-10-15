#!/usr/bin/env bash
# install_sme_review.sh — schedule scripts/sme_review.sh daily with launchd.
#
#   scripts/install_sme_review.sh            install (daily at 09:00 local)
#   scripts/install_sme_review.sh 07:30      install at another time
#   scripts/install_sme_review.sh --uninstall
#
# launchd runs a missed job when the Mac wakes, so a review skipped during
# sleep still happens. Logs: data/sme/<date>/run.log and data/sme/launchd.log.
set -euo pipefail

LABEL="com.optionsbot.sme-review"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "${1:-}" == "--uninstall" ]]; then
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$PLIST"
  echo "uninstalled $LABEL"
  exit 0
fi

AT="${1:-09:00}"
if ! [[ "$AT" =~ ^([01][0-9]|2[0-3]):([0-5][0-9])$ ]]; then
  echo "time must be HH:MM, got $AT" >&2
  exit 1
fi
HOUR=$((10#${AT%%:*}))
MIN=$((10#${AT##*:}))

for bin in claude curl python3; do
  command -v "$bin" >/dev/null || { echo "$bin not found on PATH" >&2; exit 1; }
done
# launchd starts with a bare PATH: carry the one that finds claude, gh, curl.
PATH_FOR_JOB="$(dirname "$(command -v claude)"):/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

mkdir -p "$HOME/Library/LaunchAgents" "$REPO/data/sme"
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>$REPO/scripts/sme_review.sh</string>
  </array>
  <key>WorkingDirectory</key><string>$REPO</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$PATH_FOR_JOB</string>
  </dict>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key><integer>$HOUR</integer>
    <key>Minute</key><integer>$MIN</integer>
  </dict>
  <key>StandardOutPath</key><string>$REPO/data/sme/launchd.log</string>
  <key>StandardErrorPath</key><string>$REPO/data/sme/launchd.log</string>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
echo "installed $LABEL: daily at $AT, runs $REPO/scripts/sme_review.sh"
echo "run it now:  launchctl kickstart gui/$(id -u)/$LABEL"
