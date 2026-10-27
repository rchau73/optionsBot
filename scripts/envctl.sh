#!/usr/bin/env bash
# envctl.sh — run a local environment. The only way a stack is started.
#
#   scripts/envctl.sh <stable|mvp> init      clone the repo into <envs>/<env> (scripts/env_lib.sh)
#                                           and write its deploy.env (then add its .env)
#   scripts/envctl.sh <stable|mvp> check     is it set up, and does it share nothing
#                                           (Deribit account, monitor port) with the other?
#   scripts/envctl.sh <stable|mvp> up        build and start (after check)
#   scripts/envctl.sh <stable|mvp> down|ps|logs|restart|stop|start|exec|run …   (docker compose, this stack)
#   scripts/envctl.sh <stable|mvp> status    version, containers, monitor URL
#
# stable moves only with scripts/promote.sh; mvp moves with every merge
# (scripts/agents/deploy.sh). See docs/operations.md "Environments".
set -euo pipefail
# shellcheck source=env_lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/env_lib.sh"

name="${1:-}"; cmd="${2:-status}"
[[ -n "$name" ]] || { sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 1; }
shift 2 2>/dev/null || shift $#
dir="$(env_dir "$name")"

case "$cmd" in
  init)
    why="$(env_not_in_clone)" || { echo "refusing to init $name: $why" >&2; exit 1; }
    case " $ENV_NAMES " in *" $name "*) ;; *) echo "unknown environment '$name' (stable | mvp)" >&2; exit 1 ;; esac
    if [[ ! -d "$dir/.git" ]]; then
      mkdir -p "$ENVS_DIR"
      git clone -q "$(git -C "$(dirname "${BASH_SOURCE[0]}")/.." remote get-url origin)" "$dir"
      echo "cloned into $dir"
    fi
    if [[ ! -f "$dir/deploy.env" ]]; then
      if [[ "$name" == "stable" ]]; then port=3000; rec=1; else port=3100; rec=0; fi
      printf 'OPTIONSBOT_ENV=%s\nMONITOR_PORT=%s\nBOT_RECORD_MARKET=%s\n' "$name" "$port" "$rec" > "$dir/deploy.env"
      echo "wrote $dir/deploy.env (monitor :$port, market recorder $rec)"
    fi
    [[ -f "$dir/.env" ]] || echo "next: create $dir/.env with this environment's OWN Deribit subaccount (see .env.example)"
    ;;
  check)
    env_check "$name" && echo "$name: ok ($dir, $(env_version "$name"), monitor $(env_monitor_url "$name"))" ;;
  up)
    why="$(env_check "$name")" || { echo "refusing to start $name: $why" >&2; exit 1; }
    env_compose "$name" up -d --build "$@"
    echo "$name $(env_version "$name") → $(env_monitor_url "$name")" ;;
  down|ps|logs|restart|stop|start|exec|run|config)
    # Any other docker compose command, on this environment's stack only.
    env_compose "$name" "$cmd" "$@" ;;
  status)
    echo "$name: $dir"
    echo "  version: $(env_version "$name" || echo '-')   monitor: $(env_monitor_url "$name")"
    why="$(env_check "$name")" && echo "  check: ok" || echo "  check: $why"
    [[ -d "$dir/.git" && -f "$dir/deploy.env" && -f "$dir/.env" ]] && env_compose "$name" ps --format '  {{.Service}}: {{.Status}}' || true ;;
  *)
    echo "unknown command '$cmd'" >&2; exit 1 ;;
esac
