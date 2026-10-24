#!/usr/bin/env node
// Admin CLI for one person's analytics events (event plan §5, #296): export
// them for an access request or delete them for a deletion request.
//
//   AUTH_DB_PATH=… ANALYTICS_SALT=… node frontend/scripts/analytics-user.mjs export|delete <username>
//
// Prints counts and paths only, never an event; every run writes an audit
// line. The logic lives in lib/analytics/userrequest.js (tested there).
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

// lib/ has no "type": "module" (Next.js resolves it), so node warns once per
// file that it parsed ES module syntax there: harmless, kept off the output.
process.removeAllListeners("warning");
process.on("warning", (w) => {
  if (w.code !== "MODULE_TYPELESS_PACKAGE_JSON") console.error(w.toString());
});
const { runUserRequest } = await import("../lib/analytics/userrequest.js");

const cwd = process.cwd();
// sql.js reads its .wasm from <frontend>/node_modules: relative paths are
// resolved against the caller's directory first, then we move there.
process.chdir(path.join(path.dirname(fileURLToPath(import.meta.url)), ".."));

const { code, out } = await runUserRequest({ argv: process.argv.slice(2), cwd, by: `cli:${os.userInfo().username}` });
for (const line of out) (code === 0 ? console.log : console.error)(line);
process.exitCode = code;
