/**
 * The shapes this API answers with.
 *
 * Written by hand against `internal/api`, and `snake_case` because that is what arrives —
 * the server's field names are its Go struct tags and its SQL columns, and renaming them
 * here would mean a field called one thing in the network tab and another in the code.
 */

export type Role = "admin" | "user";

export type Slot = "lead" | "feature" | "standard" | "brief";

export interface Me {
  id: string;
  username: string;
  role: Role;
  created_at: number;
}

/** What an invitation link is, before anybody types a password into it. */
export interface Invite {
  role: Role;
  expires_at: number;
  usable: boolean;
  accepted: boolean;
  expired: boolean;
}

export interface FeedStub {
  id: string;
  title: string;
  site_url: string;
}

export interface Article {
  id: string;
  rank: number;
  slot: Slot;
  read_at: number | null;
  title: string;
  link: string;
  author: string;
  /** Sanitized on the server, at ingest. Never sanitized again here — see the reader. */
  summary: string;
  image_url: string;
  published_at: number;
  feed: FeedStub;
}

export interface Edition {
  id: string;
  generated_at: number;
  next_edition_at: number;
  size: number;
  items: Article[];
}

/** A feed as its follower sees it: what they chose, plus what the fetcher learned. */
/** A subscription list, ready to be copied or saved. */
export interface Export {
  opml: string;
  filename: string;
  count: number;
}

/** One tag on a feed in a pasted list, and whether it is already yours. */
export interface PlannedTag {
  path: string[];
  /** The path rendered for reading: "News / World". */
  name: string;
  existing: boolean;
}

/** One feed in a pasted list, as it would land here. */
export interface PlannedFeed {
  title: string;
  feed_url: string;
  site_url: string;
  priority: number;
  already_subscribed: boolean;
  tags: PlannedTag[];
}

export interface ImportPlan {
  feeds: PlannedFeed[];
}

/** What the interface decided to keep. Unticking something is not sending it. */
export interface ImportSelection {
  feed_url: string;
  title: string;
  site_url: string;
  priority: number;
  tag_paths: string[][];
}

export interface ImportResult {
  added: number;
  skipped: number;
  failed: { feed_url: string; error: string }[];
  tags_created: string[];
}

/** A feed a URL offers, before anybody has subscribed to it. */
export interface Candidate {
  url: string;
  /** The publisher's own label — how a site distinguishes "Posts" from "Comments". */
  title: string;
  type: string;
}

/**
 * Something already read, as remembered after its page is gone.
 *
 * Not an article on a page: it has no slot and no rank, because it is not on one. The
 * server keeps these for a month and prunes them.
 */
export interface ReadArticle {
  item_id: string;
  title: string;
  link: string;
  published_at: number;
  read_at: number;
  /** Empty `id` once the feed is no longer followed — the title survives, the source does not. */
  feed: FeedStub;
}

export interface Subscription {
  id: string;
  url: string;
  site_url: string;
  title: string;
  title_override: string;
  priority: number;
  tag_ids: string[];
  created_at: number;
  last_success_at: number | null;
  last_error: string;
  failure_count: number;
}

export interface Tag {
  id: string;
  name: string;
  parent_id: string | null;
  priority: number;
  created_at: number;
}

export interface Settings {
  /** Seconds. One of the four in `EDITION_INTERVALS`. */
  edition_interval: number;
  edition_size: number;
  next_edition_at: number;
}

export interface User {
  id: string;
  username: string;
  role: Role;
  created_at: number;
  disabled_at: number | null;
  feed_count: number;
}

export interface AdminInvite {
  id: string;
  role: Role;
  created_at: number;
  expires_at: number;
  accepted_at: number | null;
  /** Who the invitation became, once accepted. */
  username: string;
  /** Present only in the response that minted it. It is never readable again. */
  url?: string;
}
