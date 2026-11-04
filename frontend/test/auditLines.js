// Test helper: the entries of an audit.jsonl file as the code wrote them,
// without the hash chain's fields (prev, line_hash: #619, checked in
// __tests__/audit-chain.test.js). A missing file has none.
import fs from "node:fs";

export function auditEntries(file) {
  if (!fs.existsSync(file)) return [];
  return fs
    .readFileSync(file, "utf8")
    .split("\n")
    .filter(Boolean)
    .map((l) => {
      const { prev: _prev, line_hash: _hash, ...entry } = JSON.parse(l);
      return entry;
    });
}
