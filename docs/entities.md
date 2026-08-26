# Entities

Two SQLite databases under `/data`, both WAL.

```
main.db      what a person typed        back this up
derived.db   what the machine produced  delete it freely
```

They are separate handles and are **never `ATTACH`ed**. SQLite's atomic multi-database
commit does not hold under `journal_mode=WAL`, so a transaction that spanned both would
be a transaction only on paper. No foreign key crosses the boundary; `derived.db` stores
`feed_id` and `principal_id` as plain text and tolerates them pointing at nothing.

Every connection is opened with:

```
journal_mode(WAL) busy_timeout(5000) synchronous(NORMAL) foreign_keys(ON)
```

and `SetMaxOpenConns(1)`. Writes are serialised anyway, and one connection removes lock
contention as a category of bug.

Timestamps are Unix seconds. Ids are prefixed ULIDs — see [conventions.md](conventions.md).

---

## main.db

### `principals`

```sql
CREATE TABLE principals (
  id            TEXT    PRIMARY KEY,                        -- p_…
  username      TEXT    NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT    NOT NULL,                           -- bcrypt, cost 12
  role          TEXT    NOT NULL CHECK (role IN ('admin','user')),
  created_at    INTEGER NOT NULL,
  disabled_at   INTEGER,
  deleted_at            INTEGER,                            -- asked to be erased
  deletion_cancelled_at INTEGER,                            -- ...and withdrew it by signing in
  slug          TEXT    NOT NULL DEFAULT ''                 -- the public name; empty until chosen
);
CREATE UNIQUE INDEX principals_slug ON principals(slug) WHERE slug <> '';
```

`COLLATE NOCASE` on the username: `Alice` and `alice` are the same person trying to log
in, and letting both exist is a support ticket waiting to happen.

Bcrypt hashes only the first 72 bytes of input, so the password is length-checked on the
way in rather than silently truncated. The hash carries its own salt and cost, so raising
the cost later does not invalidate existing rows.

Disabling sets `disabled_at` and deletes the principal's sessions. It does not delete
their feeds — a disabled account that is re-enabled should find its subscriptions where
it left them.

`deleted_at` and `deletion_cancelled_at` are somebody leaving. Asking sets `deleted_at` and
ends every session; the account keeps working, and a sweep erases it once `DeletionGrace` —
a week — has passed. **Signing in withdraws the request**, which is not a courtesy: a
deletion pressed by mistake or through a borrowed session has to be recoverable by the person
who owns the account, without asking anybody, and signing in is the one thing they can always
do. `deletion_cancelled_at` records that withdrawal, because otherwise it is invisible —
an account quietly stops being scheduled and nothing says why or when.

The purge takes what belongs to the person and leaves what belongs to everybody. In `main.db`
that is the cascade: sessions, tags, subscriptions, pages and their filters, recovery,
shares — invitations keep their row and lose their pointer. In `derived.db` it is done
explicitly rather than left to the orphan sweep, because an erasure that depends on a garbage
collector running later is an erasure that has not happened yet. **Feeds and items are never
touched**: they are held once for the whole instance, a subscription is the only part of that
relationship anybody owns, and one person leaving must not take another person's reading with
them. A feed left with no followers is collected afterwards by `DeleteOrphanFeeds`, on the
same terms as unsubscribing from it by hand.

`slug` is the name a published page is addressed by, and it is deliberately **not** the
username. Two names for two jobs: one to sign in with, one to be known by — and the one to
sign in with is a credential half the world reuses, so putting it in a URL that gets shared
hands out half of somebody's login. Empty until they choose one, and nothing can be published
before they do.

The unique index is partial, so empty is not a value that collides: every account that has
not chosen a name has the same empty string, and only chosen names have to be distinct.

Published pages are addressed as `/p/<slug>/<publish_slug>`, built from the name rather than
stored beside the page — so changing the name moves every published page and the old links
stop working, which is what changing your name means. Clearing it unpublishes them all, in the
same transaction that clears it. See `SetPublicName` in `store/principals.go`.

### `sessions`

```sql
CREATE TABLE sessions (
  id_hash         BLOB    PRIMARY KEY,                      -- sha256(cookie value)
  principal_id    TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  created_at      INTEGER NOT NULL,
  last_seen_at    INTEGER NOT NULL,
  expires_at      INTEGER NOT NULL,
  last_ip         TEXT    NOT NULL DEFAULT '',
  last_user_agent TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sessions_expires   ON sessions(expires_at);
CREATE INDEX sessions_principal ON sessions(principal_id);
```

The table is keyed by the **hash** of the cookie value, never the value. A database file,
a backup, a heap dump or a swapped page therefore never contains something replayable as
a credential, and the lookup is timing-safe for free: telling two rows apart by timing
would require a 256-bit preimage first.

Sliding expiry of one week since last use. The slide is throttled to once an hour —
without that, a polling SPA rewrites the row and emits a `Set-Cookie` on every single
request for a window measured in days.

`last_ip` and `last_user_agent` are what makes a list of your own sessions worth having: a
row saying nothing but a time cannot be recognised or disowned. Neither is evidence of
anything, and nothing decides anything on them — an address belongs to a network rather than
to a person, and a user agent is a sentence the browser wrote about itself. They are written
on the same hourly throttle, plus whenever either changes, floored at one write a minute per
session by `session.Move`. That floor is not decoration: a dual-stack browser alternating
between its IPv4 and IPv6 route to the same origin would otherwise rewrite the row on every
request, which is the churn the hourly throttle exists to prevent arriving by another door.

