// Lists that every PR adds to are kept in order (#465), so two PRs that each
// add an entry land on different lines instead of colliding at the end.
// tests/sorted_lists_test.go checks the glossary, golden.jsonl and plan.go.

/** The first pair of names out of ascending order, as a sentence, or null. */
export function outOfOrder(names) {
  for (let i = 1; i < names.length; i++) {
    if (names[i] < names[i - 1]) return `"${names[i]}" comes after "${names[i - 1]}"`;
  }
  return null;
}
