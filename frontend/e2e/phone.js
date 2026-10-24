// Pure helpers of the phone-width check (#309): turn what the browser
// measured (e2e/measure.js) into the report, and compare a report with the
// recorded baseline. No browser, no file system: Jest tests them directly.

/** The widths the audit (docs/responsive-audit.md, #207) estimated. */
export const WIDTHS = [
  { name: "360", width: 360, height: 800 }, // small Android
  { name: "390", width: 390, height: 844 }, // iPhone 12–15
  { name: "768", width: 768, height: 1024 }, // iPad portrait
];

/** Where each dialog must fit: a small phone held upright and on its side. */
export const DIALOG_VIEWPORTS = [
  { name: "portrait", width: 360, height: 640 },
  { name: "landscape", width: 640, height: 360 },
];

export const TARGET_AAA = 44; // WCAG 2.5.5
export const TARGET_AA = 24; // WCAG 2.2, 2.5.8

// Demo data moves (prices, new events), so lengths wobble a little between
// runs: a measurement is worse only beyond these margins.
export const OVERFLOW_TOLERANCE_PX = 16;
export const HEADER_TOLERANCE_PX = 24;

/**
 * A label stable across runs: spaces collapsed, numbers replaced by "#"
 * (prices, counts and times change on demo data), at most 40 characters.
 */
export function normaliseLabel(text) {
  const s = String(text ?? "")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\d+([.,:]\d+)*/g, "#");
  return s.length > 40 ? `${s.slice(0, 39)}…` : s;
}

/** "Open positions › button "Close"": where a control is and what it says. */
export function controlKey(c) {
  return `${normaliseLabel(c.panel) || "Page"} › ${c.kind} "${normaliseLabel(c.label) || "(no label)"}"`;
}

/**
 * The kinds of control under `limit` px (width or height), each with its
 * smallest box, sorted by key — one entry per kind, however many rows repeat it.
 */
export function smallKinds(controls, limit) {
  const by = new Map();
  for (const c of controls) {
    if (Math.min(c.width, c.height) >= limit) continue;
    const key = controlKey(c);
    const prev = by.get(key);
    const w = Math.round(c.width);
    const h = Math.round(c.height);
    if (!prev) by.set(key, { key, width: w, height: h, count: 1 });
    else {
      prev.count += 1;
      if (Math.min(w, h) < Math.min(prev.width, prev.height)) Object.assign(prev, { width: w, height: h });
    }
  }
  return [...by.values()].sort((a, b) => a.key.localeCompare(b.key));
}

/** One width's raw measurement (measurePage's result) → its part of the report. */
export function summariseWidth(viewport, raw) {
  const controls = raw.controls ?? [];
  const under = (n) => controls.filter((c) => Math.min(c.width, c.height) < n).length;
  const titleKinds = [...new Set((raw.titleOnly ?? []).map((t) => `${normaliseLabel(t.panel) || "Page"} › ${normaliseLabel(t.label) || t.tag}`))].sort();
  return {
    viewport: { width: viewport.width, height: viewport.height },
    overflow: {
      scroll_width: raw.overflow.scrollWidth,
      client_width: raw.overflow.clientWidth,
      overflow_px: Math.max(0, raw.overflow.scrollWidth - raw.overflow.clientWidth),
      causes: (raw.overflow.causes ?? []).map((c) => ({ panel: normaliseLabel(c.panel), element: c.element, width_px: Math.round(c.width) })),
    },
    controls: {
      total: controls.length,
      [`under_${TARGET_AAA}`]: under(TARGET_AAA),
      [`under_${TARGET_AA}`]: under(TARGET_AA),
      [`under_${TARGET_AA}_kinds`]: smallKinds(controls, TARGET_AA),
      [`under_${TARGET_AAA}_kinds`]: smallKinds(controls, TARGET_AAA),
    },
    title_only: { count: (raw.titleOnly ?? []).length, kinds: titleKinds },
    header_height_px: Math.round(raw.headerHeight ?? 0),
  };
}

/** A dialog's box in one viewport → { width, height, top, bottom, fits }. */
export function summariseDialog(viewport, box) {
  if (!box) return { opened: false, fits: false };
  const fits = box.top >= 0 && box.left >= 0 && box.bottom <= viewport.height && box.right <= viewport.width;
  return {
    opened: true,
    width_px: Math.round(box.width),
    height_px: Math.round(box.height),
    top_px: Math.round(box.top),
    bottom_px: Math.round(box.bottom),
    fits,
  };
}

/** What the baseline keeps of a report: only what compareToBaseline reads. */
export function baselineOf(report) {
  const widths = {};
  for (const [name, w] of Object.entries(report.widths)) {
    widths[name] = {
      overflow_px: w.overflow.overflow_px,
      overflow_causes: w.overflow.causes.map(causeKey).sort(),
      [`under_${TARGET_AA}`]: w.controls[`under_${TARGET_AA}_kinds`].map((k) => k.key),
      [`under_${TARGET_AAA}`]: w.controls[`under_${TARGET_AAA}_kinds`].map((k) => k.key),
      title_only: w.title_only.kinds,
      header_height_px: w.header_height_px,
    };
  }
  const dialogs = {};
  for (const [name, d] of Object.entries(report.dialogs ?? {})) {
    dialogs[name] = Object.fromEntries(Object.entries(d).map(([vp, m]) => [vp, { fits: m.fits }]));
  }
  return { widths, dialogs };
}

function causeKey(c) {
  return `${c.panel} › ${c.element}`;
}

/**
 * Every way `report` is worse than `baseline`, one sentence each naming what
 * got worse; [] when nothing did. Today's failures are in the baseline and
 * pass; only something new fails: a new cause of page overflow or more
 * overflow, a new kind of control under 24 or 44 px, a new hover-only
 * explanation, a taller header, a dialog that no longer fits.
 */
export function compareToBaseline(baseline, report) {
  const problems = [];
  for (const [name, w] of Object.entries(report.widths)) {
    const base = baseline.widths?.[name];
    if (!base) {
      problems.push(`${name} px: not in the baseline (record it with UPDATE_PHONE_BASELINE=1)`);
      continue;
    }
    for (const c of w.overflow.causes) {
      if (!base.overflow_causes.includes(causeKey(c))) problems.push(`${name} px: page overflow now caused by ${causeKey(c)} (${c.width_px} px wide)`);
    }
    if (w.overflow.overflow_px > base.overflow_px + OVERFLOW_TOLERANCE_PX) {
      problems.push(`${name} px: page overflows by ${w.overflow.overflow_px} px, baseline ${base.overflow_px} px`);
    }
    for (const limit of [TARGET_AA, TARGET_AAA]) {
      const known = new Set(base[`under_${limit}`]);
      for (const k of w.controls[`under_${limit}_kinds`]) {
        if (!known.has(k.key)) problems.push(`${name} px: new control under ${limit} px: ${k.key} (${k.width}×${k.height} px)`);
      }
    }
    const titles = new Set(base.title_only);
    for (const t of w.title_only.kinds) {
      if (!titles.has(t)) problems.push(`${name} px: new hover-only explanation (title): ${t}`);
    }
    if (w.header_height_px > base.header_height_px + HEADER_TOLERANCE_PX) {
      problems.push(`${name} px: header ${w.header_height_px} px tall, baseline ${base.header_height_px} px`);
    }
  }
  for (const [name, d] of Object.entries(report.dialogs ?? {})) {
    for (const [vp, m] of Object.entries(d)) {
      if (baseline.dialogs?.[name]?.[vp]?.fits && !m.fits) problems.push(`${name} dialog no longer fits ${vp}`);
    }
  }
  return problems;
}
