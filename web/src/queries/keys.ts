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
  /** Its own root: the masthead's `me` and the account page answer different questions. */
  account: ["account"] as const,

  /**
   * The root, and one key per page beneath it.
   *
   * Beneath, because a reader with several pages holds several editions at once and switching
   * tabs must not discard the one being left — while marking something read has to be able to
   * reach all of them at a stroke, which invalidating the root does.
   */
  edition: ["edition"] as const,
  editionOf: (page: string) => ["edition", page] as const,
  /** Its own root: marking something read changes both, and neither is a prefix of the other. */
  read: ["read"] as const,

  feeds: ["feeds"] as const,
  feed: (id: string) => ["feeds", id] as const,

  tags: ["tags"] as const,

  /** Under one root, so saving one page also refreshes the strip that lists them all. */
  pages: ["pages"] as const,
  page: (id: string) => ["pages", id] as const,

  /** Under one root, so refreshing the user list never discards the invitation list. */
  adminUsers: ["admin", "users"] as const,
  adminInvites: ["admin", "invites"] as const,
  adminSmtp: ["admin", "smtp"] as const,
  /** How the pictures on this instance are getting on. */
  adminImages: ["admin", "images"] as const,
  adminImagesUnmeasured: (reason: string) =>
    ["admin", "images", "unmeasured", reason] as const,
  /** What this instance serves to strangers. */
  adminInstance: ["admin", "instance"] as const,

  /** Keyed by the token, because two links are two different answers. */
  invite: (token: string) => ["invite", token] as const,
  share: (token: string) => ["share", token] as const,
};
