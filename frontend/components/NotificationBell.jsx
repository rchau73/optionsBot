"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import NotificationList from "./NotificationList";
import { useNotifications } from "@/hooks/useNotifications";
import { navigation } from "@/lib/api/http";
import { badgeText, bellLabel } from "@/lib/notifications";
import { track } from "@/lib/track";

// The notification bell (#447, prototype approved on #164): a menu-bar
// control before the person's menu, not a page. Its badge counts only what
// needs the person; updates are listed, never counted. On a desktop it opens
// a dropdown — a dialog that keeps focus and closes on Escape; on a phone it
// opens the /notifications page.

const PHONE_QUERY = "(max-width: 639px)";
const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

const isPhone = () => typeof window !== "undefined" && Boolean(window.matchMedia?.(PHONE_QUERY).matches);

/** The /notifications page, keeping the page's bot. */
export const notificationsHref = (bot) => (bot ? `/notifications?bot=${encodeURIComponent(bot)}` : "/notifications");

export default function NotificationBell({ bot = null }) {
  const notifications = useNotifications(bot);
  const count = notifications.view?.needs_you ?? 0;
  const [open, setOpen] = useState(false);
  const button = useRef(null);

  const close = useCallback(() => {
    setOpen(false);
    button.current?.focus();
  }, []);

  const onClick = () => {
    if (open) return close();
    track("notifications_opened", {});
    if (isPhone()) return navigation.assign(notificationsHref(bot));
    setOpen(true);
  };

  const badge = badgeText(count);
  return (
    <div className="relative">
      <button
        ref={button}
        type="button"
        aria-label={bellLabel(count)}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={onClick}
        className="relative inline-flex min-h-11 min-w-11 items-center justify-center rounded-full text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
      >
        <BellIcon />
        {badge ? (
          <span aria-hidden="true" className="absolute right-0.5 top-0.5 min-w-5 rounded-full bg-testnet px-1 text-center text-xs font-bold leading-5 text-slate-950">
            {badge}
          </span>
        ) : null}
      </button>
      {open ? <NotificationDialog bot={bot} notifications={notifications} onClose={close} /> : null}
    </div>
  );
}

/** The dropdown: a dialog that keeps focus inside, closes on Escape or a click outside. */
function NotificationDialog({ bot, notifications, onClose }) {
  const ref = useRef(null);

  useEffect(() => {
    ref.current?.querySelector(FOCUSABLE)?.focus();
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key !== "Tab" || !ref.current) return;
      const items = [...ref.current.querySelectorAll(FOCUSABLE)];
      if (!items.length) return;
      const [first, last] = [items[0], items[items.length - 1]];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    const onMouse = (e) => {
      // The bell itself toggles; any other click outside closes.
      if (ref.current && !ref.current.contains(e.target) && !ref.current.parentElement?.contains(e.target)) onClose();
    };
    window.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onMouse);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onMouse);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-labelledby="notifications-title"
      className="absolute right-0 z-40 mt-1 max-h-[80vh] w-96 max-w-[calc(100vw-1rem)] overflow-y-auto rounded border border-line bg-panel py-2 shadow-xl"
    >
      <div className="flex items-center gap-2 px-3 pb-1">
        <h2 id="notifications-title" className="text-base font-semibold">
          Notifications
        </h2>
        <button type="button" aria-label="Close notifications" onClick={onClose} className="ml-auto inline-flex min-h-11 min-w-11 items-center justify-center rounded-md text-lg text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet">
          ×
        </button>
      </div>
      {/* Following an item closes the dropdown (the assistant panel opens), except the public issue, a new tab. */}
      <NotificationList
        notifications={notifications}
        onFollow={(item) => {
          if (!item.href) onClose();
        }}
      />
      <div className="border-t border-line px-3 pt-2">
        <Link href={notificationsHref(bot)} onClick={onClose} className="inline-flex min-h-11 items-center text-sm text-testnet underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet">
          All notifications and settings
        </Link>
      </div>
    </div>
  );
}

function BellIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
  );
}
