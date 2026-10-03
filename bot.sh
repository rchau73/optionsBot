#!/usr/bin/env bash
# bot.sh — Run on your LOCAL MACHINE (not inside the container).
#
# Starts the bot via Docker and monitors whether it is truly alive every
# CHECK_INTERVAL seconds. "Alive" means the bot has logged a heartbeat
# message within the last HEARTBEAT_WINDOW minutes — a running container
# with a hung bot does NOT pass this check.
#
# When the bot is unhealthy the whole container is restarted (SIGTERM →
# graceful shutdown → fresh container). The bot's own reconnect logic
# handles transient Deribit / rate-limit drops; this script handles the
# deeper "bot process is stuck" case that the reconnect loop cannot fix.
#
# Usage:
#   ./bot.sh                          — monitor the default 'bot-btc' service
#   ./bot.sh --service bot-btc        — monitor a specific service
#   ./bot.sh --once                   — start once and exit (no loop)
#   ./bot.sh --once --service bot-eth — start a specific service once
#   ./bot.sh --stop                   — stop all services
#   ./bot.sh --stop --service bot-btc — stop a specific service
#   caffeinate -i ./bot.sh            — macOS: prevent sleep while monitor runs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE="docker compose -f $SCRIPT_DIR/docker-compose.yml"
SERVICE=bot-btc   # default; override with --service

CHECK_INTERVAL=300   # seconds between health checks (5 min)
HEARTBEAT_WINDOW=3m  # docker logs --since window; bot heartbeats every 60s,
                     # so 3 missed = definitely hung
STARTUP_GRACE=120    # seconds to wait after first start before heartbeat checks begin

# ── argument parsing ──────────────────────────────────────────────────────────

CMD=""   # --once | --stop | "" (monitor loop)

while [[ $# -gt 0 ]]; do
    case "$1" in
        --service)
            [[ $# -lt 2 ]] && { echo "Usage: --service <service-name>"; exit 1; }
            SERVICE="$2"
            shift 2
            ;;
        --once|--stop)
            CMD="$1"
            shift
            ;;
        *)
            echo "Usage: $0 [--service <name>] [--once|--stop]"
            exit 1
            ;;
    esac
done

# ── helpers ───────────────────────────────────────────────────────────────────

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [${SERVICE}] $*"
}

check_docker() {
    if ! docker info >/dev/null 2>&1; then
        log "ERROR — Docker is not running. Start Docker Desktop and try again."
        exit 1
    fi
}

container_is_running() {
    $COMPOSE ps --services --status running 2>/dev/null | grep -q "^${SERVICE}$"
}

# Returns 0 (healthy) only if the container is up AND the bot logged a
# heartbeat in the last HEARTBEAT_WINDOW.  A hung-but-running container fails.
bot_is_alive() {
    if ! container_is_running; then
        log "WARN — container is not running"
        return 1
    fi
    if ! $COMPOSE logs --since "$HEARTBEAT_WINDOW" "$SERVICE" 2>/dev/null \
            | grep -q '"heartbeat"'; then
        log "WARN — container is up but no heartbeat in last ${HEARTBEAT_WINDOW} (bot may be hung)"
        return 1
    fi
    return 0
}

restart_bot() {
    log "Restarting bot container..."
    # SIGTERM lets the bot close positions / flush logs gracefully before Docker
    # force-kills it. If it doesn't exit within 30 s Docker sends SIGKILL.
    $COMPOSE restart "$SERVICE"
    log "Restart issued — waiting ${STARTUP_GRACE}s for bot to come back online"
    sleep "$STARTUP_GRACE"
    if bot_is_alive; then
        log "OK — bot is back online"
    else
        log "ERROR — bot still not healthy after restart; will retry on next check"
    fi
}

ensure_running() {
    if bot_is_alive; then
        log "OK — bot is alive (heartbeat seen within ${HEARTBEAT_WINDOW})"
    else
        restart_bot
    fi
}

# ── commands ──────────────────────────────────────────────────────────────────

check_docker

case "$CMD" in
    --stop)
        if [[ "$SERVICE" == "bot" ]]; then
            log "Stopping all services..."
            $COMPOSE down
            log "All services stopped"
        else
            log "Stopping service..."
            $COMPOSE stop "$SERVICE"
            log "Service stopped"
        fi
        exit 0
        ;;
    --once)
        if ! container_is_running; then
            log "Starting service..."
            $COMPOSE up -d --build "$SERVICE"
        else
            log "Service already running"
        fi
        exit 0
        ;;
esac

# ── main: start + monitor loop ────────────────────────────────────────────────

if ! container_is_running; then
    log "Starting bot container..."
    $COMPOSE up -d --build "$SERVICE"
    log "Waiting ${STARTUP_GRACE}s for bot to initialise before first heartbeat check..."
    sleep "$STARTUP_GRACE"
else
    log "Bot container already running"
fi

log "Monitor started — checking every ${CHECK_INTERVAL}s"
log "Ctrl+C exits the monitor; bot keeps running in Docker"
log "Logs: docker compose logs -f ${SERVICE}"
echo ""

while true; do
    sleep "$CHECK_INTERVAL"
    ensure_running
done
