"use client";

import clsx from "clsx";
import { useEffect, useState } from "react";
import { downloadMyData } from "@/lib/api/auth";
import { track } from "@/lib/track";

// "Download my data" (#750), from the person's menu: what the file holds, that
// it is theirs to keep safe and that it holds no secret, then one button that
// downloads it. Built like Active sessions: one dialog, Escape closes, 44 px targets.

const BUTTON = "min-h-11 rounded px-3 font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-60";

/** The dialog's words (__tests__/download-my-data.test.jsx checks them for plain words). */
export const MY_DATA_COPY = {
  title: "Download my data",
  holds:
    "You get one file with what the monitor keeps about you. It has your account, your analytics choice, your sessions, your survey answers, your usage events and your own audit lines.",
  safe: "The file is yours. Keep it somewhere safe: it shows how and when you used the monitor.",
  noSecrets: "It holds no password, no password hash, no sign-in token and no address.",
  limit: "You can download it 3 times an hour.",
  confirm: "Download the file",
  done: "Downloaded. Look for the file in your browser's downloads.",
};

/** Saves a blob as a file through a temporary link (the browser's own download). */
function saveFile(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

const DEPS = { downloadMyData, saveFile, track };

export default function DownloadMyDataModal({ onClose, deps = DEPS }) {
  const [state, setState] = useState({ phase: "idle" }); // idle | downloading | done | error
  const busy = state.phase === "downloading";

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, busy]);

  async function download() {
    setState({ phase: "downloading" });
    try {
      const { blob, filename } = await deps.downloadMyData();
      deps.saveFile(blob, filename);
      deps.track("my_data_downloaded", { outcome: "ok" });
      setState({ phase: "done" });
    } catch (err) {
      deps.track("my_data_downloaded", { outcome: err.status === 429 ? "refused" : "error" });
      setState({ phase: "error", error: err.message });
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="my-data-title">
      <div className="max-h-full w-full max-w-md space-y-3 overflow-y-auto rounded-lg border border-line bg-panel p-4 text-sm shadow-xl">
        <h2 id="my-data-title" className="text-base font-bold">
          {MY_DATA_COPY.title}
        </h2>
        <p>{MY_DATA_COPY.holds}</p>
        <p>{MY_DATA_COPY.safe}</p>
        <p>{MY_DATA_COPY.noSecrets}</p>
        <p className="text-xs text-muted">{MY_DATA_COPY.limit}</p>
        {busy ? <p role="status">Preparing your file…</p> : null}
        {state.phase === "done" ? (
          <p role="status" className="rounded bg-profit/10 px-2 py-1 text-profit">
            {MY_DATA_COPY.done}
          </p>
        ) : null}
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            Not downloaded: {state.error}
          </p>
        ) : null}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onClose} disabled={busy} className={clsx(BUTTON, "bg-slate-700 font-normal")}>
            {state.phase === "done" ? "Close window" : "Cancel"}
          </button>
          {state.phase === "done" ? null : (
            <button type="button" onClick={download} disabled={busy} className={clsx(BUTTON, "bg-action text-white")}>
              {busy ? "Downloading…" : MY_DATA_COPY.confirm}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
