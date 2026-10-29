import Badge from "./Badge";
import Panel from "./Panel";
import {
  FIRST_ANSWERS_TARGET_MS,
  NPS_MIN_ANSWERS,
  attentionItems,
  countRows,
  minutesText,
  neverOpenedScreens,
  pctText,
  total,
} from "@/lib/analytics/usageView";
import { formatDateTime } from "@/lib/format";

// The admin Analytics page's body (#445): "Needs your attention", the eight
// cards of the event plan's questions (Q1–Q8) and the data behind them, all
// read from one weekly usage report (internal/usage/report.go). Counts of
// people and actions only: the report carries no id, name, amount or text.

const TAG_TONE = { DROP: "bad", FLAG: "warn", NEVER: "muted", OK: "good" };

/** A text tag: colour is never the only signal. */
export function Tag({ tag }) {
  return <Badge tone={TAG_TONE[tag] ?? "neutral"}>{tag}</Badge>;
}

/** A small two-column table of a count map, most first; "none" when empty. */
function Counts({ caption, rows, head = "Count" }) {
  if (!rows.length) return <p className="text-sm text-muted">{caption}: none</p>;
  return (
    <table className="w-full text-sm">
      <caption className="text-left text-xs uppercase tracking-wide text-muted">{caption}</caption>
      <thead className="sr-only">
        <tr>
          <th scope="col">Value</th>
          <th scope="col">{head}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map(([k, v]) => (
          <tr key={k} className="border-t border-line/60">
            <td className="py-0.5">{k}</td>
            <td className="py-0.5 text-right tabular-nums">{v}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** One figure with its label. */
function Figure({ label, value }) {
  return (
    <div>
      <div className="text-xs text-muted">{label}</div>
      <div className="text-lg font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function Card({ q, title, children }) {
  return (
    <Panel title={`${q} · ${title}`}>
      <div className="space-y-3">{children}</div>
    </Panel>
  );
}

/** "Needs your attention": every item tagged, or one "Nothing dropped" line. */
export function Attention({ report }) {
  const items = attentionItems(report);
  return (
    <Panel title="Needs your attention">
      {items.length ? (
        <ul className="space-y-1 text-sm">
          {items.map((i) => (
            <li key={`${i.tag}:${i.text}`} className="flex items-start gap-2">
              <Tag tag={i.tag} />
              <span>{i.text}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="flex items-center gap-2 text-sm">
          <Tag tag="OK" />
          Nothing dropped, nothing flagged, every screen and panel opened.
        </p>
      )}
    </Panel>
  );
}

function Active({ a }) {
  const max = Math.max(1, ...a.days.map((d) => d.users));
  const summary = `Active people per day: ${a.days.map((d) => `${d.day} ${d.users}`).join(", ")}`;
  return (
    <Card q="Q1" title="Who is active">
      <div role="img" aria-label={summary} className="flex h-20 items-end gap-1">
        {a.days.map((d) => (
          <div key={d.day} className="flex flex-1 flex-col items-center justify-end gap-0.5">
            <span className="text-[10px] tabular-nums text-muted">{d.users}</span>
            <div className="w-full rounded-t bg-testnet/70" style={{ height: `${(60 * d.users) / max}px` }} />
            <span className="text-[10px] text-muted">{d.day.slice(5)}</span>
          </div>
        ))}
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Figure label="This week" value={a.week.users} />
        <Figure label="Last 30 days" value={a.month.users} />
      </div>
      <Counts caption="This week by role" rows={countRows(a.week.by_role)} head="People" />
    </Card>
  );
}

function Cohorts({ cohorts }) {
  const offsets = [1, 2, 3, 4];
  return (
    <Card q="Q2" title="Do they come back">
      <table className="w-full text-sm">
        <caption className="sr-only">Share of each first-week cohort back in each later week; — for a week not ended</caption>
        <thead>
          <tr className="text-xs text-muted">
            <th scope="col" className="text-left font-normal">
              First week
            </th>
            <th scope="col" className="text-right font-normal">
              People
            </th>
            {offsets.map((o) => (
              <th key={o} scope="col" className="text-right font-normal">
                +{o}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {cohorts.map((c) => (
            <tr key={c.week} className="border-t border-line/60">
              <th scope="row" className="py-0.5 text-left font-normal">
                {c.week}
              </th>
              <td className="text-right tabular-nums">{c.users}</td>
              {offsets.map((o) => {
                const r = (c.retained ?? []).find((x) => x.offset === o);
                const pct = r?.pct;
                return (
                  <td
                    key={o}
                    className="text-right tabular-nums"
                    style={typeof pct === "number" ? { backgroundColor: `rgb(56 189 248 / ${(0.05 + (0.45 * pct) / 100).toFixed(2)})` } : undefined}
                  >
                    {r ? `${pctText(pct)} (${r.users})` : "—"}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}

function Panels({ p, report }) {
  const never = neverOpenedScreens(report);
  return (
    <Card q="Q3" title="What is opened">
      <Counts caption="Views per screen" rows={countRows(p.screens).filter(([k]) => k !== "login")} head="Views" />
      {never.length ? (
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <Tag tag="NEVER" /> screens: {never.join(", ")}
        </p>
      ) : null}
      <Counts caption="Panels expanded" rows={countRows(p.opens)} head="Opens" />
      {p.never_opened?.length ? (
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <Tag tag="NEVER" /> panels: {p.never_opened.join(", ")}
        </p>
      ) : null}
    </Card>
  );
}

function FirstAnswers({ f }) {
  const median = f.median_elapsed_ms;
  const within = typeof median === "number" ? median <= FIRST_ANSWERS_TARGET_MS : null;
  return (
    <Card q="Q4" title="First answers">
      <div className="grid grid-cols-2 gap-3">
        <Figure label="Found in the first session" value={`${f.with_answers} of ${f.first_sessions}`} />
        <Figure label="Share" value={pctText(f.pct)} />
      </div>
      <p className="flex flex-wrap items-center gap-2 text-sm">
        Median time {minutesText(median)} against the {FIRST_ANSWERS_TARGET_MS / 60000}-minute target
        {within === null ? null : <Tag tag={within ? "OK" : "DROP"} />}
        {within === false ? <span>over the target</span> : null}
      </p>
    </Card>
  );
}

function Help({ h, a }) {
  return (
    <Card q="Q5" title="Help and the assistant">
      <div className="grid grid-cols-3 gap-3">
        <Figure label="Tips opened" value={total(h?.tips)} />
        <Figure label="Videos played" value={total(h?.videos_played)} />
        <Figure label="Helped" value={h ? `${pctText(h.helped_pct)} (${h.helped} of ${h.answers})` : "—"} />
      </div>
      {a ? (
        <>
          <Counts caption="Assistant replies by kind" rows={countRows(a.answers)} head="Replies" />
          <p className="flex items-center gap-2 text-sm">
            {a.flagged > 0 ? <Tag tag="FLAG" /> : null}
            Flagged answers: {a.flagged}
          </p>
        </>
      ) : (
        <p className="text-sm text-muted">The assistant is not in this report.</p>
      )}
    </Card>
  );
}

function Funnels({ mc, rb }) {
  return (
    <Card q="Q6" title="Manual close and rebalance">
      <table className="w-full text-sm">
        <caption className="text-left text-xs uppercase tracking-wide text-muted">Manual close</caption>
        <thead className="sr-only">
          <tr>
            <th scope="col">Step</th>
            <th scope="col">Count</th>
          </tr>
        </thead>
        <tbody>
          {[
            ["Dialog opened", mc.opened],
            ["… allowed", mc.opened_allowed],
            ["Cancelled", mc.cancelled],
            ["Confirmed", mc.confirmed],
            ["… last leg", mc.confirmed_last_leg],
          ].map(([k, v]) => (
            <tr key={k} className="border-t border-line/60">
              <td className="py-0.5">{k}</td>
              <td className="text-right tabular-nums">{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <Counts caption="Manual-close results" rows={countRows(mc.results)} />
      <Counts caption="Rebalance previewed" rows={countRows(rb.previewed)} />
      <Counts caption="Rebalance confirmed" rows={countRows(rb.confirmed)} />
    </Card>
  );
}

function Trust({ t }) {
  return (
    <Card q="Q7" title="Trust">
      <Counts caption="Stale data shown, per source" rows={countRows(t.stale)} />
      <Counts caption="Signed out, per reason" rows={countRows(t.signed_out)} />
    </Card>
  );
}

function Survey({ s }) {
  if (!s) {
    return (
      <Card q="Q8" title="Why they stay or leave">
        <p className="text-sm text-muted">No survey answers yet.</p>
      </Card>
    );
  }
  const n = s.nps;
  const absent = (m) => (m === undefined ? <p className="text-sm text-muted">Not in this report (written before #445).</p> : null);
  return (
    <Card q="Q8" title="Why they stay or leave">
      <div className="grid grid-cols-2 gap-3">
        <Figure
          label="NPS"
          value={typeof n.score === "number" ? Math.round(n.score) : `Not scored: ${n.answers} of ${NPS_MIN_ANSWERS} answers`}
        />
        <Figure label="Promoters · passives · detractors" value={`${n.promoters} · ${n.passives} · ${n.detractors}`} />
      </div>
      {absent(s.why_best) ?? <Counts caption="What we do best (9–10)" rows={countRows(s.why_best)} head="Answers" />}
      {absent(s.why_improve) ?? <Counts caption="What to improve first (0–8)" rows={countRows(s.why_improve)} head="Answers" />}
      <Counts caption="Exit reasons" rows={countRows(s.exit_reasons)} head="Answers" />
    </Card>
  );
}

/** The eight cards, two columns on a desktop and one on a phone. */
export function QuestionCards({ report }) {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <Active a={report.active} />
      <Cohorts cohorts={report.cohorts ?? []} />
      <Panels p={report.panels} report={report} />
      <FirstAnswers f={report.first_answers} />
      <Help h={report.help} a={report.assistant} />
      <Funnels mc={report.manual_close} rb={report.rebalance} />
      <Trust t={report.trust} />
      <Survey s={report.survey} />
    </div>
  );
}

/** "Data behind this page": the report file and time, accepted and rejected events, consent counts, the week not reported yet. */
export function DataBehind({ data }) {
  const r = data.report;
  const src = r?.source;
  const c = data.consent;
  return (
    <Panel title="Data behind this page">
      <ul className="space-y-1 text-sm">
        {data.file ? (
          <li>
            Report <code>{data.file}</code>, written {formatDateTime(data.written_at)} from <code>{src?.file?.split("/").pop() || "events.jsonl"}</code>.
          </li>
        ) : null}
        {src ? (
          <li>
            Events accepted: {src.accepted} ({r.events_in_week} in the week); lines rejected: {src.rejected_total}.
          </li>
        ) : null}
        <li>
          {c
            ? `Analytics answers: ${c.yes} yes, ${c.no} no, ${c.unasked} not asked yet.`
            : "Analytics answers: counted from this monitor's own users only; open the other environment's monitor to see its own."}
        </li>
        {data.current_week && !data.weeks?.includes(data.current_week) ? (
          <li>This week ({data.current_week}) isn&apos;t in a report yet: the report is written on Monday for the week before.</li>
        ) : null}
      </ul>
    </Panel>
  );
}
