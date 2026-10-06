#!/usr/bin/env bash
# Captures the inputs of the stress test (cmd/stress) into a folder:
#   positions.json  the bot's book            (monitor → bot /api/positions)
#   account.json    Deribit margin figures    (monitor → bot /api/account)
#   market.json     DVOL daily closes, the perpetual's daily closes and spot
#                   (Deribit MAINNET public market data — read only, no key)
# Usage: scripts/stress_inputs.sh [btc|eth] [out_dir]
# The monitor must be running (docker compose up), on MONITOR_URL.
set -euo pipefail

bot="${1:-btc}"
out="${2:-data/stress/$bot}"
monitor="${MONITOR_URL:-http://127.0.0.1:3000}"
cur="$(echo "$bot" | tr '[:lower:]' '[:upper:]')"
api="https://www.deribit.com/api/v2/public"
now_ms=$(( $(date +%s) * 1000 ))
from_ms=$(( now_ms - 400 * 86400 * 1000 )) # ~13 months: a year of IV percentile + warm-up

mkdir -p "$out"
curl -fsS "$monitor/api/bots/$bot/positions" -o "$out/positions.json"
curl -fsS "$monitor/api/bots/$bot/account" -o "$out/account.json"
dvol=$(curl -fsS "$api/get_volatility_index_data?currency=$cur&start_timestamp=$from_ms&end_timestamp=$now_ms&resolution=1D")
closes=$(curl -fsS "$api/get_tradingview_chart_data?instrument_name=$cur-PERPETUAL&start_timestamp=$from_ms&end_timestamp=$now_ms&resolution=1D")

python3 - "$out/market.json" <<PY
import json, sys
dvol = json.loads('''$dvol''')["result"]["data"]          # [ts, open, high, low, close]
tv = json.loads('''$closes''')["result"]                  # ticks[], close[]
market = {
    "dvol": [[d[0], d[4]] for d in dvol],
    "closes": [[t, c] for t, c in zip(tv["ticks"], tv["close"])],
    "spot": tv["close"][-1],
}
json.dump(market, open(sys.argv[1], "w"))
print(f"{sys.argv[1]}: {len(market['dvol'])} DVOL days, {len(market['closes'])} closes, spot {market['spot']}")
PY
echo "inputs in $out — run: go run ./cmd/stress -dir $out -shock crash -calm 14"
