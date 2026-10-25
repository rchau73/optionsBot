"use client";

import { useEffect, useState } from "react";
import AddUserForm from "./AddUserForm";
import Badge from "./Badge";
import Panel from "./Panel";
import ResetLinkDialog from "./ResetLinkDialog";
import UserActionDialog from "./UserActionDialog";
import { fetchUsers } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { ROLES } from "@/lib/users";

const BUTTON =
  "rounded border border-line px-2 py-0.5 text-xs hover:bg-line/40 disabled:opacity-40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/**
 * The Users page (#306, admins only — the /api/admin/users routes enforce it):
 * every person with role, email, created and last sign-in; add a person;
 * change a role, reset a password, make a reset link (#307), sign out
 * everywhere and delete, each after a confirmation. `me` is the signed-in
 * admin's username.
 */
export default function UsersAdmin({ me }) {
  const [users, setUsers] = useState(null);
  const [error, setError] = useState(null);
  const [reload, setReload] = useState(0);
  const [roles, setRoles] = useState({}); // user id → role picked in its row, not yet sent
  const [pending, setPending] = useState(null); // { action, user, role } awaiting confirmation
  const [notice, setNotice] = useState(null); // { tone, text } of the last action
  const [linkFor, setLinkFor] = useState(null); // the user a reset link is being made for

  useEffect(() => {
    const ctrl = new AbortController();
    fetchUsers(ctrl.signal).then(
      (list) => {
        setUsers(list);
        setError(null);
      },
      (err) => {
        if (!ctrl.signal.aborted) setError(err.message);
      },
    );
    return () => ctrl.abort();
  }, [reload]);

  const done = (result) => {
    setPending(null);
    setLinkFor(null);
    setRoles({});
    setNotice(result);
    setReload((n) => n + 1);
  };

  return (
    <div className="space-y-4">
      {notice ? (
        <p role="status" className={notice.tone === "good" ? "rounded bg-profit/10 px-3 py-2 text-profit" : "rounded bg-warn/15 px-3 py-2 text-warn"}>
          {notice.text}
        </p>
      ) : null}

      <Panel title="Add user">
        <AddUserForm onCreated={(u) => done({ tone: "good", text: `${u.username} (${u.role}) is added and can sign in now.` })} />
      </Panel>

      <Panel title="Users" right={users ? <span className="text-xs text-muted">{users.length}</span> : null}>
        {error ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            Users could not be loaded: {error}
          </p>
        ) : null}
        {!users && !error ? <p className="text-muted">Loading users…</p> : null}
        {users ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-xs uppercase text-muted">
                <tr>
                  <th className="px-2 py-1">Username</th>
                  <th className="px-2 py-1">Role</th>
                  <th className="px-2 py-1">Email</th>
                  <th className="px-2 py-1">Created</th>
                  <th className="px-2 py-1">Last sign-in</th>
                  <th className="px-2 py-1">Actions</th>
                </tr>
              </thead>
              <tbody>
                {users.map((u) => {
                  const self = u.username === me;
                  const picked = roles[u.id] ?? u.role;
                  return (
                    <tr key={u.id} className="border-t border-line" data-testid={`user-${u.username}`}>
                      <td className="px-2 py-1 font-semibold">
                        {u.username} {self ? <Badge tone="muted">you</Badge> : null}
                      </td>
                      <td className="px-2 py-1">
                        <div className="flex items-center gap-2">
                          <select
                            aria-label={`Role of ${u.username}`}
                            value={picked}
                            onChange={(e) => setRoles({ ...roles, [u.id]: e.target.value })}
                            className="rounded border border-line bg-panel px-1 py-0.5"
                          >
                            {ROLES.map((r) => (
                              <option key={r} value={r}>
                                {r}
                              </option>
                            ))}
                          </select>
                          {picked !== u.role ? (
                            <button type="button" className={BUTTON} onClick={() => setPending({ action: "role", user: u, role: picked })}>
                              Change role
                            </button>
                          ) : null}
                        </div>
                      </td>
                      <td className="px-2 py-1 text-muted">{u.email ?? "—"}</td>
                      <td className="px-2 py-1 tabular-nums text-muted">{formatDateTime(u.created_at)}</td>
                      <td className="px-2 py-1 tabular-nums text-muted">{u.last_sign_in_at ? formatDateTime(u.last_sign_in_at) : "never"}</td>
                      <td className="px-2 py-1">
                        <div className="flex flex-wrap gap-1">
                          <button
                            type="button"
                            className={BUTTON}
                            disabled={self}
                            title={self ? "your own password: Account ▾ → Change password" : undefined}
                            onClick={() => setPending({ action: "password", user: u })}
                          >
                            Reset password
                          </button>
                          <button
                            type="button"
                            className={BUTTON}
                            disabled={self}
                            title={self ? "your own password: Account ▾ → Change password" : undefined}
                            onClick={() => setLinkFor(u)}
                          >
                            Reset link
                          </button>
                          <button type="button" className={BUTTON} onClick={() => setPending({ action: "signout", user: u })}>
                            Sign out everywhere
                          </button>
                          <button
                            type="button"
                            className={BUTTON}
                            disabled={self}
                            title={self ? "you cannot delete yourself" : undefined}
                            onClick={() => setPending({ action: "delete", user: u })}
                          >
                            Delete
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
      </Panel>

      {pending ? <UserActionDialog {...pending} me={me} onDone={done} onCancel={() => setPending(null)} /> : null}
      {linkFor ? <ResetLinkDialog user={linkFor} me={me} onDone={done} onCancel={() => setLinkFor(null)} /> : null}
    </div>
  );
}
