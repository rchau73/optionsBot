"use client";

import clsx from "clsx";
import { Fragment, useMemo, useState } from "react";
import PrTimeline from "./PrTimeline";
import { ROW_FILTERS, filterRows, prRows, sortRows } from "@/lib/analytics/deliveryView";

const TARGET =
  "inline-flex min-h-11 items-center justify-center rounded border border-line px-3 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** The result chip's tone: the words are always shown, colour only adds to them. */
const RESULT_TONE = {
  "ROLLED BACK": "bg-loss/15 text-loss",
  "ON STABLE": "bg-profit/15 text-profit",
  "LIVE ON MVP": "bg-testnet/20 text-testnet",
  "NOT LIVE": "bg-slate-800 text-muted",
};

/** A sortable column's header button: the arrow shows the order, aria-sort on the <th> says it. */
function SortHead({ k, sort, onSort, children }) {
  return (
    <button type="button" onClick={() => onSort(k)} className="inline-flex min-h-11 items-center gap-1 font-semibold underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
    >
      {children}
      <span aria-hidden="true">{sort.key === k ? (sort.dir === "asc" ? "▲" : "▼") : "↕"}</span>
    </button>
  );
}

/**
 * What went live (#473): every merged PR, newest first, with a filter (All,
 * On mvp only, On stable), a search (PR, epic or date) and sortable columns
 * under a sticky header. Opening a row shows where its time went; the open
 * PR is also placed on the release train (openPr, onOpen).
 */
export default function DeliveryTable({ report, openPr, onOpen }) {
  const [filter, setFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState({ key: "merged", dir: "desc" });
  const all = useMemo(() => prRows(report), [report]);
  const rows = sortRows(filterRows(all, filter, query), sort.key, sort.dir);

  const ariaSort = (key) => (sort.key === key ? (sort.dir === "asc" ? "ascending" : "descending") : undefined);
  const head = (k) => ({ k, sort, onSort: (key) => setSort((s) => ({ key, dir: s.key === key && s.dir === "desc" ? "asc" : "desc" })) });

  return (
    <section aria-labelledby="dora-prs" className="rounded border border-line bg-panel p-3">
      <h2 id="dora-prs" className="mb-2 font-semibold">
        What went live
      </h2>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <div role="group" aria-label="Show" className="flex flex-wrap gap-1">
          {ROW_FILTERS.map((f) => (
            <button
              key={f.key}
              type="button"
              aria-pressed={filter === f.key}
              onClick={() => setFilter(f.key)}
              className={clsx(TARGET, filter === f.key ? "bg-slate-700 font-semibold" : "hover:bg-line/40")}
            >
              {f.label}
            </button>
          ))}
        </div>
        <label className="flex flex-wrap items-center gap-2 text-sm">
          Search by PR, epic or date
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="#458, E6, 29 Oct"
            className="min-h-11 rounded border border-line bg-surface px-2"
          />
        </label>
        <span className="text-sm text-muted" role="status">
          {rows.length} of {all.length} PRs
        </span>
      </div>
      <div className="max-h-[32rem] overflow-auto">
        <table className="w-full text-left text-sm">
          <thead className="sticky top-0 z-10 bg-panel">
            <tr className="border-b border-line">
              <th aria-sort={ariaSort("number")}>
                <SortHead {...head("number")}>PR</SortHead>
              </th>
              <th>Title</th>
              <th>Epic</th>
              <th aria-sort={ariaSort("merged")}>
                <SortHead {...head("merged")}>Merged</SortHead>
              </th>
              <th aria-sort={ariaSort("mvp")}>
                <SortHead {...head("mvp")}>Live on mvp</SortHead>
              </th>
              <th aria-sort={ariaSort("stable")}>
                <SortHead {...head("stable")}>Live on stable</SortHead>
              </th>
              <th>Delivery result</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={7} className="py-3 text-muted">
                  {all.length === 0 ? "No PR merged since recording began." : "No PR matches this filter and search."}
                </td>
              </tr>
            ) : null}
            {rows.map((r) => {
              const open = openPr === r.number;
              return (
                <Fragment key={r.number}>
                  <tr className="border-b border-line/60 align-top">
                    <td>
                      <button
                        type="button"
                        aria-expanded={open}
                        aria-controls={`pr-${r.number}`}
                        onClick={() => onOpen(r.number)}
                        className="inline-flex min-h-11 min-w-11 items-center gap-1 font-semibold underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
                      >
                        <span aria-hidden="true">{open ? "▾" : "▸"}</span>#{r.number}
                      </button>
                    </td>
                    <td className="py-2 pr-2">{r.title}</td>
                    <td className="py-2 pr-2">{r.epic || "—"}</td>
                    <td className="py-2 pr-2 tabular-nums whitespace-nowrap">{r.mergedText}</td>
                    <td className="py-2 pr-2 tabular-nums">{r.mvpText}</td>
                    <td className="py-2 pr-2 tabular-nums">{r.stableText}</td>
                    <td className="py-2">
                      <span className={clsx("rounded px-1.5 py-0.5 text-xs font-semibold whitespace-nowrap", RESULT_TONE[r.result])}>{r.result}</span>
                    </td>
                  </tr>
                  {open ? (
                    <tr id={`pr-${r.number}`}>
                      <td colSpan={7} className="pb-3">
                        <PrTimeline pr={r.pr} report={report} />
                      </td>
                    </tr>
                  ) : null}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}
