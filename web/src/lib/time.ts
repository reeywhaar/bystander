/**
 * Timestamps arrive as Unix seconds and are rendered in the reader's own zone, here and
 * nowhere else. The server holds everything in UTC precisely so that this is the only
 * place a zone is applied.
 */

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** "3 hours ago", "yesterday", "12 August". */
export function since(unix: number, now = Date.now() / 1000): string {
  const seconds = Math.max(0, Math.floor(now - unix));

  if (seconds < MINUTE) return "just now";
  if (seconds < HOUR) {
    const minutes = Math.floor(seconds / MINUTE);
    return `${minutes} minute${minutes === 1 ? "" : "s"} ago`;
  }
  if (seconds < DAY) {
    const hours = Math.floor(seconds / HOUR);
    return `${hours} hour${hours === 1 ? "" : "s"} ago`;
  }
  if (seconds < 2 * DAY) return "yesterday";
  if (seconds < 7 * DAY) {
    return `${Math.floor(seconds / DAY)} days ago`;
  }
  return absolute(unix);
}

/** "12 August", with the year when it is not this one. */
export function absolute(unix: number, now = Date.now() / 1000): string {
  const date = new Date(unix * 1000);
  const sameYear = date.getFullYear() === new Date(now * 1000).getFullYear();
  return date.toLocaleDateString(undefined, {
    day: "numeric",
    month: "long",
    ...(sameYear ? {} : { year: "numeric" }),
  });
}

/** "in 4 hours", "in 2 days" — for the next page. */
export function until(unix: number, now = Date.now() / 1000): string {
  const seconds = Math.floor(unix - now);
  if (seconds <= 0) return "any moment";
  if (seconds < HOUR) {
    const minutes = Math.max(1, Math.floor(seconds / MINUTE));
    return `in ${minutes} minute${minutes === 1 ? "" : "s"}`;
  }
  if (seconds < DAY) {
    const hours = Math.floor(seconds / HOUR);
    return `in ${hours} hour${hours === 1 ? "" : "s"}`;
  }
  const days = Math.floor(seconds / DAY);
  return `in ${days} day${days === 1 ? "" : "s"}`;
}

/** The exact moment, for a title attribute — where a relative time is not enough. */
export function exact(unix: number): string {
  return new Date(unix * 1000).toLocaleString();
}
