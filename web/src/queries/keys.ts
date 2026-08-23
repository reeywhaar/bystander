/**
 * Every query key, in one place.
 *
 * Two things follow from that, and the second is why this file exists at all. Nothing
 * outside it writes a key, so a key cannot be typo'd at a call site; and an invalidation
 * names the same constant the query does, so it cannot address a key that no query uses.
 * TanStack matches by prefix and says nothing when a prefix matches nothing, which makes
 * that second failure completely silent.
 *
 * The hierarchy does the invalidation work by construction: `["feeds"]` is a prefix of
 * every single feed, so a write that changes the listing also refreshes the detail.
 */
export const qk = {
  me: ["me"] as const,

  edition: ["edition"] as const,
  /** Its own root: marking something read changes both, and neither is a prefix of the other. */
  read: ["read"] as const,

  feeds: ["feeds"] as const,
  feed: (id: string) => ["feeds", id] as const,

  tags: ["tags"] as const,
  settings: ["settings"] as const,

  /** Under one root, so refreshing the user list never discards the invitation list. */
  adminUsers: ["admin", "users"] as const,
  adminInvites: ["admin", "invites"] as const,
  adminSmtp: ["admin", "smtp"] as const,

  /** Keyed by the token, because two links are two different answers. */
  invite: (token: string) => ["invite", token] as const,
};
