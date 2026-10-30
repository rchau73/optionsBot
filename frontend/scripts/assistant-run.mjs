#!/usr/bin/env node
// The live golden-set run (#536): asks the real assistant every question of
// docs/product/assistant/golden.jsonl and writes the answers.jsonl that
// scripts/product/assistant_grade.py grades.
//
//   ANTHROPIC_API_KEY=… node frontend/scripts/assistant-run.mjs --out <answers.jsonl> --log <run.jsonl>
//
// Usually started by scripts/product/assistant_run.sh, which reads the key
// from the environment's .env and grades the answers. The logic lives in
// lib/assistant/goldenrun.js (tested there): a test run, outside the
// per-person limit, paced, with its own log (never assistant.jsonl) marked
// test_run. Without the key it stops before asking anything and writes nothing.
// Exit 0 when every question got its line, 2 on bad input or no key, 3 when
// the run's log could not be written.
import fs from "node:fs";
import { registerHooks } from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const FRONTEND = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");

// lib/ is written for Next.js: "@/lib/…" and extensionless "./…" imports,
// resolved here as Next resolves them.
registerHooks({
  resolve(specifier, context, nextResolve) {
    let spec = specifier.startsWith("@/") ? pathToFileURL(path.join(FRONTEND, specifier.slice(2))).href : specifier;
    if ((spec.startsWith("file:") || spec.startsWith(".")) && !path.extname(spec)) spec += ".js";
    return nextResolve(spec, context);
  },
});
// lib/ has no "type": "module" (Next.js resolves it), so node warns once per
// file that it parsed ES module syntax there: harmless, kept off the output.
process.removeAllListeners("warning");
process.on("warning", (w) => {
  if (w.code !== "MODULE_TYPELESS_PACKAGE_JSON") console.error(w.toString());
});

const { anthropicClient } = await import("../lib/assistant/model.js");
const { HelpIndex } = await import("../lib/assistant/search.js");
const { PAUSE_MS, runGolden, runLogError } = await import("../lib/assistant/goldenrun.js");

function usage(msg) {
  console.error(`assistant-run: ${msg}`);
  console.error("usage: node frontend/scripts/assistant-run.mjs --out <answers.jsonl> --log <run.jsonl> [--golden FILE] [--index FILE] [--pause-ms N]");
  process.exit(2);
}

const opts = {
  golden: path.join(FRONTEND, "..", "docs", "product", "assistant", "golden.jsonl"),
  index: path.join(FRONTEND, "content", "help-index.json"),
  pauseMs: PAUSE_MS,
};
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  const flag = argv[i];
  const value = argv[++i];
  if (value === undefined) usage(`${flag} needs a value`);
  if (flag === "--out") opts.out = value;
  else if (flag === "--log") opts.log = value;
  else if (flag === "--golden") opts.golden = value;
  else if (flag === "--index") opts.index = value;
  else if (flag === "--pause-ms") opts.pauseMs = Number(value);
  else usage(`unknown flag ${flag}`);
}

const model = anthropicClient();
if (!model) {
  console.error("assistant-run: ANTHROPIC_API_KEY is not set: nothing was asked and no answer or score was written");
  process.exit(2);
}
if (!opts.out) usage("--out is required");
const logError = runLogError(opts.log);
if (logError) usage(logError);
if (!Number.isFinite(opts.pauseMs) || opts.pauseMs < 0) usage("--pause-ms must be a number of milliseconds ≥ 0");

let golden, index;
try {
  golden = fs.readFileSync(opts.golden, "utf8").split("\n").filter((l) => l.trim()).map((l) => JSON.parse(l));
  index = new HelpIndex(JSON.parse(fs.readFileSync(opts.index, "utf8")).chunks);
} catch (err) {
  usage(err.message);
}

// Every model call counted, for the cost line.
let calls = 0;
const counted = {
  model: model.model,
  ask: (args) => (calls++, model.ask(args)),
  checkRequest: (args) => (calls++, model.checkRequest(args)),
};

let result;
try {
  result = await runGolden({
    golden,
    model: counted,
    index,
    file: path.resolve(opts.log),
    pauseMs: opts.pauseMs,
    sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
    onAnswer: (a, n, total) => console.error(`${n}/${total} ${a.id} ${a.outcome}`),
  });
} catch (err) {
  console.error(`assistant-run: ${err.message}`);
  process.exit(3);
}
fs.mkdirSync(path.dirname(path.resolve(opts.out)), { recursive: true, mode: 0o700 });
fs.writeFileSync(opts.out, result.answers.map((a) => JSON.stringify(a)).join("\n") + "\n", { mode: 0o600 });
console.log(`assistant-run: ${result.answers.length} questions, ${calls} model calls (${model.model}), ${result.modelErrors} model errors → ${opts.out}`);
