import { resolveCitation } from "@/lib/assistant/service";

const passages = [
  { id: "explain:closed:delta_exit/1", source: "explain:closed:delta_exit" },
  { id: "glossary:delta/1", source: "glossary:delta" },
  { id: "glossary:delta exit/1", source: "glossary:delta exit" },
];

test.each([
  ["a listed id stays", "glossary:delta/1", "glossary:delta/1"],
  ["a listed source stays", "glossary:delta", "glossary:delta"],
  ["an id without its first part is the one it ends", "closed:delta_exit/1", "explain:closed:delta_exit/1"],
  ["a source without its first part is the one it ends", "closed:delta_exit", "explain:closed:delta_exit"],
  ["an unknown id stays, for the guard to reject", "glossary:gamma/1", "glossary:gamma/1"],
  ["a suffix two ids end with stays", "1", "1"],
])("resolveCitation: %s (#412)", (_name, c, want) => {
  expect(resolveCitation(c, passages)).toBe(want);
});
