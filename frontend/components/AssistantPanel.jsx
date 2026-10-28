"use client";

import clsx from "clsx";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import MyRequests from "./MyRequests";
import RequestChecklist from "./RequestChecklist";
import RequestDraftCard from "./RequestDraftCard";
import {
  EXAMPLES,
  MAX_QUESTION,
  NOTICE,
  OFF_TEXT,
  REFUSAL_FOLLOW_UPS,
  citationHref,
  citationLabel,
  footLine,
  remainingText,
} from "@/lib/assistantPanel";

// The assistant panel (#408, E10 prototype): a 400 px side panel on desktop
// (a complementary region), a full-screen sheet on a phone (a dialog with a
// focus trap). Esc or × closes it; the provider returns focus to the opener.

const BUTTON = "min-h-11 rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const PHONE_QUERY = "(max-width: 639px)";

/** True under 640 px (Tailwind's sm), false where the browser cannot tell. */
function usePhone() {
  const [phone, setPhone] = useState(() => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(PHONE_QUERY).matches);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return undefined;
    const mq = window.matchMedia(PHONE_QUERY);
    const on = () => setPhone(mq.matches);
    mq.addEventListener?.("change", on);
    return () => mq.removeEventListener?.("change", on);
  }, []);
  return phone;
}

const TABS = [
  ["ask", "Ask"],
  ["requests", "My requests"],
];

const FOCUSABLE ='a[href], button:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export default function AssistantPanel({ assistant }) {
  const { bot, messages, enabled, remaining, close, ask } = assistant;
  const phone = usePhone();
  const ref = useRef(null);
  const input = useRef(null);
  const [draft, setDraft] = useState("");
  const [tab, setTab] = useState("ask");
  const thinking = messages.some((m) => m.role === "thinking");

  useEffect(() => {
    input.current?.focus();
  }, []);

  // Esc closes on every screen; Tab stays inside the sheet on a phone only.
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        close();
        return;
      }
      if (e.key !== "Tab" || !phone || !ref.current) return;
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
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [close, phone]);

  const send = (text = draft) => {
    if (!text.trim() || thinking || enabled === false) return;
    ask(text, { bot, entry: "menu" });
    setDraft("");
  };

  return (
    <aside
      ref={ref}
      role={phone ? "dialog" : "complementary"}
      aria-modal={phone ? "true" : undefined}
      aria-label="Assistant"
      className="fixed inset-0 z-50 flex flex-col border-line bg-panel shadow-2xl sm:inset-y-0 sm:left-auto sm:right-0 sm:w-[400px] sm:border-l"
    >
      <header className="flex items-center gap-2 border-b border-line px-3 py-2">
        <h2 className="text-base font-bold">Assistant</h2>
        <span className="rounded-full bg-slate-800 px-2 py-0.5 text-xs text-muted">AI · answers from the docs and your journal</span>
        <button type="button" onClick={close} aria-label="Close the assistant" className={clsx(BUTTON, "ml-auto min-w-11 text-lg text-muted hover:text-white")}>
          ×
        </button>
      </header>
      <div role="tablist" aria-label="Assistant" className="flex gap-1 border-b border-line px-3">
        {TABS.map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            onClick={() => setTab(key)}
            className={clsx(BUTTON, tab === key ? "border-b-2 border-testnet font-semibold" : "text-muted hover:text-white")}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === "requests" ? (
        <div role="tabpanel" aria-label="My requests" className="flex-1 overflow-y-auto px-3 py-3">
          <MyRequests />
        </div>
      ) : (
        <div role="log" aria-live="polite" aria-label="Conversation" className="flex-1 space-y-3 overflow-y-auto px-3 py-3">
          {enabled === false && !messages.length ? <OffNotice /> : null}
          {!messages.length && enabled !== false ? <Examples onAsk={send} /> : null}
          {messages.map((m) => (
            <Message key={m.id} message={m} assistant={assistant} onShowRequests={() => setTab("requests")} />
          ))}
        </div>
      )}

      {tab === "requests" ? null : <form
        className="space-y-1 border-t border-line px-3 py-2"
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <div className="flex items-end gap-2">
          <label htmlFor="assistant-question" className="sr-only">
            Your question
          </label>
          <textarea
            id="assistant-question"
            ref={input}
            value={draft}
            maxLength={MAX_QUESTION}
            rows={2}
            disabled={enabled === false}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
            placeholder="Ask about a page, the strategy or a trade"
            className="min-h-11 flex-1 resize-none rounded-md border border-line bg-slate-900 px-2 py-1 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
          />
          <button type="submit" disabled={!draft.trim() || thinking || enabled === false} className={clsx(BUTTON, "bg-slate-700 font-semibold text-white disabled:opacity-50")}>
            Send
          </button>
        </div>
        <p className="text-xs text-muted">{NOTICE}</p>
        {remaining != null ? <p className="text-xs text-muted">{remainingText(remaining)}</p> : null}
      </form>}
    </aside>
  );
}

function OffNotice() {
  return (
    <p role="status" className="text-sm text-warn">
      {OFF_TEXT}
    </p>
  );
}

