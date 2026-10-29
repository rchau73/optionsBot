"use client";

import NotificationList from "./NotificationList";
import NotificationSettings from "./NotificationSettings";
import { useNotifications } from "@/hooks/useNotifications";

// The /notifications page (#447): the bell's list for a phone, where the
// dropdown would not fit, and the settings per kind on every screen. The
// same list and the same read marks as the bell.

export default function NotificationsPage({ bot = null }) {
  const notifications = useNotifications(bot);
  return (
    <div className="min-h-screen">
      <header className="border-b border-line bg-panel px-4 py-3">
        <h1 className="text-lg font-bold">
          Notifications{bot ? ` · ${bot.toUpperCase()}` : ""}
        </h1>
        <p className="text-sm text-muted">What needs you, and what changed. Kept 90 days, in the monitor only.</p>
      </header>
      <main className="mx-auto max-w-3xl space-y-4 p-4">
        <div className="rounded-lg border border-line bg-panel py-3">
          <NotificationList notifications={notifications} />
        </div>
        <NotificationSettings notifications={notifications} />
      </main>
    </div>
  );
}
