// Conditional className composition.
//
// UI-SPEC §Stack & Bundle Targets notes "10-line zero-dep alt acceptable if
// executor prefers" for `clsx`. We re-export the published `clsx` package
// because @xyflow/react v12 already pulls the same transitive dependency
// (no extra bytes). If the bundle gate (<500KB gzipped, plan 04-16) shows
// clsx as a measurable contributor in isolation we'll swap to a hand-rolled
// 10-line variant — the call sites are identical.
export { clsx, clsx as default } from "clsx";
export type { ClassValue } from "clsx";
