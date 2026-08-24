/**
 * The shapes this API answers with.
 *
 * Written by hand against `internal/api`, and `snake_case` because that is what arrives —
 * the server's field names are its Go struct tags and its SQL columns, and renaming them
 * here would mean a field called one thing in the network tab and another in the code.
 */

export type Role = "admin" | "user";

/** How much of the page's sixteen tracks an article takes. See internal/store/editions.go. */
export type Slot =
  "lead" | "wide" | "feature" | "narrow" | "standard" | "brief";

export interface Me {
  id: string;
  username: string;
  role: Role;
  created_at: number;
}

/**
 * Your own account, as only you see it.
 *
 * Separate from [Me], which every island fetches on load and which stays small: a name, a
 * role, and whether to show the admin link. What an account *is* belongs to the one page
 * that shows it.
 */
export interface Account {
  username: string;
  role: Role;
  created_at: number;
  /** An address this account has *proved* it can read, or empty. */
  recovery_email: string;
  /**
   * An address partway through being proved, or empty.
   *
   * So a page reopened mid-flow says which address it is waiting on rather than starting
   * somebody over on a code they are already holding.
   */
  recovery_pending: string;
  /**
   * Whether a relay is configured at all.
   *
   * A recovery address is worth nothing without one, and a page that invited somebody to
   * add an address while quietly being unable to send to it would be making a promise the
   * instance cannot keep.
   */
  mail_configured: boolean;
}

/** A link that hands somebody a list of feeds. */
export interface ShareLink {
  /** The whole URL. It is about to leave this browser, so a path would be no use. */
  url: string;
  count: number;
  expires_at: number;
}

/** What a shared link holds, once opened. */
export interface SharedList {
  /** Who made it. A list of feeds is a recommendation, and one with no name on it is a
   *  list of URLs. */
  from: string;
  expires_at: number;
  /** The same shape a pasted file produces, so the same picker reads it. */
  feeds: PlannedFeed[];
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
  /** The picture's real size, or zero when nothing has measured it yet — see internal/jobs. */
  image_width: number;
  image_height: number;
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

/** One tag on a feed in a pasted list, and which of yours it matched. */
export interface PlannedTag {
  path: string[];
  /** The path rendered for reading: "News / World". */
  name: string;
  /**
   * The tag you already have that this path names, or empty when you have none.
   *
   * The id rather than a flag, because the dialog offers every tag you own under every
   * feed and has to know which of them to tick. Working that out in the browser would be a
   * second implementation of path matching, with its own opinion about case and about the
   * escaping.
   */
  tag_id: string;
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
  /** What to call it here: the override if there is one, the publisher's otherwise. */
  title: string;
  /** What the publisher calls it, always — so a rename can show what it overrides. */
  feed_title: string;
  title_override: string;
  priority: number;
  tag_ids: string[];
  /** Seconds. How old an article from this feed may be and still appear; 0 is no limit. */
  article_window: number;
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

/** How a relay's connection is protected. There is no third option on purpose. */
export type SmtpTls = "starttls" | "implicit";

/** The relay as the interface may show it: everything except the password. */
export interface SmtpConfig {
  /** False when nothing has been set up. The rest are then the form's defaults. */
  configured: boolean;
  host: string;
  port: number;
  tls: SmtpTls;
  username: string;
  /** What recipients see in From, which is routinely not the username. */
  from_address: string;
  /** Blank means the server's own default rather than an empty name. */
  sender_name: string;
  updated_at: number;
}

/** The relay as it is written back. The password is the only field that is write-only. */
export interface SmtpForm {
  host: string;
  port: number;
  tls: SmtpTls;
  username: string;
  /** Empty leaves the stored password alone; required only the first time. */
  password: string;
  from_address: string;
  sender_name: string;
}
