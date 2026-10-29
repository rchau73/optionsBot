#!/usr/bin/env bash
# promote.sh — move the stable environment to a release. A person runs it.
#
#   scripts/promote.sh v0.3.0            promote; a new tag goes on the newest mvp build
#                                        that has soaked PROMOTE_SOAK_DAYS (#360)
#   scripts/promote.sh v0.3.0 --commit <sha>   a new tag on a named commit that ran in mvp
#   scripts/promote.sh v0.2.0            rollback: a tag stable already ran skips the soak
#   scripts/promote.sh v0.1.0 --first    the very first promotion (no stable history yet)
#   add --yes to skip the confirmation
#
# A new release must have run in mvp for PROMOTE_SOAK_DAYS (3) — the tagged
# commit or a later one, per <envs>/history/deploys.tsv — and no open
# issue may carry sme:not-fixed (the SME agent found a merged fix that did not
# work). Then stable checks out the tag, rebuilds, and every bot's decision loop
# must report again; if not, stable goes back to its previous release.
# Testnet only: a live stable is promoted by hand. Run it from the development
# checkout, never from an environment's clone.
set -euo pipefail
# shellcheck source=env_lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/env_lib.sh"
why="$(env_not_in_clone)" || { echo "promote: $why" >&2; exit 1; }

tag="${1:-}"; shift || true
first=0; yes=0; pick=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --first) first=1 ;;
    --yes) yes=1 ;;
    --commit) [[ -n "${2:-}" ]] || { echo "--commit needs a commit" >&2; exit 1; }; pick="$2"; shift ;;
    *) echo "unknown option $1" >&2; exit 1 ;;
  esac
  shift
done
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: promote.sh vMAJOR.MINOR.PATCH [--commit <sha>] [--first] [--yes]" >&2; exit 1; }
[[ -z "$pick" || $first -eq 0 ]] || { echo "--commit and --first do not go together" >&2; exit 1; }
SOAK_DAYS="${PROMOTE_SOAK_DAYS:-3}"
stable="$(env_dir stable)"
promotions="$ENV_HISTORY/promotions.tsv"; deploys="$ENV_HISTORY/deploys.tsv"
die() { echo "promote: $*" >&2; exit 1; }

# newest_soaked <deploys.tsv> <days> → the commit of the newest mvp `deploy`
# line at least <days> old (its time on stderr). Fails, saying when the oldest
# deploy will have soaked, when none has. A `rollback` line (deploy.sh, #204)
# re-runs an older build and is never a candidate.
newest_soaked() {
  python3 - "$1" "$2" <<'PY'
import datetime as d, sys
days = float(sys.argv[2])
now = d.datetime.now(d.timezone.utc)
deploys = []
try:
    with open(sys.argv[1]) as f:
        for line in f:
            p = line.rstrip("\n").split("\t")
            if len(p) < 3 or p[1] != "mvp" or (len(p) > 3 and p[3] == "rollback"):
                continue
            at = d.datetime.strptime(p[0], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=d.timezone.utc)
            deploys.append((at, p[2]))
except FileNotFoundError:
    pass
if not deploys:
    sys.exit("promote: no commit deployed to mvp yet: nothing has soaked")
soaked = [x for x in deploys if now - x[0] >= d.timedelta(days=days)]
if not soaked:
    at, c = min(deploys)
    sys.exit(f"promote: nothing has soaked {sys.argv[2]} days in mvp yet: the oldest deploy ({c[:7]}, "
             f"{at:%Y-%m-%d %H:%M} UTC) will have soaked at {at + d.timedelta(days=days):%Y-%m-%d %H:%M} UTC")
at, c = max(soaked)
print(f"newest mvp build soaked {sys.argv[2]} days: {c[:7]}, deployed {at:%Y-%m-%d %H:%M} UTC", file=sys.stderr)
print(c)
PY
}

why="$(env_check stable)" || die "$why"
[[ "$(env_file_var "$stable/.env" DERIBIT_ENV)" != "live" ]] || die "stable runs live: promote it by hand"
[[ -z "$(git -C "$stable" status --porcelain --untracked-files=no)" ]] || die "$stable has local changes"
git -C "$stable" fetch -q --tags origin

# ── The release ─────────────────────────────────────────────────────────────
# An existing tag, or a new one created only once every gate has passed and the
# person has confirmed: on origin/main for --first, on the --commit named (it
# must itself have run in mvp), else on the newest mvp deploy at least SOAK_DAYS
# old. mvp runs main, so every later deploy contains that build and the soak
# gate below passes by construction; it stays the check. Not mvp.deployed: mvp
# redeploys after every merge, so its current build never has 3 days (#360).
new_tag=0
if git -C "$stable" rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  [[ -z "$pick" ]] || die "$tag already exists: --commit is only for a new tag"
  commit="$(git -C "$stable" rev-parse "$tag^{commit}")"