A session is named in public by `ids.Derive(ids.Session, id_hash)` — a hash of the hash, so
the stored one never leaves the store either, and there is no second column to keep in step.
Revoking by that id reads the account's own rows and matches, which is the scoping as well as
the lookup: somebody else's id matches nothing rather than deleting their session.

The address is resolved through whatever proxies are in front — see `clientIP` in
`session/client.go`. `X-Forwarded-For` is written by whoever sends it, so the question is
never what the header says but who said it, and the answer needs no configuration: a header
is believed only when the peer that handed us the request is itself on the loopback or a
private network, which is where a reverse proxy in a compose file sits and is not where the
internet is. The chain is then walked from the right, discarding private hops, because a
client can prepend anything it likes and only what our own proxies appended means anything.

### `invites`

```sql
CREATE TABLE invites (
  id           TEXT    PRIMARY KEY,                         -- i_…
  token_hash   BLOB    NOT NULL UNIQUE,                     -- sha256(token)
  role         TEXT    NOT NULL CHECK (role IN ('admin','user')),
  created_by   TEXT    REFERENCES principals(id) ON DELETE SET NULL,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  accepted_at  INTEGER,
  principal_id TEXT    REFERENCES principals(id) ON DELETE SET NULL,
  email        TEXT    NOT NULL DEFAULT ''             -- '' when handed over rather than sent
);
```

Single use, seven days. Hashed for the same reason sessions are. `created_by` survives
its creator's deletion as `NULL`, because who issued an invitation is a fact about the
invitation.

An accepted invite keeps its row and points at the principal it produced. That is the
audit trail, and it is the reason accepting sets `accepted_at` rather than deleting.

**It survives that principal too.** `principal_id` was `ON DELETE CASCADE`, which meant
deleting an account deleted the invitation that made it, taking the answer to "who let this
person in" with it — the one question the row is kept for. It is `SET NULL` now, matching
`created_by` beside it, which had already settled the same question the same way. The listing
then shows the invitation as accepted with nobody to name, which is the truth about an account
that has been deleted.

Nothing about single use rests on this. An accepted invitation is refused by its `accepted_at`
stamp, and the row keeps that either way; before, a deleted account left no row and the link
died by being unknown instead. What was being lost was the record, never the guarantee.

