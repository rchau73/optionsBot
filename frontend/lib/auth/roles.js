// The monitor's three roles (PRD §3): admin satisfies manager satisfies user.
//   user    — sees the monitor
//   manager — also closes legs and rebalances (and its dry run)
//   admin   — also manages users
const RANK = { user: 1, manager: 2, admin: 3 };

/** True when `role` is at least `need` (unknown roles satisfy nothing). */
export function roleAtLeast(role, need) {
  return (RANK[role] ?? 0) >= (RANK[need] ?? Infinity);
}
