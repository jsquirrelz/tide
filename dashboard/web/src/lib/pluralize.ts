// Naive English pluralizer for node summary counts ("1 plan" / "2 plans").
// All TIDE count nouns (milestone/phase/plan/task/wave) are regular, so a
// trailing-"s" rule covers every call site; if an irregular noun ever appears
// in a summary, special-case it here rather than at the call site.
export function pluralize(n: number, singular: string): string {
  return `${n} ${n === 1 ? singular : singular + "s"}`;
}
