/**
 * The four intervals a page can be generated on, in seconds.
 *
 * The server holds the same closed set and refuses anything else, so this list is a copy —
 * kept short and in one place precisely because it is one.
 */
export const EDITION_INTERVALS: { seconds: number; label: string }[] = [
  { seconds: 3600, label: "Hourly" },
  { seconds: 21600, label: "Every 6 hours" },
  { seconds: 86400, label: "Daily" },
  { seconds: 604800, label: "Weekly" },
];

/** Where a page's article count may sit. Matches the store's bounds. */
export const EDITION_SIZE = { min: 10, max: 200, step: 10 };

/** Priorities run 0..100 and default to the middle, so every adjustment reads as a move up
 *  or down from ordinary rather than away from an edge. */
export const DEFAULT_PRIORITY = 50;

/** What a priority means, in words, for the places a number alone is unhelpful. */
export function describePriority(priority: number): string {
  if (priority === 0) return "never";
  if (priority < 20) return "rarely";
  if (priority < 40) return "less often";
  if (priority <= 60) return "as usual";
  if (priority < 85) return "more often";
  return "often";
}
