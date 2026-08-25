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
  /**
   * The name this account's published pages live under, or empty.
   *
   * Its own name and not the username, which is a credential half the world reuses:
   * publishing a page should not oblige anybody to announce theirs.
   */
  public_name: string;
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
   * Whether this instance publishes pages at all.
   *
   * Here for the same reason `mail_configured` is: a screen offering somebody a public name
   * on an instance that will never serve a public page is offering a thing that does not
   * exist. So the whole section is hidden rather than shown and refused.
   */
  public_pages: boolean;
  /**
   * Whether a published page may ask to be indexed here.
   *
   * The administrator's answer, and the interface does not argue with it: where this is false
   * the choice is absent from the publish dialog rather than shown and refused.
   */
  public_indexing: boolean;
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
  /** How far back the list says this feed is worth reading, in seconds. */
  reach: number;
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
  /** Seconds. Carried back so a list's reaches arrive with its feeds. */
  reach: number;
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
  /** The feed itself, which is what a page's feed filter names. */
  feed_id: string;
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
  /** What the server answered with, or 0 when the request never reached one. */
  last_status: number;
  last_error: string;
  /** What the server said when it refused, bounded. Empty when nothing answered. */
  last_error_body: string;
  failure_count: number;
}

export interface Tag {
  id: string;
  name: string;
  parent_id: string | null;
  priority: number;
  created_at: number;
}

/**
 * One front page: what it is called, where it lives, and what it draws from.
 *
 * Everybody has at least one — the main page, served at `/`, whose name and address are fixed
 * and which cannot be removed. The rest live at `/f/:slug`.
 */
export interface Page {
  id: string;
  name: string;
  /** Empty for the main page, which is at `/` rather than at `/f/:slug`. */
  slug: string;
  is_main: boolean;

  /** Seconds. One of the four in `EDITION_INTERVALS`. */
  edition_interval: number;
  edition_size: number;
  next_edition_at: number;
  /** Seconds; zero is no limit. One of `ARTICLE_WINDOWS`. */
  max_article_age: number;

  /**
   * What this page does with each tag and each feed it has an opinion about. Anything on
   * neither side it has no opinion about, which is the ordinary case.
   *
   * The tags are a funnel: any `include_tag_ids` holds the page to subscriptions carrying one
   * of them, and `exclude_tag_ids` then drops what it matches. An empty include side means the
   * page was never narrowed this way, not that it is narrowed to nothing.
   *
   * The feeds override the result in both directions: `include_feed_ids` are on the page
   * whatever the tags decided, `exclude_feed_ids` are off it whatever they decided.
   */
  include_tag_ids: string[];
  exclude_tag_ids: string[];
  include_feed_ids: string[];
  exclude_feed_ids: string[];

  /**
   * Where this page lives on the open web, under its owner's public name:
   * `/p/<their name>/<this>`. Kept when a page is taken down, so publishing it again offers
   * the address the links already point at.
   */
  publish_slug: string;
  /** Whether that address answers. */
  published: boolean;
  /** Whether a search engine may keep it, after both the owner and the instance were asked. */
  indexable: boolean;
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

/** One article in a feed nobody has subscribed to yet. */
export interface PreviewItem {
  title: string;
  link: string;
  /**
   * The same picture a card would carry, and for some feeds the entire article: a comic's
   * summary is an `<img>` and nothing else, and the sanitizer drops images. Without this a
   * comics feed previews as a list of titles and dates — which is the kind of feed somebody
   * most needs to look at before following.
   */
  image_url: string;
  /**
   * Sanitized on the server, at parse, by the pass every stored summary goes through — an
   * allowlist of a dozen tags with every script and every attribute but a resolved href
   * removed. What is looked at here is what would arrive.
   */
  summary: string;
  published_at: number;
}

/** What a feed has published, for somebody deciding whether to follow it. */
export interface FeedPreview {
  title: string;
  site_url: string;
  feed_url: string;
  /** The most recent first, capped by the server. */
  items: PreviewItem[];
}

/** One reason pictures are unmeasured, and how many. */
export interface ImageFailure {
  /**
   * Empty for pictures nothing has asked about yet — a queue that has not caught up rather
   * than a failure. The others are the categories the measurer records: gone, refused, busy,
   * unreachable, undecodable, empty.
   */
  reason: string;
  count: number;
}

/** One picture behind a reason on the images screen. */
export interface UnmeasuredImage {
  url: string;
  reason: string;
  /** When the queue will ask again, in Unix seconds. Zero means it is already due. */
  retry_at: number;
  /** How many articles are waiting on this one picture. */
  articles: number;
  /** The newest of those articles, because a bare CDN address names no publisher. */
  title: string;
}

/** How the pictures on this instance are getting on. */
export interface ImageTally {
  /** Distinct pictures, because one is measured once however many articles use it. */
  pictures: number;
  measured: number;
  unmeasured: number;
  /** Largest group first: it is the answer more often than not. */
  failures: ImageFailure[];
}

/**
 * Somebody's published page.
 *
 * Whose it is appears in the address and nowhere else: the public name is the only identity
 * its owner chose to expose, and putting a username beside it would expose one they did not.
 *
 * The read marks on it are *yours*, never the owner's — whether they have read something is a
 * fact about them, and publishing a page is not an offer to publish that too. A stranger has
 * none, so every article arrives unmarked.
 */
export interface PublicPage {
  /**
   * The edition's id, which is what every card's appearance is drawn from.
   *
   * The page has to be seeded with it and nothing else, or the same edition renders
   * differently for a stranger than for the person who published it.
   */
  id: string;
  name: string;
  generated_at: number;
  /**
   * Whether whoever asked has an account here.
   *
   * Decides whether a way to mark anything read is offered at all: a control that exists and
   * refuses is worse than one that is not there, and a stranger has no read state to act on.
   */
  signed_in: boolean;
  /** Whether a search engine may keep this, after both the owner and the instance were asked. */
  indexable: boolean;
  items: Article[];
}

/**
 * The answers that belong to the instance rather than to anybody on it.
 *
 * Both off until an administrator says otherwise, and the asymmetry between them is the point:
 * publishing is reversible and indexing is not. Taking a page down is a switch; taking it out
 * of somebody else's search index is a request nobody controls.
 */
export interface InstanceSettings {
  /** Whether anybody here may publish a page. Off takes every published page down. */
  public_pages: boolean;
  /** A ceiling on the per-page choice, not a default for it. */
  public_indexing: boolean;
}
