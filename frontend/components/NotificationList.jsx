"use client";

import clsx from "clsx";
import { useState } from "react";
import { useAssistant } from "./AssistantProvider";
import { GROUPS } from "@/lib/notifications";
import { formatDateTime } from "@/lib/format";
import { track } from "@/lib/track";

// The bell's list (#447), in its dropdown and on the /notifications page:
// Needs you, then Updates, newest first. Unread is a dot and bold text (never
// colour alone). Following an item marks it read; "Mark all read" marks the
// whole list, on every device of the person (read marks live in auth.db).

const TARGET = "inline-flex min-h-11 items-center rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const LINK = clsx(TARGET, "border border-line text-testnet hover:border-muted");

const EMPTY = {
  needs_you: "Nothing needs you.",
  updates: "No updates in the last 90 days.",
};

export default function NotificationList({ notifications, onFollow = () => {} }) {
  const { view, error, loading, markRead, markAllRead } = notifications;
  const [actionError, setActionError] = useState(null);

  if (loading && !view) return <p className="px-3 py-2 text-sm text-muted">Loading your notifications…</p>;
  if (!view) {
    return (
      <p role="alert" className="px-3 py-2 text-sm text-warn">
        Your notifications could not be read: {error}.
      </p>
    );
  }

  const run = async (fn) => {
    setActionError(null);
    try {
      await fn();
    } catch (e) {
      setActionError(e.message);
    }
  };
  const anyUnread = view.items.some((i) => !i.read);
  const follow = (item) => {
    track("notification_followed", { kind: item.kind });
    if (!item.read) run(() => markRead([item.id]));
    onFollow(item);
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 px-3">
        <button type="button" disabled={!anyUnread} onClick={() => run(markAllRead)} className={clsx(TARGET, "ml-auto border border-line disabled:opacity-50")}>
          Mark all read
        </button>
      </div>
      {error ? (
        <p role="status" className="px-3 text-xs text-warn">
          Not refreshed: {error}. This is the last list read.
        </p>
      ) : null}
      {view.errors?.bot_events ? (
        <p role="status" className="px-3 text-xs text-warn">
          The bot&apos;s events could not be read ({view.errors.bot_events}); the rest is listed.
        </p>
      ) : null}
      {actionError ? (
        <p role="alert" className="px-3 text-xs text-warn">
          Not saved: {actionError}.
        </p>
      ) : null}
      {GROUPS.map(([group, label]) => {
        const items = view.items.filter((i) => i.group === group);
        const unread = items.filter((i) => !i.read).length;
        return (
          <section key={group} aria-labelledby={`notifications-${group}`} className="space-y-1">
            <h3 id={`notifications-${group}`} className="px-3 text-xs font-semibold uppercase tracking-wide text-muted">
              {label}
              {group === "needs_you" && unread ? ` (${unread})` : ""}
            </h3>
            {items.length ? (
              <ul className="divide-y divide-line">
                {items.map((item) => (
                  <NotificationItem key={item.id} item={item} onFollow={follow} />
                ))}
              </ul>
            ) : (
              <p className="px-3 py-1 text-sm text-muted">{EMPTY[group]}</p>
            )}
          </section>
        );
      })}
    </div>
  );
}

/** One item: dot and bold while unread, its time, its text and what it leads to. */
function NotificationItem({ item, onFollow }) {
  return (
    <li className="flex gap-2 px-3 py-2 text-sm">
      <span aria-hidden="true" className={clsx("mt-1.5 h-2 w-2 shrink-0 rounded-full", item.read ? "bg-transparent" : "bg-testnet")} />
      <div className="min-w-0 flex-1 space-y-1">
        <p className={clsx(item.read ? "font-normal text-muted" : "font-semibold text-white")}>
          {item.read ? null : <span className="sr-only">Unread: </span>}
          {item.title}
        </p>
        <p className="text-xs text-muted">{formatDateTime(item.ts)}</p>
        {item.text ? <p className="break-words">{item.text}</p> : null}
        <FollowAction item={item} onFollow={onFollow} />
      </div>
    </li>
  );
}

/**
 * What an item leads to: the survey and "needs more" open the assistant
 * panel (the question, My requests), an accepted request its public issue,
 * a bot update the assistant's Why? (the facts of /api/explain). Nothing
 * outside the assistant's provider, except the public issue link.
 */
function FollowAction({ item, onFollow }) {
  const assistant = useAssistant();
  if (item.href) {
    return (
      <a href={item.href} target="_blank" rel="noreferrer" onClick={() => onFollow(item)} className={LINK}>
        The public issue
      </a>
    );
  }
  if (!assistant) return null;
  const action = actionOf(item, assistant);
  if (!action) return null;
  return (
    <button
      type="button"
      aria-label={action.name}
      onClick={(e) => {
        const from = e.currentTarget;
        onFollow(item);
        action.run(from);
      }}
      className={LINK}
    >
      {action.label}
    </button>
  );
}

/** The assistant action an item leads to: { label, name, run(from) }, or null (a What's new note says it all). */
function actionOf(item, assistant) {
  if (item.kind === "survey") return { label: "Open the question", name: "Open the question", run: (from) => assistant.openPanel({ from }) };
  if (item.kind === "request_needs_info" || item.kind === "request_status") {
    return { label: "Open My requests", name: "Open My requests", run: (from) => assistant.openPanel({ from, tab: "requests" }) };
  }
  if (item.kind === "bot_event" && item.why) return { label: "Why?", name: `Why? ${item.title}`, run: (from) => assistant.askWhy(item.why, from) };
  return null;
}
