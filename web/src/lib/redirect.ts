/**
 * Where to go after signing in.
 *
 * `?next=` is honoured only when it names a path on this origin. A full URL would let a
 * link of the form `/login?next=https://elsewhere` turn the login page into somebody
 * else's redirect — the classic open redirect, and a phishing primitive precisely because
 * the domain in the address bar is genuinely ours right up until the moment it is not.
 *
 * The leading-double-slash case is why this is not simply "starts with a slash":
 * `//evil.example` is a protocol-relative URL, and a browser follows it off-origin.
 * Backslashes are here because some browsers have historically normalised `\` to `/`.
 */
export function safeNext(search: string, fallback = "/"): string {
  const next = new URLSearchParams(search).get("next");
  if (!next) return fallback;
  if (!next.startsWith("/")) return fallback;
  if (next.startsWith("//") || next.startsWith("/\\")) return fallback;
  return next;
}