function Examples({ onAsk }) {
  return (
    <div className="space-y-2">
      <p className="text-sm text-muted">Ask about a page, the strategy or a trade. For example:</p>
      {EXAMPLES.map((q) => (
        <button key={q} type="button" onClick={() => onAsk(q)} className={clsx(BUTTON, "block w-full border border-line text-left hover:border-muted")}>
          {q}
        </button>
      ))}
    </div>
  );
}

function Message({ message: m, assistant, onShowRequests }) {
  if (m.role === "user") {
    return (
      <div className="flex justify-end">
        <p className="max-w-[85%] whitespace-pre-line rounded-lg bg-slate-700 px-3 py-2 text-sm text-white">{m.text}</p>
      </div>
    );
  }
  if (m.role === "thinking") {
    return (
      <div aria-busy="true" data-testid="assistant-thinking" className="max-w-[85%] space-y-2 rounded-lg bg-slate-800 px-3 py-3">
        <span className="sr-only">The assistant is thinking…</span>
        <div className="h-3 w-3/4 animate-pulse rounded bg-slate-700" />
        <div className="h-3 w-1/2 animate-pulse rounded bg-slate-700" />
      </div>
    );
  }
  if (m.role === "notice") {
    return m.kind === "off" ? (
      <OffNotice />
    ) : (
      <p role="status" className="text-sm text-warn">
        {m.text}
      </p>
    );
  }
  if (m.role === "error") {
    return (
      <div role="alert" className="space-y-1 text-sm text-warn">
        <p>The assistant did not answer: {m.text}.</p>
        <button type="button" onClick={() => assistant.retry(m)} className={clsx(BUTTON, "border border-line text-white")}>
          Retry
        </button>
      </div>
    );
  }
  return <Answer message={m} assistant={assistant} onShowRequests={onShowRequests} />;
}

function Answer({ message: m, assistant, onShowRequests }) {
  const [liked, setLiked] = useState(false);
  const [flagError, setFlagError] = useState(null);
  const bot = m.bot ?? assistant.bot;
  const q = bot ? `?bot=${encodeURIComponent(bot)}` : "";
  const dontKnow = m.outcome === "dont_know" || m.outcome === "no_citation";

  const onFlag = async () => {
    setFlagError(null);
    try {
      await assistant.flag(m);
    } catch (e) {
      setFlagError(e.message);
    }
  };

  return (
    <div className="max-w-[92%] space-y-2 rounded-lg bg-slate-800 px-3 py-2 text-sm" data-testid="assistant-answer">
      <p className="whitespace-pre-line">{m.text}</p>
      <RequestChecklist checklist={m.checklist} />
      {m.draft ? <RequestDraftCard message={m} assistant={assistant} onShowRequests={onShowRequests} /> : null}
      {dontKnow ? (
        <p>
          <Link href={`/how-it-works${q}`} className="text-testnet underline">
            How it works
          </Link>{" "}
          explains the strategy step by step.
        </p>
      ) : null}
      {m.outcome === "refused" ? (
        <div className="flex flex-wrap gap-2">
          {REFUSAL_FOLLOW_UPS.map((f) =>
            f.kind === "ask" ? (
              <button key={f.label} type="button" onClick={() => assistant.ask(f.label, { bot, entry: "menu" })} className={clsx(BUTTON, "border border-line")}>
                {f.label}
              </button>
            ) : (
              <Link key={f.label} href={`${f.path}${q}`} onClick={assistant.close} className={clsx(BUTTON, "inline-flex items-center border border-line")}>
                {f.label}
              </Link>
            ),
          )}
        </div>
      ) : null}
      {m.citations?.length ? (
        <ul aria-label="Sources" className="flex flex-wrap gap-1">
          {m.citations.map((c) => (
            <li key={c.id}>
              <SourceChip citation={c} bot={bot} />
            </li>
          ))}
        </ul>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" aria-label="Helpful" aria-pressed={liked} onClick={() => setLiked(!liked)} className={clsx(BUTTON, "min-w-11 border border-line", liked && "bg-slate-700")}>
          👍
        </button>
        <button type="button" onClick={onFlag} disabled={m.flagged} className={clsx(BUTTON, "border border-line disabled:opacity-60")}>
          {m.flagged ? "Flagged" : "Flag"}
        </button>
        {flagError ? <span className="text-xs text-warn">Not flagged: {flagError}</span> : null}
      </div>
      <p className="text-xs text-muted">{footLine(m)}</p>
    </div>
  );
}

function SourceChip({ citation, bot }) {
  const link = citationHref(citation, bot);
  const label = citationLabel(citation);
  const chip = "inline-flex min-h-11 items-center rounded-full border border-line px-3 text-xs";
  if (!link) return <span className={clsx(chip, "text-muted")}>{label}</span>;
  if (link.external) {
    return (
      <a href={link.href} target="_blank" rel="noreferrer" className={clsx(chip, "text-testnet hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet")}>
        {label}
      </a>
    );
  }
  return (
    <Link href={link.href} className={clsx(chip, "text-testnet hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet")}>
      {label}
    </Link>
  );
}