else
  new_tag=1
  if [[ $first -eq 1 ]]; then
    commit="$(git -C "$stable" rev-parse origin/main)"
  elif [[ -n "$pick" ]]; then
    commit="$(git -C "$stable" rev-parse -q --verify "$pick^{commit}")" || die "unknown commit $pick"
    awk -F'\t' -v c="$commit" '$2 == "mvp" && $3 == c { f = 1 } END { exit !f }' "$deploys" 2>/dev/null ||
      die "${commit:0:7} never ran in mvp (no mvp line in $deploys)"
  else
    commit="$(newest_soaked "$deploys" "$SOAK_DAYS")" || exit 1
  fi
fi
prev=""
[[ -s "$promotions" ]] && prev="$(tail -1 "$promotions" | cut -f2)"

# ── Gates ───────────────────────────────────────────────────────────────────
if [[ $first -eq 1 ]]; then
  [[ ! -s "$promotions" ]] || die "--first is only for the very first promotion (last: $prev)"
  echo "first promotion: no soak check"
elif [[ -s "$promotions" ]] && cut -f2 "$promotions" | grep -qx "$tag"; then
  echo "$tag ran in stable before: rollback, no soak check"
else
  # Earliest mvp deploy of this commit or a later one. A `rollback` line
  # (deploy.sh, #204) names a commit that had already run, so it never moves
  # the clock earlier.
  since=""
  while IFS=$'\t' read -r at env c _; do
    [[ "$env" == "mvp" ]] || continue
    if git -C "$stable" merge-base --is-ancestor "$commit" "$c" 2>/dev/null; then since="$at"; break; fi
  done < <(cat "$deploys" 2>/dev/null || true)
  [[ -n "$since" ]] || die "$tag (${commit:0:7}) never ran in mvp"
  python3 - "$since" "$SOAK_DAYS" <<'PY' || exit 1
import datetime as d, sys
since = d.datetime.strptime(sys.argv[1], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=d.timezone.utc)
ready = since + d.timedelta(days=float(sys.argv[2]))
now = d.datetime.now(d.timezone.utc)
if now < ready:
    print(f"promote: in mvp since {since:%Y-%m-%d %H:%M} UTC; the {sys.argv[2]}-day soak ends {ready:%Y-%m-%d %H:%M} UTC", file=sys.stderr)
    sys.exit(1)
print(f"soak ok: in mvp since {since:%Y-%m-%d %H:%M} UTC ({(now - since).total_seconds() / 86400:.1f} days)")
PY
  slug="$(git -C "$stable" remote get-url origin | sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')"
  open_nf="$(gh issue list -R "$slug" --label sme:not-fixed --state open --json number,title -q '.[] | "#\(.number) \(.title)"')" ||
    die "could not read the sme:not-fixed issues from GitHub ($slug): not promoting blind"
  [[ -z "$open_nf" ]] || die "open issues the SME agent found not fixed:
$open_nf"
fi

echo "stable: ${prev:-none} → $tag (${commit:0:7}$([[ $new_tag -eq 1 ]] && echo ", new tag"))"
# `|| true`: head closes the pipe after 20 lines, git log then dies of SIGPIPE
# (141) and pipefail + set -e ended the script silently before the prompt
# (2026-10-26, v0.1.0 → v0.2.0 is 99 merges).
{ git -C "$stable" log --oneline "${prev:-$commit~1}..$commit" 2>/dev/null || true; } | head -20 | sed 's/^/  /'
n="$(git -C "$stable" rev-list --count "${prev:-$commit~1}..$commit" 2>/dev/null || echo 0)"
[[ "$n" -le 20 ]] || echo "  … and $((n - 20)) more"
if [[ $yes -ne 1 ]]; then
  read -r -p "Promote stable to $tag? [y/N] " ok
  [[ "$ok" == "y" || "$ok" == "Y" ]] || die "cancelled"
fi
if [[ $new_tag -eq 1 ]]; then
  git -C "$stable" tag -a "$tag" "$commit" -m "release $tag"
  git -C "$stable" push -q origin "$tag"
  echo "tagged $tag at ${commit:0:7} and pushed it"
fi

# ── Promote, with rollback on a failed health check ─────────────────────────
url="$(env_monitor_url stable)"
run() { git -C "$stable" checkout -q --detach "$1" && env_compose stable up -d --build && wait_healthy "$url" "${PROMOTE_HEALTH_TIMEOUT_SEC:-300}" "$(env_monitor_token stable)"; }
# Each outcome is also a `stable` line in deploys.tsv (#470: cmd/dora's
# releases and failures): `release <tag>` when healthy, `rollback <prev>` when
# a failed promotion went back. Readers of the mvp soak skip these lines.
if run "$tag"; then
  env_record promotions.tsv "$tag" "$commit"
  env_record deploys.tsv stable "$commit" release "$tag"
  echo "stable is on $tag and healthy: $url"
  exit 0
fi
echo "promote: $tag failed its health check" >&2
if [[ -n "$prev" ]] && run "$prev"; then
  env_record deploys.tsv stable "$(git -C "$stable" rev-parse "$prev^{commit}")" rollback "$prev"
  die "rolled back to $prev (healthy). See: scripts/envctl.sh stable logs"
fi
die "rollback failed too: check scripts/envctl.sh stable logs"