`email` is the address it was sent to, and accepting binds it to the new account as a
**proved** recovery address — straight into `user_recovery`, with no code to type. The proof
is that the link went to that inbox and nowhere else, which is why the API does not hand the
link back for an invitation it sent. See [mail.md](mail.md#invitations).

### `tags`

```sql
CREATE TABLE tags (
  id           TEXT    PRIMARY KEY,                         -- t_…
  principal_id TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  name         TEXT    NOT NULL,
  parent_id    TEXT    REFERENCES tags(id) ON DELETE SET NULL,
  priority     INTEGER NOT NULL DEFAULT 50 CHECK (priority BETWEEN 0 AND 100),
  created_at   INTEGER NOT NULL
);
CREATE UNIQUE INDEX tags_name ON tags(principal_id, ifnull(parent_id,''), name);
CREATE INDEX tags_parent ON tags(parent_id);
```

The unique index is over `ifnull(parent_id,'')` rather than `parent_id` because **SQLite
treats NULLs as distinct in a UNIQUE constraint** — without the `ifnull` a person could
create two root tags both called "Art" and neither the database nor the interface would
object.

Tags are per principal. A taxonomy is a personal thing, and a shared one would need an
owner and a merge policy nobody asked for.

`parent_id` forms a forest. A cycle check runs on every write that sets it: walk to the
root and refuse if the tag is met again. Deleting a parent promotes its children to roots
(`ON DELETE SET NULL`) rather than cascading — deleting "News" should not silently delete
everything filed under it.

### `feeds`

```sql
CREATE TABLE feeds (
  id              TEXT    PRIMARY KEY,                      -- f_…
  url             TEXT    NOT NULL,                         -- as entered
  canonical_url   TEXT    NOT NULL UNIQUE,                  -- normalised; the dedup key
  title           TEXT    NOT NULL DEFAULT '',
  site_url        TEXT    NOT NULL DEFAULT '',
  etag            TEXT    NOT NULL DEFAULT '',
  last_modified   TEXT    NOT NULL DEFAULT '',
  last_fetch_at   INTEGER,
  last_success_at INTEGER,
  last_status     INTEGER,                                  -- HTTP status, or 0 for a transport failure
  last_error      TEXT    NOT NULL DEFAULT '',
  failure_count   INTEGER NOT NULL DEFAULT 0,
  next_fetch_at   INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  last_error_body TEXT    NOT NULL DEFAULT '',              -- what the server actually said
  fetch_interval  INTEGER NOT NULL DEFAULT 0                -- worked out from what it publishes
);
CREATE INDEX feeds_due ON feeds(next_fetch_at);
```

**Feeds are global, not per user.** Two people following the same URL cause one fetch,
which matters to the publisher more than to us and makes the poller's work proportional
to distinct URLs rather than to subscriptions.

`canonical_url` is the dedup key: scheme and host lowercased, default port dropped,
fragment dropped, trailing slash normalised. `url` keeps what was typed, so an error
message can quote it back.

`etag` and `last_modified` are echoed as `If-None-Match` / `If-Modified-Since`. A `304`
costs one round trip and no parsing.

`failure_count` drives exponential backoff capped at six hours, written into
`next_fetch_at`. A dead feed is retried occasionally rather than every cycle forever.
`last_error` is what the manage page shows when a feed goes quiet — "it broke" without
"how" sends somebody to the logs, and `last_error_body` is the half of it anybody can act on:
"the server answered 503" is a fact, and the rate-limit note underneath it is the reason.

`fetch_interval` is how often this feed is worth asking, worked out from the median gap
between its recent articles and held between half an hour and a week. It is stored rather than
recomputed because most fetches carry nothing to compute it from: a `304` is the commonest
answer once a feed has been followed for a day, and the interval from the last fetch that did
bring articles is the right one to keep using. Zero means nothing has worked it out yet, and
the caller reads that as a day. See `feeds.Cadence`.

There is no operator setting for it, and there was: nobody configuring a reader knows how
often each publisher they follow puts something out. Measured against nineteen real feeds, one
number meant a comic published every three weeks was asked for three hundred and thirty-six
times between articles.

### `subscriptions`

```sql
CREATE TABLE subscriptions (
  id             TEXT    PRIMARY KEY,                       -- s_…
  principal_id   TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  feed_id        TEXT    NOT NULL REFERENCES feeds(id)      ON DELETE CASCADE,
  title_override TEXT    NOT NULL DEFAULT '',
  priority       INTEGER NOT NULL DEFAULT 50 CHECK (priority BETWEEN 0 AND 100),
  max_article_age INTEGER NOT NULL DEFAULT 604800
                     CHECK (max_article_age IN (0, 86400, 604800, 1209600, 2592000, 31536000)),
  created_at     INTEGER NOT NULL,
  UNIQUE (principal_id, feed_id)
);
CREATE INDEX subscriptions_feed ON subscriptions(feed_id);
```

Everything a person *chose* about a feed lives here; everything the fetcher *learned*
lives on `feeds`.

`max_article_age` is how far back a page reaches **into this feed** — seconds, or 0 for no
limit. It began as one setting per person and moved here, because that was the wrong shape:
a news feed worth a day and a blog worth a year are exactly the pair one number cannot
serve. The move is `20260823103614_main_article_window_per_feed`, which carries each
person's old setting onto every feed they follow, so nobody's pages changed the day it
landed.

A feed with no subscriptions left is deleted by the poller's sweep, and its items go with
it.

### `subscription_tags`

```sql
CREATE TABLE subscription_tags (
  subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  tag_id          TEXT NOT NULL REFERENCES tags(id)          ON DELETE CASCADE,
  PRIMARY KEY (subscription_id, tag_id)
) WITHOUT ROWID;
CREATE INDEX subscription_tags_tag ON subscription_tags(tag_id);
```

A subscription may carry several tags, or none, and selection does not care either way: tags
decide whether a feed may appear on a page, and its own priority decides how much of that page
it gets — see [edition.md](edition.md#in-plain-terms).

Reads come back **ordered the way `ListTags` orders them**, not as the join happened to
return them. Unordered, the summary under a feed said "Tech · Design" while the dialog
showed the same two as "Design, Tech" — the same answer twice, in two different orders,
which reads as two different answers. Ordering it here rather than in each caller means
the exported OPML agrees too.

### `pages`

```sql
CREATE TABLE pages (
  id               TEXT    NOT NULL PRIMARY KEY,            -- pg_…
  principal_id     TEXT    NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  name             TEXT    NOT NULL,
  slug             TEXT    NOT NULL,                        -- empty for the Front Page
  is_main          INTEGER NOT NULL DEFAULT 0 CHECK (is_main IN (0, 1)),
  edition_interval INTEGER NOT NULL DEFAULT 86400
                     CHECK (edition_interval IN (3600, 21600, 86400, 604800)),
  edition_size     INTEGER NOT NULL DEFAULT 60
                     CHECK (edition_size BETWEEN 10 AND 200),
  next_edition_at  INTEGER NOT NULL,
  max_article_age  INTEGER NOT NULL DEFAULT 0
                     CHECK (max_article_age IN (0, 86400, 604800, 1209600, 2592000, 31536000)),
  created_at       INTEGER NOT NULL,
  publish_slug     TEXT    NOT NULL DEFAULT '',            -- the address, kept after a take-down
  published        INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
  indexable        INTEGER NOT NULL DEFAULT 0 CHECK (indexable IN (0, 1)),
  UNIQUE (principal_id, slug)
) STRICT;
CREATE UNIQUE INDEX pages_main ON pages(principal_id) WHERE is_main = 1;
CREATE INDEX pages_principal ON pages(principal_id, created_at);
CREATE INDEX pages_due ON pages(next_edition_at);
CREATE UNIQUE INDEX pages_publish_slug ON pages(principal_id, publish_slug) WHERE publish_slug <> '';
CREATE INDEX pages_published ON pages(published) WHERE published = 1;
```

This was `settings`, keyed by principal, back when a person had exactly one front page. Its
entire contents were facts about a page, so it was not joined by a `pages` table beside it —
it became one.

A row is created with the principal, so the scheduler never has to cope with its absence, and
the partial unique index says every person has exactly one Front Page. The interval is a closed
set — hourly, six-hourly, daily, weekly — because an arbitrary cron expression is a support
burden with no matching demand.

`max_article_age` sits over the top of each feed's own window and the tighter of the two wins.
The feed's window says how long that publisher stays worth reading; a page's says how current
that page is meant to be.

There were two filter *mode* columns here — `tag_filter` and `feed_filter`, each of them
`no`/`including`/`excluding`. They are gone. A page no longer has a mode: it has an opinion
per tag and per feed, held in the two tables below, and "including" and "excluding" are things
a page does at the same time rather than a state it is in. See `page_tags`.

**Publishing** is the last three columns, and they are three because they answer three
questions that fail differently.

`publish_slug` is the address, and it is kept when a page is taken down rather than cleared —
so publishing it again offers the address the existing links already point at. Unique per
person and partial, so every unpublished page's empty slug does not collide.

`published` is whether it is served. Taking a page down is a switch and leaves everything else
alone.

`indexable` is whether it may be crawled, and it is **not** the answer on its own: the instance
has to agree, and `PublishedPage` applies the instance's ceiling on the way out. Two yeses
because the two are not symmetrical — taking a page down is a switch, and taking it out of
somebody else's search index is a request nobody controls.

### `page_tags`, `page_feeds`

```sql
CREATE TABLE page_tags (
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  tag_id  TEXT NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
  mode    TEXT NOT NULL CHECK (mode IN ('include', 'exclude')),
  PRIMARY KEY (page_id, tag_id)
) WITHOUT ROWID;

CREATE TABLE page_feeds (
  page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  feed_id TEXT NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
  mode    TEXT NOT NULL CHECK (mode IN ('include', 'exclude')),
  PRIMARY KEY (page_id, feed_id)
) WITHOUT ROWID;
```

What the filter reads. **No row means the page has no opinion** about that tag or that feed,
which is where everything starts — so there is no mode column on the page any more, and no
question of what an empty list means.

The primary key is the invariant: one row per tag per page, so a tag cannot be on both sides.
Drawing from a tag and dropping it is not a filter with an unlucky answer, it is a
contradiction, and a key that cannot hold one is better than a rule every caller has to
remember. The interface says the same thing in its own way — one list, each name on a switch
with three positions, so there is no second list to disagree with.

**Tags are a funnel and feeds overrule it**, and the asymmetry is the point.

Include any tag and the page draws only from tags marked include; exclude a tag and it drops
what carries that tag *afterwards*. The order is the whole reason there are two directions
rather than a choice between them: tags overlap, so a finance page that wants rid of the crypto
half of itself takes Finance and then drops Crypto, and neither gesture alone says it.

A feed's row is not a narrower tag rule — it beats the tags outright. Include puts the feed on
the page whatever the tags decided; exclude keeps it off whatever they decided. Otherwise the
two useful things anybody wants to say about one publisher, "this one as well" and "this one
never", would depend on working out a tag rule first.

### `instance_settings`

```sql
CREATE TABLE instance_settings (
  id              TEXT    NOT NULL PRIMARY KEY,
  singleton       INTEGER NOT NULL UNIQUE DEFAULT 1 CHECK (singleton = 1),
  public_pages    INTEGER NOT NULL DEFAULT 0 CHECK (public_pages IN (0, 1)),
  public_indexing INTEGER NOT NULL DEFAULT 0 CHECK (public_indexing IN (0, 1)),
  landing         INTEGER NOT NULL DEFAULT 1 CHECK (landing IN (0, 1)),
  updated_at      INTEGER NOT NULL
) STRICT;
```

The answers that belong to the instance rather than to anybody on it. `singleton` is the same
one-row trick `smtp_config` uses: a unique column that can only hold `1`, so a second row is a
constraint violation rather than a question about which row wins.

**Both start off**, and that is a decision rather than caution. An instance that serves nothing
to strangers should not begin serving to strangers because it was upgraded — somebody has to
decide, and the safe answer is the one that happens when nobody does.

`landing` is the only column here that starts as yes, and the asymmetry is deliberate. The
other two are exposure — who may put a page on the open web, and whether a search engine may
keep it — where a default of yes decides something on somebody's behalf. This one decides what
`/` *says* to a visitor with no session: the page explaining what this is, or the sign-in form
on its own. A front door that explains itself is the better default, and turning it off is the
choice. A missing row therefore reads as `InstanceSettings{Landing: true}` rather than the zero
value; see `Instance` in store/publish.go.

The server caches it, because it decides which HTML `/` answers with and the alternative is a
query on the front door for every visitor without a cookie. `putInstance` is the only writer
and invalidates it there — see [backend.md](backend.md#serving-the-spa).

Turning `public_pages` off takes every published page down rather than only stopping new ones.
It is the instance's answer, not a default for pages to inherit.

`public_indexing` is a ceiling over each page's `indexable`, applied on read. Where the
instance says no, the interface does not offer the choice at all — a control that exists and
refuses is worse than one that is not there.

---

## derived.db

Everything here is reconstructible from `main.db` plus one fetch cycle. Nothing here is
worth a backup, and the ability to say that is what keeps read marks where they are.

### `items`

```sql
CREATE TABLE items (
  id           TEXT    PRIMARY KEY,                         -- a_…
  feed_id      TEXT    NOT NULL,                            -- main.db feeds.id; no FK across databases
  guid         TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  link         TEXT    NOT NULL,
  author       TEXT    NOT NULL DEFAULT '',
  summary      TEXT    NOT NULL DEFAULT '',                 -- sanitized HTML
  image_url    TEXT    NOT NULL DEFAULT '',
  published_at INTEGER NOT NULL,
  fetched_at   INTEGER NOT NULL,
  image_width  INTEGER NOT NULL DEFAULT 0,                  -- 0 until something measures it
  image_height INTEGER NOT NULL DEFAULT 0,
  image_retry_at INTEGER NOT NULL DEFAULT 0,                -- when it is worth asking again
  image_error  TEXT    NOT NULL DEFAULT '',                 -- why the last attempt failed
  UNIQUE (feed_id, guid)
);
CREATE INDEX items_feed_published ON items(feed_id, published_at DESC);
CREATE INDEX items_fetched        ON items(fetched_at);
CREATE INDEX items_feed_link      ON items(feed_id, link);
```

Identity is the feed's `guid`, falling back to the item link, falling back to a hash of
title and published date. The unique constraint is what makes re-fetching idempotent: a
feed that republishes its whole window every hour produces no duplicates.

`summary` is **sanitized on the way in**, not on the way out. An allowlist over tags and
attributes, all script and style dropped, relative URLs resolved against the feed's base.
Storing the safe form means every reader of this table gets it by construction, and a bug
in a renderer cannot become an injection.

There is no `content` column. This is a front page that links out, not a reading app —
see [Open questions](#open-questions).

`published_at` falls back to `fetched_at` when the feed omits a date, because a null date
would need handling at every point that orders by it.

Pruned **per feed**, by `fetched_at`, sparing anything a live edition references — *any*
reader's, not the one whose settings were consulted. An article is stored once and shared by
everyone who follows the feed, so a page holding it holds it for everybody; the guard is
`id NOT IN (SELECT item_id FROM edition_items)`, unscoped by person on purpose, and it is on
every one of the four statements that delete an item.

What makes that mean *current* editions rather than every edition that ever existed is the
sweep's order: `PruneOldEditions` runs first, its `edition_items` rows cascade away with it,
and what is left is exactly what each page is showing now. If it ever fails, the sweep logs
and carries on — and the failure keeps too much rather than too little, which is the direction
to fail in.

How long a feed is kept follows the people who follow *that feed* — the longest window any of
them chose, floored at 30 days. One number for the whole instance was one number too few: it
took the longest window chosen anywhere, so a webcomic somebody wanted a year of made a news
feed at ninety articles a day keep a year as well, tens of thousands of articles nobody had
asked for to serve a page that shows sixty. The windows are per subscription precisely because
a news feed worth a day and a blog worth a year are the pair one number cannot serve, and the
pruning has to agree with that or the setting is only half real. See `ItemRetentionByFeed`.

**"No limit" means no limit** — there is no ceiling in years. There was one, and it was wrong
for a reason worth writing down: how far back a page reaches bounds when an article was
*published*, and pruning goes by when it was *fetched*. A feed whose every article was written
two years ago — an archive, a comic's back catalogue, a blog that stopped — would have had its
articles dropped a year after they were first seen, and if the publisher had moved them out of
the document by then they would never have come back.

What bounds such a feed instead is `MaxItemsPerFeed`, a shelf length rather than a date: **1,000
articles, newest by publication**, whatever the window says. That is a judgement about reading
rather than about storage — a front page is about what is going on, and the thousandth most
recent thing one publisher has said is not something anybody is going to get to. A feed that put
out a thousand articles yesterday has nothing to offer past them however far back somebody asked
to reach.

Two bounds, and the tighter one wins. For a feed quiet enough for a thousand to span its window,
age decides and the 30-day floor holds. For a busy one — ninety articles a day is about eleven
days' worth — the ceiling decides instead, and the sweep names any feed it takes from, because a
window shortened by something other than the setting somebody chose should not be something they
have to discover.

Nothing prunes an article for having fallen out of the feed document. Feeds publish a rolling
window of ten to fifty items, so "no longer in the feed" is true of almost everything within
days; age is the lever that means anything.

`image_width`/`image_height` are the picture's real size and zero means nothing has measured
it, which is the ordinary case for anything published in the last few minutes. The page falls
back to a drawn shape, so zero is not a missing value anything downstream has to work around.

`image_retry_at` and `image_error` replaced a boolean `image_probed`, and the difference is
described under [Measuring a picture](#measuring-a-picture). `items_feed_link` supports
recognising an article whose guid moved by the link it kept.

### `editions`

```sql
CREATE TABLE editions (
  id           TEXT    PRIMARY KEY,                         -- e_…
  principal_id TEXT    NOT NULL,
  page_id      TEXT    NOT NULL DEFAULT '',
  generated_at INTEGER NOT NULL,
  seed         INTEGER NOT NULL,
  size         INTEGER NOT NULL
);
CREATE INDEX editions_principal ON editions(principal_id);
CREATE INDEX editions_page      ON editions(page_id, generated_at DESC);
```

This used to carry a unique index on `principal_id`, which made "exactly one live edition per
person" structural. A person has several pages now, so the invariant is one live edition **per
page** and it is no longer expressible as an index — the newest row per `page_id` is the live
one, and a `currentEditions` CTE says so once for the three questions that need it: which
edition a page shows, which editions an article should be marked read across, and which still
count as displaying an article. A second spelling of "the live one" that drifted from this
would show up as read marks landing on a page nobody is looking at.

Ordered by `generated_at DESC, rowid DESC`, because `generated_at` is Unix seconds and two
compositions inside one second tie — which is not the curiosity it looks like, since a
regeneration immediately after a scheduled turn is exactly that.

`seed` makes a generation reproducible when something looks wrong. It is also, separately, what
the *client* seeds every card's appearance from — the edition's id rather than this column; see
[frontend.md](frontend.md). Handing a published page a different seed gave a stranger the same
articles with none of the same faces, which is worth knowing before anybody changes what the
public endpoint sends.

### `edition_items`

```sql
CREATE TABLE edition_items (
  edition_id TEXT    NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
  item_id    TEXT    NOT NULL REFERENCES items(id)    ON DELETE CASCADE,
  rank       INTEGER NOT NULL,
  slot       TEXT    NOT NULL CHECK (slot IN ('lead','wide','feature','standard','brief')),
  PRIMARY KEY (edition_id, item_id)
) WITHOUT ROWID;
CREATE UNIQUE INDEX edition_items_rank ON edition_items(edition_id, rank);
CREATE INDEX edition_items_item ON edition_items(item_id);
```

**There was a `read_at` here, and it was wrong.** The note in the initial schema said why it
was right — "a read mark belongs to the edition it was made in, so when the edition goes the
mark goes with it" — and that was true until `read_articles` arrived. From the moment composing
a page began copying the mark forward out of that table so an article shown again did not come
back looking new, this column was a cache of a fact with a different home, and every write had
to remember to keep the two in step. Marking a whole feed read wrote both. Unmarking wrote
both. Reading one article wrote both — and could only do so for an article on one of *your*
pages, which is why marking something read on a page somebody else published had no way to work
at all.

So: one home. This table says what is on the page. Whether somebody has read it is a fact about
them and the article, joined when the page is read, and joined against **whoever is looking** —
which is what lets a visitor see their own reading on a page they did not compose.

`rank` is the generation order; `slot` is the layout weight. Both are decided server-side and
stored, so the page is identical on every reload until the next edition — see
[edition.md](edition.md#layout) for what decides the slot, which now includes what the
picture is shaped like.

### `shown`

```sql
CREATE TABLE shown (
  page_id   TEXT    NOT NULL,
  feed_id   TEXT    NOT NULL,
  guid_hash BLOB    NOT NULL,                               -- sha256(guid), first 16 bytes
  shown_at  INTEGER NOT NULL,
  PRIMARY KEY (page_id, feed_id, guid_hash)
) WITHOUT ROWID;
CREATE INDEX shown_age ON shown(shown_at);
```

The one thing that outlives an edition, and deliberately tiny — a truncated hash, not a
row of content. Without it the same article resurfaces every cycle and a front page
stops feeling like one. Pruned at 90 days, which is three times the item retention, so a
hash always outlives the item it refers to.

Not pruned at all while any feed is unlimited. A hash that stops existing while the article it
names is still on the shelf is an article that comes back round as though it were new, which is
the one thing this table is for. It stays bounded anyway, because what it refers to is —
see `MaxItemsPerFeed`.

**Per page, not per person**, which is the difference between a front page being a view and
being a share. It was per person while a person had one page, and that was right then and wrong
the moment they had two: an art story belongs on a page of everything *and* on a page of art,
and with one record between them whichever page composed first took it and the others could
never see it. Measured on real feeds — a page of everything composed after a filtered one held
zero of the filtered page's articles, not a few.

The cost is rows: the same article is recorded once per page that shows it. A few thousand short
rows per page is not a cost worth designing around.

Whether something has been **read** is the opposite: that is a fact about a person and an
article, so it is shared across every page — see `read_articles`, and the read marks on
`edition_items`, which are written across all of a person's live editions at once.

### `read_articles`

```sql
CREATE TABLE read_articles (
  principal_id TEXT    NOT NULL,
  item_id      TEXT    NOT NULL,
  feed_id      TEXT    NOT NULL,
  title        TEXT    NOT NULL,
  link         TEXT    NOT NULL,
  published_at INTEGER NOT NULL,
  read_at      INTEGER NOT NULL,
  PRIMARY KEY (principal_id, item_id)
) WITHOUT ROWID;
CREATE INDEX read_articles_when ON read_articles(principal_id, read_at DESC);
CREATE INDEX read_articles_feed ON read_articles(principal_id, feed_id);
```

What somebody actually read, kept for as long as they follow the feed.

**Nothing expires it.** It was a month, which was right while its only job was a list to look
back at. It has a second job now: an article somebody has read is never offered to any of their
pages as *new* again — it falls to the last band, behind everything unread — so this is what
stops a story coming back a year later as though it were fresh. A month-long memory forgets, and
forgetting is the one thing it must not do.

What ends it is unfollowing the feed, and `DeleteSubscription` does that in the same call. The
sweep is the safety net for the two ways that can be missed: the delete crosses the two
databases and so cannot share a transaction with the unsubscribe, and a feed the last follower
drops is collected wholesale rather than one subscription at a time.

The list is bounded instead of the record — `ReadListLimit`, five hundred. A reader getting
through fifty a day has eighteen thousand rows after a year, and a screen that renders all of
them is a screen that stops opening.

**This is the only home a read mark has.** There was a second — `edition_items.read_at` — and
the two were kept in step by every write until the column was dropped; see
[`edition_items`](#edition_items) for why that had to end. A card is greyed by joining this
table against whoever is looking, so the same page shows one visitor's reading to them and
nobody else's, and marking something read on a page you did not compose works for the same
reason it works on your own.

It is not an unread count in disguise. It counts nothing and lists only what has already been
dealt with. A list of things you have finished with asks nothing of you, which is the whole
reason it is allowed to exist here.

Denormalised on purpose: the row outlives the item (pruned at 30 days) and the
subscription, which somebody may drop the day after reading something. It is also what lets a
read mark survive the page it was made on, which is now the only thing carrying it. The feed *title* is
not stored because it lives in the other database — `feed_id` is kept so the interface can
name the source while the subscription still exists, and an article read and then
unsubscribed from keeps its own title and loses its source, which is the honest rendering.

---

## Migrations

**One Go file per change**, in `internal/store/migrations/`, named
`<timestamp>_<database>_<snake_case_name>.go`. The timestamp orders them and stops two
people adding a migration on the same day from colliding; the database name says which of
the two it belongs to.

```
internal/store/migrations/
  migrations.go                                    the two lists, and the types
  20260823030000_main_initial_schema.go
  20260823030000_derived_initial_schema.go
  20260823051000_derived_read_articles.go
  20260823061500_main_article_window.go
```

### Why Go and not `.sql`

A schema change is usually SQL and nothing else. The ones that are not are the reason:

```go
type Context struct {
	Ctx   context.Context
	Tx    *sql.Tx  // this database, inside the transaction that stamps the version
	Other *sql.DB  // the other one
}
```

Moving data from one of the two databases to the other cannot be expressed in a file handed
to one connection. A migration gets its own transaction **and** a handle on the other
database, so a change that has to carry rows across can.

Nothing done through `Other` is atomic with `Tx`, and it cannot be — SQLite's
multi-database commit does not hold under WAL, which is why these are two connections and
never `ATTACH`ed. So **read from `Other`, write into `Tx`**: a failure then leaves the
source intact and the destination rolled back, which is the recoverable direction.

There is no `Down`. Downgrading is unsupported, and a rollback nobody has ever run is a
rollback that does not work.

### Applying

Progress is `PRAGMA user_version` — the count applied. The runner sorts by name, so a file
added out of order still applies in the right place; the order in `migrations.go` is
documentation. Each runs in its own transaction that also stamps the version, so a failure
leaves the database at the last version that fully applied rather than half way through
one. A `user_version` ahead of the build is a startup error rather than a guess.

### Adding one

1. Write `internal/store/migrations/<timestamp>_<main|derived>_<name>.go`, stamped with
   **the actual time you write it** — `date -u +%Y%m%d%H%M%S`. The timestamp is a record of
   when a change was made, not a sort key to be invented.
2. Add it to `Main` or `Derived` in `migrations.go`.
3. Run `go test ./internal/store/...`. `TestMigrationsAreAppendOnly` fails and prints the
   line to add.
4. Paste it into `released` in `migrations_test.go`.

That last step looks like bureaucracy and is the whole point. **Never edit a released
migration** — every deployment past it has already recorded it as applied and will skip the
change forever, so the schema in front of the code differs from the schema in the file on
those databases and no others. The hash makes that mistake fail the build instead of
failing in production a month later.

The hash is over the **whole file**, so a comment is as protected as the SQL. Deliberate:
the comment above a migration is usually the only record of why it exists, and quietly
rewriting it is its own kind of losing the history.

### What is actually tested

- `TestMigrationsAreAppendOnly` — a released migration cannot be edited or removed.
- `TestEveryMigrationHasItsOwnFile` — the name is a sortable timestamp plus a readable name,
  the file exists, and the migration does something.
- `TestTimestampsAreUnique` — two migrations sharing a timestamp have no defined order
  between them, which is a difference that would only show up on somebody else's database.
- `TestMigrationsAreInTheListTheyName` — a migration named `_main_` is not in `Derived`.
- `TestEveryEarlierVersionUpgrades` — every version that has ever shipped reaches the
  current one. A migration that only works against a fresh database is not a migration.
- `TestUpgradingKeepsWhatWasThere` — an upgrade does not cost anybody their data.
- `TestAFailedMigrationLeavesTheVersionBehind` — a half-applied migration rolls back whole,
  version included.

`serve` logs `main_schema` and `derived_schema` at startup, because "did my deployment
actually migrate?" is otherwise answered by shelling into a container with sqlite3 — and the
moment somebody wants that answer is the moment something has already gone wrong.

## An article's id is derived, not minted

Every other entity gets a fresh time-sortable id when it is created. An article does not: its
id is a digest of its feed id and its guid, so the same article carries the same id on every
fetch, on every instance, and after being pruned and coming back.

It was random, and that was survivable but wrong in one specific way. `INSERT OR IGNORE`
already kept the stored id stable across a re-fetch — the freshly minted one was simply
thrown away — but retention deletes articles at thirty days, and a feed with a long window
still lists some of them. When one came back it came back as a stranger, and everything keyed
by the id — what somebody has read, what they have been shown — reset. Deriving the id closes
that by construction rather than by remembering to.

**The store names articles, not the parser.** `Parse` is also called during discovery, before
there is a feed row to name them after, so naming them there gave every feed's articles the
same ids and the primary key dropped the second feed's copies one at a time. `SaveItems`
assigns the id, where the feed id is real, and writes it back onto the item so a caller sees
what actually went into the table.

A renamed article keeps the id it was first given rather than the one its new guid would
derive — see below. That is not a contradiction: the id is an opaque handle, and nothing
computes one to find a row. It is derived so that it is *stable*, not so that it is a lookup
key.

These do not sort by time, unlike every other id here. Nothing orders articles by id —
they are ordered by publication, which is the order that means anything about an article —
but `ids.Derive` says so at its definition for whoever reaches for it next.

## An article whose guid moved is still the same article

An article's identity is `(feed_id, guid)`, and plenty of publishers cannot keep a guid
still. theblueprint.ru writes the permalink with the publication time appended inside the
guid, so editing an article changes the one field whose whole job is to stay the same. Every
edit arrived as a new article: the same story on the page twice, old headline beside new, and
the second copy unread because nothing had ever shown *it*.

So ingest checks the other identifier a feed carries. When a guid is one we have never seen
but its link belongs to an article we already have, that row is **updated in place** rather
than a second row inserted. The id staying is the part that matters — being on somebody's
page survives, and so does having been read.

Three things follow the rename: the `shown` record, which is keyed by a hash of the guid and
would otherwise hand the article back to the sampler as brand new; the headline in
`read_articles`, so the same story does not read one way on the page and another in the list
of what has been read; and nothing else. `published_at` deliberately stays where it was — an
edit is not a republication, and letting the date follow would shuffle an article back up the
page every time somebody fixed a comma.

The guard is that the link appeared exactly once in the fetch. Feeds whose items all point at
one page are real, and matching those on their link would fold a whole feed into one article
— losing articles to prevent duplicates is the wrong way round. `items_feed_link` is a plain
index for the same reason: a unique one would make that failure structural.

It cannot rescue an article whose guid *and* link both moved. A publisher who rewrites slugs
is out of reach without guessing from titles, and guessing merges stories that are merely
similar. Two identifiers is what a feed gives, and using both is as far as this goes.

## Background work is a queue in main.db

`jobs` holds work that is nobody's request: a kind, an opaque JSON payload, a unique key, an
attempt count, a `run_at`, and a `label` — a few words naming the subject, so a log line reads
`job ended kind=feed.fetch label="The Meridian"` rather than an id.

`internal/jobs` registers a `Work` per kind: a handler, an optional refill that queues what is
due, and a `Policy` — how often to sweep, how many at a time, how many at once, how many
attempts, and the backoff. **Everything background is a kind now.** Measuring pictures was the
first, and fetching feeds was moved in after it, which is what makes `job started` / `job
ended` / `job failed` the one voice all background work speaks in. Feeds are registered with
`MaxAttempts: 1` on purpose: a failed fetch backs the feed off in the `feeds` table and comes
round again as ordinary due work, and retrying the job as well would be two clocks disagreeing
about one feed.

In main.db rather than derived.db. derived is the disposable half and is rebuilt from the
feeds; a queue is not rebuildable from anything, and work lost because a cache was rebuilt is
the failure a queue exists to prevent. A job whose subject has since been pruned is one its
handler drops.

**The rate limit is per kind, in its `Policy`.** Pictures are one job every three seconds, one
at a time, and that is the whole of it. A handful per pass would be less total traffic and
worse behaviour: work arrives in bursts — a feed with thirty new articles is thirty pictures,
all on one host — and a pass that ran eight of them would make eight requests to one server
inside a second. Bursts are what gets rate-limited.

**Two paths put work in, and the runner only ever takes it out.** A fetch that brought
articles in queues their pictures — that is the moment work appears, and asking then rather
than on a timer is what keeps an idle instance idle. The hourly sweep queues anything the first
path cannot know about: articles that predate the feature, a queue lost to a restore, a hook
that failed. Enqueueing is idempotent on the URL, so both paths asking about one picture is one
row.

An earlier version had the runner look for work whenever its queue came up empty, which meant
an instance with nothing to do ran that query every three seconds for the life of the process.

A ticker, not a chain that reschedules itself after each result. The spacing is identical, but
a chain has no owner: lose the process between finishing a job and queueing the next and the
work stops with nothing to notice.

### Measuring a picture

`image.DecodeConfig` reads a file's header and stops — dimensions live in the first bytes, not
spread through the image. With a `Range` request and a 64KB ceiling, measuring a 3.2MB picture
read **16KB of it**, which the test asserts rather than assumes.

**A failure is postponed, not settled.** This was `image_probed`, a boolean set on any failure
and never cleared, and the reasoning for it read well: a picture nothing could measure costs
nothing, because the page draws a shape for it, so no outcome is worth a second request to
somebody else's server.

It was wrong, and the way it was wrong is worth keeping. One bad minute at a CDN cost that
picture its size **for ever** — and CDNs have bad minutes. On a copy of a real instance,
fifteen of the nineteen pictures on a comics page were stuck that way, none of them for a
reason that was still true. "Never ask again" is the right answer to a settled no and the wrong
answer to a timeout, and a boolean cannot tell them apart.

So `image_retry_at` says when it is worth asking again, and `image_error` says why the last
attempt failed:

| reason | wait | what it is |
| --- | --- | --- |
| `gone` | a day | 404, 410 — a settled answer |
| `refused` | a day | 401, 403 — hotlink protection, and it will not change on its own |
| `busy` | an hour | 429 and 5xx, honouring `Retry-After` when the server sends one |
| `unreachable` | an hour | nothing answered: DNS, a refused connection, a timeout |
| `undecodable` | a day | it arrived and was not an image this build can read |
| `empty` | a day | no bytes |

`Retry-After` is parsed both ways it is written — a number of seconds or an HTTP date — and
clamped to between a minute and a day, because a server asking to be left alone for a month is
not a thing to obey literally.

Recording the reason is what makes a later build able to re-ask only what it newly understands:
WebP was `undecodable` until a decoder was registered, and `RetryImages(reason)` is one query.
Admin → Images shows the tally and offers exactly that, per reason.

The measured ratio is used as it is, with two exceptions and no clamping. A picture more than
two and a half times taller than wide is drawn square and filled, because a sliver cropped to
fit a column is not a picture of anything. And every picture, measured or not, is bounded at
70vh. There **was** a clamp — measured ratios were held to 5:3..1:1, the range of the drawn
ladder — and it meant a panorama, the one shape whose entire point is its width, was the shape
cropped hardest. See [edition.md](edition.md#layout).

## Open questions

- **`shown` is keyed per feed, not per subscription.** Identical until somebody
  unsubscribes and resubscribes, at which point the question is whether that should bring
  the back catalogue with it. Currently: no.
- **No `content` column.** If the reader ever grows an inline view, this is where it goes,
  and item retention becomes a storage question rather than a bookkeeping one.
- **`edition_size` default of 60** was a guess and has been checked: measured against a real
  instance — nineteen feeds publishing about eighty-five articles a day, pages moved to 90 and
  to 30, both editions filling to their ceiling — so 60 is deliberately shy of what a heavy
  reader wants. Too small is a slider somebody moves once; too large is a page that looks like
  a feed reader. Unchanged, and no longer a guess.
- **Slots are frozen at compose time**, and a picture's measured shape is now one of the
  things deciding them — so a page composed before its pictures were measured keeps the slots
  it chose, and only a recomposition fixes it. Whether layout should move to the client instead
  is open; see [edition.md](edition.md#open-questions).
