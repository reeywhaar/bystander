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
export const ARTICLE_WINDOWS: {
  seconds: number;
  label: string;
  short: string;
}[] = [
  { seconds: 0, label: "No limit", short: "no limit" },
  { seconds: 31536000, label: "A year", short: "1y" },
  { seconds: 2592000, label: "A month", short: "1m" },
  { seconds: 1209600, label: "Two weeks", short: "2w" },
  { seconds: 604800, label: "A week", short: "1w" },
  { seconds: 86400, label: "A day", short: "1d" },
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

/**
 * A feed's reach as a label, for a list where it is one fact among several.
 *
 * "no limit" stays a phrase because there is no honest abbreviation of it, and it is the one
 * worth reading anyway. Empty for a value the server would refuse, so an unknown number
 * renders as nothing rather than as a chip saying nothing.
 */
export function shortWindow(seconds: number): string {
  return (
    ARTICLE_WINDOWS.find((option) => option.seconds === seconds)?.short ?? ""
  );
}

/**
 * How much a reach is worth noticing, from 0 to 2.
 *
 * A long reach is the one that changes what a page can contain — it is what lets something
 * from last winter turn up beside this morning — so it is the one that gets colour. A day
 * or a week is the ordinary case and is left quiet.
 *
 * Three steps rather than one per option: this is a tint on a label in a list, and six
 * shades of the same idea would be six things to tell apart instead of one to notice.
 */
export function windowWeight(seconds: number): 0 | 1 | 2 {
  if (seconds === 0 || seconds >= 31536000) return 2;
  if (seconds >= 1209600) return 1;
  return 0;
}

/** Where a page's article count may sit. Matches the store's bounds. */
export const EDITION_SIZE = { min: 10, max: 200, step: 10 };

/** Priorities run 0..100 and default to the middle, so every adjustment reads as a move up
 *  or down from ordinary rather than away from an edge. */
export const DEFAULT_PRIORITY = 50;

/**
 * What a feed reaches back by default — a week. Matches `store.DefaultArticleWindow`.
 *
 * Here so a list of feeds about to be added can tell a reach somebody chose from the one the
 * server filled in. The two arrive as the same number: a source that names no reach, or names
 * one this program does not offer, is given the default before it is ever sent. So this is
 * what "nobody said" looks like on the wire, and a chip repeating it says nothing.
 */
export const DEFAULT_ARTICLE_WINDOW = 604800;

/** What a priority means, in words, for the places a number alone is unhelpful. */
export function describePriority(priority: number): string {
  if (priority === 0) return "never";
  if (priority < 20) return "rarely";
  if (priority < 40) return "less often";
  if (priority <= 60) return "as usual";
  if (priority < 85) return "more often";
  return "often";
}

/**
 * What this is and who made it, for the places that say so.
 *
 * Here rather than written into each of them, because it is written into three and the kind
 * of thing that changes once and has to change everywhere: a repository that moves, a name
 * that is spelled differently in one corner.
 *
 * Capitalised here and lowercase in the nameplate, which is not an inconsistency — the
 * nameplate is a wordmark and this is a sentence.
 */
export const PRODUCT = {
  name: "Bystander",
  url: "https://github.com/reeywhaar/bystander",
  author: { name: "Misha Vyrtsev", url: "https://vyrtsev.com" },
};
