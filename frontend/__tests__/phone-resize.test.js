// Text resize and reflow of the phone check (#887): the counts per page and pass,
// their ranking and that the a11y report keeps its 360 px fields. The browser
// measure itself is pinned by e2e/phone.spec.js (`npm run test:phone`).
import { RESIZE_PASSES, a11yReport, rankResize, summariseResize } from "@/e2e/phone";

const raw = {
  overflowPx: 30.4,
  clipped: [{ tag: "span", text: "Net 12 345", selector: "main > span" }],
  offscreen: [
    { kind: "button", label: "Close", selector: "button" },
    { kind: "button", label: "Close", selector: "button" },
  ],
};

describe("summariseResize", () => {
  test("counts overflow, clipped text and off-screen controls", () => {
    const s = summariseResize(raw);
    expect(s).toMatchObject({ overflow_px: 30, clipped_text: 1, offscreen_controls: 2 });
    expect(s.offscreen_kinds).toHaveLength(1);
  });

  test("an empty measurement is all zero", () => {
    expect(summariseResize({})).toMatchObject({ overflow_px: 0, clipped_text: 0, offscreen_controls: 0 });
  });
});

describe("rankResize", () => {
  const zero = summariseResize({});
  const bad = summariseResize(raw);
  const pages = {
    overview: { text_200: bad, width_320: zero },
    details: { text_200: bad, width_320: bad },
    income: { text_200: zero, width_320: zero },
  };

  test("ranks by pages affected and names the pages", () => {
    const ranked = rankResize(pages);
    expect(ranked[0]).toMatchObject({ pass: "text_200", page_count: 2, pages: ["details", "overview"] });
    expect(ranked.every((f) => f.page_count > 0)).toBe(true);
    expect(ranked.some((f) => f.pages.includes("income"))).toBe(false);
    expect(ranked.map((f) => f.page_count)).toEqual([...ranked.map((f) => f.page_count)].sort((a, b) => b - a));
  });
});

describe("a11yReport", () => {
  test("keeps its fields and adds the resize section only when given", () => {
    const plain = a11yReport({}, "t");
    expect(plain.resize).toBeUndefined();
    expect(plain).toMatchObject({ viewport_px: 360, report_only: true, ranked: [] });
    const withResize = a11yReport({}, "t", { overview: { text_200: summariseResize(raw) } });
    expect(withResize.resize.passes.map((p) => p.name)).toEqual(RESIZE_PASSES.map((p) => p.name));
    expect(withResize.resize.pages.overview.text_200.clipped_text).toBe(1);
    expect(withResize.resize.ranked).toHaveLength(3);
  });
});
