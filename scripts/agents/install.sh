#!/usr/bin/env bash
# install.sh — schedule the agents with launchd on this Mac. Run it from the
# mvp clone (~/optionsbot-env/mvp/scripts/agents/install.sh): the jobs run the
# scripts of the clone they were installed from, and deploy.sh keeps that clone
# on main.
#
#   scripts/agents/install.sh              SME review daily at 09:00, tick every 15 min
#   scripts/agents/install.sh 07:30        SME review at another time
#   scripts/agents/install.sh --uninstall  remove both jobs
#
# Jobs: com.optionsbot.sme-review (sme_review.sh) and com.optionsbot.agents-tick
# (tick.sh: deploy, the Reviewer agent, the PM agent, then the Dev agent). launchd runs a
# missed calendar job on wake. Logs: data/sme/, data/agents/ and data/agents/launchd-*.log.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENTS="$HOME/Library/LaunchAgents"
SME="com.optionsbot.sme-review"
TICK="com.optionsbot.agents-tick"
uid="$(id -u)"

unload() { launchctl bootout "gui/$uid/$1" 2>/dev/null || true; rm -f "$AGENTS/$1.plist"; }
if [[ "${1:-}" == "--uninstall" ]]; then
  unload "$SME"; unload "$TICK"; echo "uninstalled $SME and $TICK"; exit 0
fi

AT="${1:-09:00}"
[[ "$AT" =~ ^([01][0-9]|2[0-3]):([0-5][0-9])$ ]] || { echo "time must be HH:MM, got $AT" >&2; exit 1; }
HOUR=$((10#${AT%%:*})); MIN=$((10#${AT##*:}))

for bin in claude gh curl python3 docker go gofmt; do
  command -v "$bin" >/dev/null || { echo "$bin not found on PATH" >&2; exit 1; }
done
gh auth status >/dev/null 2>&1 || { echo "gh is not logged in" >&2; exit 1; }
# launchd starts with a bare PATH: carry the directories that hold every tool
# an agent or a check runs (go was missing on 2026-10-15: the Dev agent could
# not test and the script's checks failed with "go: command not found").
PATH_FOR_JOBS="$(for b in claude gh docker go gofmt python3 node npm npx mmdc; do
  command -v "$b" >/dev/null && dirname "$(command -v "$b")"; done | awk '!seen[$0]++' | tr '\n' ':')/usr/bin:/bin:/usr/sbin:/sbin"
mkdir -p "$AGENTS" "$REPO/data/agents"

plist() { # label, script, schedule xml
  cat > "$AGENTS/$1.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$1</string>
  <key>ProgramArguments</key><array><string>/bin/bash</string><string>$REPO/scripts/agents/$2</string></array>
  <key>WorkingDirectory</key><string>$REPO</string>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>$PATH_FOR_JOBS</string></dict>
  $3
  <key>StandardOutPath</key><string>$REPO/data/agents/launchd-$2.log</string>
  <key>StandardErrorPath</key><string>$REPO/data/agents/launchd-$2.log</string>
</dict>
</plist>
EOF
  launchctl bootout "gui/$uid/$1" 2>/dev/null || true
  launchctl bootstrap "gui/$uid" "$AGENTS/$1.plist"
}
plist "$SME" sme_review.sh "<key>StartCalendarInterval</key><dict><key>Hour</key><integer>$HOUR</integer><key>Minute</key><integer>$MIN</integer></dict>"
plist "$TICK" tick.sh "<key>StartInterval</key><integer>900</integer>"

echo "installed $SME (daily at $AT) and $TICK (every 15 min) for $REPO"
echo "run now:  launchctl kickstart gui/$uid/$SME   |   launchctl kickstart gui/$uid/$TICK"
