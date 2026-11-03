// The browser name a session shows in Active sessions (#584), read from the
// User-Agent header at sign-in. Only this name is stored, never the header.

// Order matters: Edge and Opera say "Chrome" too, Chrome says "Safari".
const BROWSERS = [
  [/Edg(e|A|iOS)?\//, "Edge"],
  [/OPR\/|Opera/, "Opera"],
  [/Firefox\/|FxiOS\//, "Firefox"],
  [/Chrome\/|CriOS\//, "Chrome"],
  [/Safari\//, "Safari"],
];
// Android before Linux, iPhone/iPad before Mac OS X (iPad says "Mac OS X" too).
const SYSTEMS = [
  [/Windows/, "Windows"],
  [/Android/, "Android"],
  [/iPhone|iPad|iPod/, "iOS"],
  [/Mac OS X|Macintosh/, "macOS"],
  [/CrOS/, "ChromeOS"],
  [/Linux/, "Linux"],
];

const first = (list, ua) => list.find(([re]) => re.test(ua))?.[1] ?? null;

/** "Chrome on Windows", "Safari on iOS", "Firefox", … or "Unknown browser". */
export function browserName(userAgent) {
  if (typeof userAgent !== "string" || !userAgent) return "Unknown browser";
  const browser = first(BROWSERS, userAgent);
  const system = first(SYSTEMS, userAgent);
  if (browser && system) return `${browser} on ${system}`;
  return browser ?? (system ? `A browser on ${system}` : "Unknown browser");
}
