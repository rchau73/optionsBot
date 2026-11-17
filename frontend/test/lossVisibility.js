import { within } from "@testing-library/react";

// The checks of "losses as visible as gains" (#582), shared by
// loss-visibility.test.jsx and scenario-answers.test.jsx (#743): the size and
// weight an element shows, and whether a result is on screen or folded away.

const SIZE = /^(text-(xs|sm|base|lg|xl|[2-9]xl|\[[^\]]+\])|font-(thin|extralight|light|normal|medium|semibold|bold|extrabold|black))$/;

/** The size and weight classes an element shows, its own and inherited, nearest first (colour ignored). */
export function sizeAndWeight(el) {
  const out = [];
  for (let n = el; n && n !== document.body; n = n.parentElement) {
    const own = [...n.classList].filter((c) => SIZE.test(c));
    for (const kind of ["text-", "font-"]) {
      if (!out.some((c) => c.startsWith(kind))) {
        const c = own.find((x) => x.startsWith(kind));
        if (c) out.push(c);
      }
    }
  }
  return out.sort();
}

/** Throws when an element sits in something folded away by default (closed details, hidden, aria-hidden, sr-only); `where` names the page. */
export function assertNotFolded(where, el, container, what) {
  for (let n = el; n && n !== container; n = n.parentElement) {
    const folded = (n.tagName === "DETAILS" && !n.open) || n.hidden || n.getAttribute("aria-hidden") === "true" || n.classList.contains("hidden") || n.classList.contains("sr-only");
    if (folded) throw new Error(`${where}: the result ${what} is folded away by default (${n.tagName.toLowerCase()})`);
  }
  return el;
}

/** Throws, naming the page, when the result is missing or folded away. */
export function shownResult(page, container, matcher) {
  const found = within(container).queryAllByText(matcher);
  if (!found.length) throw new Error(`${page}: the result ${matcher} is not shown (folded away or dropped)`);
  return assertNotFolded(page, found[0], container, matcher);
}
