// Runs inside the page (page.evaluate): every function here must be
// self-contained — no imports, no module-level helpers. It measures only; the
// one change it makes (hiding a block to see whether the overflow goes) is
// undone at once.

/**
 * The page as it is now: horizontal overflow and what causes it, every
 * visible interactive control's box, the elements whose only explanation is
 * a `title`, and the header's height. Labels are raw: e2e/phone.js
 * normalises them.
 */
export function measurePage() {
  const root = document.documentElement;
  const text = (el) => (el?.innerText ?? el?.textContent ?? "").replace(/\s+/g, " ").trim();

  // Where an element is: its panel's title, the top header, or a dialog.
  const panelOf = (el) => {
    // The assistant (a side panel on desktop, a dialog-sheet on a phone).
    const aside = el.closest("aside[aria-label]");
    if (aside) return `Panel: ${aside.getAttribute("aria-label")}`;
    const dialog = el.closest("[role=dialog]");
    if (dialog) return `Dialog: ${text(dialog.querySelector("h2")) || "untitled"}`;
    const section = el.closest("section");
    if (section) return text(section.querySelector("h2")) || "untitled panel";
    if (el.closest("header")) return "Header";
    return "Page";
  };
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return false;
    const s = getComputedStyle(el);
    return s.visibility !== "hidden" && s.display !== "none";
  };

  // Overflow: hide each block in turn; a block whose absence lowers the
  // page's scroll width is a cause, narrowed to the table inside it if one is.
  const overflowPx = () => root.scrollWidth - root.clientWidth;
  const without = (el) => {
    const prev = el.style.display;
    el.style.display = "none";
    const px = overflowPx();
    el.style.display = prev;
    return px;
  };
  const header = [...document.querySelectorAll("header")].find((h) => !h.closest("section") && !h.closest("aside")) ?? null;
  const causes = [];
  const base = overflowPx();
  if (base > 0) {
    const blocks = [header, ...document.querySelectorAll("nav[aria-label=Main], main section, aside[aria-label]")].filter(Boolean);
    for (const block of blocks) {
      if (without(block) >= base) continue;
      const tables = [...block.querySelectorAll("table")].filter((t) => without(t) < base);
      const panel = panelOf(block);
      if (tables.length) for (const t of tables) causes.push({ panel, element: "table", width: t.getBoundingClientRect().width });
      else causes.push({ panel, element: block.tagName === "NAV" ? "menu bar" : "panel", width: block.scrollWidth });
    }
    // A page without panels: the block of <main> whose absence lowers it.
    if (!causes.length) {
      for (const block of document.querySelectorAll("main > *")) {
        if (without(block) < base) causes.push({ panel: panelOf(block), element: block.tagName.toLowerCase(), width: block.scrollWidth });
      }
    }
  }

  const CONTROLS = "a[href], button, input:not([type=hidden]), select, textarea, summary, [role=button], [role=tab], [role=menuitem], [tabindex]:not([tabindex='-1'])";
  const labelOf = (el) => {
    const tag = el.tagName.toLowerCase();
    if (tag === "select" || tag === "input" || tag === "textarea") {
      return el.getAttribute("aria-label") || text(el.labels?.[0]) || el.name || el.type || tag;
    }
    return el.getAttribute("aria-label") || text(el.querySelector("h2")) || text(el) || el.getAttribute("title") || tag;
  };
  const controls = [...document.querySelectorAll(CONTROLS)].filter(visible).map((el) => {
    const r = el.getBoundingClientRect();
    const role = el.getAttribute("role");
    return { panel: panelOf(el), kind: role ? `${el.tagName.toLowerCase()}[${role}]` : el.tagName.toLowerCase(), label: labelOf(el), width: r.width, height: r.height };
  });

  // A title whose words are not on screen and not otherwise described: hover-only.
  const titleOnly = [...document.querySelectorAll("[title]")]
    .filter((el) => visible(el) && el.getAttribute("title").trim() && !el.hasAttribute("aria-describedby"))
    .filter((el) => !text(el).includes(el.getAttribute("title").trim()))
    .map((el) => ({ panel: panelOf(el), label: text(el), tag: el.tagName.toLowerCase() }));

  return {
    overflow: { scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, causes },
    controls,
    titleOnly,
    headerHeight: header ? header.getBoundingClientRect().height : 0,
  };
}

/** The open dialog's box (the card inside the overlay: a div or a form), or null. */
export function measureDialog() {
  const box = document.querySelector("[role=dialog]")?.firstElementChild;
  if (!box) return null;
  const r = box.getBoundingClientRect();
  return { width: r.width, height: r.height, top: r.top, bottom: r.bottom, left: r.left, right: r.right };
}
