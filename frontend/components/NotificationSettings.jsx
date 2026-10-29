"use client";

import { useState } from "react";
import { KINDS, OPTIONAL_KINDS } from "@/lib/notifications";

// The bell's settings per kind (#447), on the /notifications page: each
// Updates kind can be turned off; Needs you cannot (the survey has its own
// "Don't ask me again" in its card). Saved on the server, for every device.

export default function NotificationSettings({ notifications }) {
  const { view, setSetting } = notifications;
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(null);
  if (!view) return null;

  const change = async (kind, enabled) => {
    setSaving(kind);
    setError(null);
    try {
      await setSetting(kind, enabled);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(null);
    }
  };

  return (
    <section aria-labelledby="notification-settings" className="space-y-2 rounded-lg border border-line bg-panel p-4">
      <h2 id="notification-settings" className="text-base font-semibold">
        What the bell lists
      </h2>
      <ul className="space-y-1">
        {OPTIONAL_KINDS.map((kind) => (
          <li key={kind}>
            <label className="flex min-h-11 items-center gap-3 text-sm">
              <input
                type="checkbox"
                checked={view.settings?.[kind] !== false}
                disabled={saving === kind}
                onChange={(e) => change(kind, e.target.checked)}
                className="h-5 w-5 accent-sky-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
              />
              {KINDS[kind].label}
            </label>
          </li>
        ))}
      </ul>
      <p className="text-xs text-muted">
        What needs you is always listed: a survey that is waiting (turn it off with its own &quot;Don&apos;t ask me again&quot;) and a request of yours that needs more.
        Nothing is sent by e-mail or to your phone.
      </p>
      {error ? (
        <p role="alert" className="text-xs text-warn">
          Not saved: {error}.
        </p>
      ) : null}
    </section>
  );
}
