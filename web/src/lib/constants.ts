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

/**
 * How recent an article must be to reach a page, in seconds. Zero is no limit.
 *
 * The server holds the same closed set and refuses anything else. "No limit" is bounded in
 * practice by how long articles are kept — which follows the longest window anybody has
 * chosen, so choosing a year means a year is kept.
 */
export const ARTICLE_WINDOWS: { seconds: number; label: string }[] = [
  { seconds: 0, label: "No limit" },
  { seconds: 31536000, label: "A year" },
  { seconds: 2592000, label: "A month" },
  { seconds: 1209600, label: "Two weeks" },
  { seconds: 604800, label: "A week" },
  { seconds: 86400, label: "A day" },
];

/**
 * A feed's reach, in words, for the places a row of buttons will not fit.
 *
 * "No limit" does not survive being put in a sentence — "reaches back no limit" — so that
 * one is phrased rather than looked up.
 */
export function describeWindow(seconds: number): string {
  if (seconds === 0) return "reaches back without limit";
  const window = ARTICLE_WINDOWS.find((option) => option.seconds === seconds);
  return window ? `reaches back ${window.label.toLowerCase()}` : "";
}

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
