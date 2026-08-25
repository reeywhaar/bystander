# API design

## Shape

**JSON in, JSON out, under `/api`.** No version segment: the frontend ships inside the
same image as the backend, so there is no third-party client whose compatibility a
version would protect. If an external client ever appears, that is when `/api/v1` earns
its place — and adding it then is a routing change, not a migration.

**Field names are `snake_case`**, matching the Go struct tags and the SQL columns
underneath them, so a field is the same string from the column to the browser. Nothing in
the path renames anything, and a grep for `edition_interval` finds every mention of it.

**Timestamps are Unix seconds**, always named `*_at`. Rendering in a reader's zone is the
browser's job and happens nowhere else.

**Ids are opaque strings** with a type prefix. Never parsed by the client.

**No pagination.** An edition is bounded by `edition_size`, and nobody has thousands of
feeds. A cursor over `id` is the answer if that stops being true — the ids sort
chronologically, so it is available whenever it is needed.

## Refusals are honest

This service serves a login page at `/`. It announces what it is by existing, so there is
nothing to disguise and no reason to collapse every refusal into one padded 404 the way a
service that must pass for something else would.

| situation | status |
| --- | --- |
| Bad input, unparseable body, failed validation | `400` |
| No session, or an expired one | `401` |
| A session that is not allowed this | `403` |
| Unknown id | `404` |
| Known path, wrong method | `405` |
| Duplicate username, duplicate subscription | `409` |
| Mutating request whose body is not `application/json` | `415` |
| Rate limited | `429` |

Errors are `{"error": "a sentence"}`. The sentence is written for the person who will
read it in the interface, not for a log grep — which is why `internal/store` builds its
classified errors through `store.NotFound`, `store.Conflict` and `store.Invalid` rather
than `fmt.Errorf("%w: …", ErrNotFound)`. The latter renders as `not found: no tag t_1`
wherever it is shown, and these are shown.

## Authentication

A session cookie, `bystander_auth`: `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure`
whenever `BYSTANDER_PUBLIC_URL` is `https`. There is no bearer token, no API key and no
header-based auth of any kind.

Sliding expiry of one week since last use, with the refresh throttled to once an hour.
See `sessions` in [entities.md](entities.md).

An unauthenticated request to `/api/*` gets `401` and a JSON body. The island redirects to
`/login`; the server never issues a redirect for an API call, because a `302` to an HTML
page is the least useful thing a `fetch` can receive.

### CSRF

Three parts, all cheap:

1. `SameSite=Lax` on the cookie.
2. A mutating request carrying `Sec-Fetch-Site: cross-site` is refused with `403`.
3. A mutating request with a body must declare `application/json`, or `415`. Checked only
   when a body is actually present — a `DELETE` legitimately carries none.

### There is no CORS middleware

Its absence is load-bearing, not an oversight. The browser only ever talks to this origin;
adding `Access-Control-Allow-Origin` would weaken two of the three defences above. A test
asserts the header is never emitted.

### Rate limits

Login is limited globally and per principal. Bcrypt at cost 12 on an unauthenticated
endpoint is a CPU exhaustion vector before it is an authentication one.

`POST /api/feeds` is limited per principal: it makes an outbound request on the caller's
behalf, and an endpoint that fetches an arbitrary URL for whoever asks needs a ceiling.

## Endpoints

### Public

```
GET    /healthz                          {"ok": true, "version": "…"}

POST   /api/login                        {username, password} → 204 + Set-Cookie
GET    /api/invites/{token}              {role, expires_at, invited_by} — validity only
POST   /api/invites/{token}/accept       {username, password} → 204 + Set-Cookie
```

`GET /api/invites/{token}` tells the acceptance page whether the link is live before
somebody types a password into it. It reveals nothing but its own validity.

### Session

```
POST   /api/logout                       → 204
GET    /api/me                           {id, username, role, created_at}
```

### The reader

```
GET    /api/edition?page=                → the live edition of one front page
POST   /api/edition/regenerate?page=     → the new edition
PUT    /api/edition/items/{id}/read      → 204
DELETE /api/edition/items/{id}/read      → 204
```

`?page=` names one of the caller's front pages, by slug or by id. **Absent is the Front Page**,
which is also what the empty slug resolves to — so the reader at `/` asks for nothing in
particular and gets the right thing, and nothing had to learn about pages to keep working.

Marking read is not addressed by page, and that is the point: reading is a fact about a person
and an article, so it lands on every one of that person's live editions at once. The same
article greyed on one tab is greyed on the next.

`GET /api/edition` answers `200` with an empty `items` array before the first page is
generated. Not a `404`: "your page has not been made yet" is a state the reader renders,
and a `404` on the one endpoint the front page calls would send the interface into its
failure path on a new account's very first visit.

`POST /api/edition/regenerate` is a **re-roll**: it returns the current page's unread
articles to the pool before composing, so it can be pressed repeatedly while somebody
settles on their priorities. Only the scheduled turn spends what it shows. See
[edition.md](edition.md#a-manual-regeneration-is-a-re-roll-not-a-page-turn).

It refuses in two different ways, because they are two different situations:

- `409` — everything on the page has been read and the feeds have published nothing since.
  A re-roll cannot help with that. The page on screen stands, and the message says so;
  answering `404` would tell somebody looking at a page that there is nothing to put on one.
- `404` — there is nothing at all yet, and the message says to add a feed.

```jsonc
// GET /api/edition
{
  "id": "e_01J…",
  "generated_at": 1755900000,
  "next_edition_at": 1755986400,
  "items": [
    {
      "id": "a_01J…",
      "rank": 0,
      "slot": "lead",
      "read_at": null,
      "title": "…",
      "link": "https://…",
      "author": "",
      "summary": "<p>…</p>",          // sanitized at ingest, see entities.md
      "image_url": "https://…",
      "published_at": 1755880000,
      "feed": { "id": "f_01J…", "title": "…", "site_url": "https://…" }
    }
  ]
}
```

The feed is denormalised onto each item rather than sent as a side table. A card shows its
source, the client would join every one of them anyway, and an edition is sixty rows.

`{id}` in the read routes is the **item** id. It is unambiguous because an item appears at
most once in the one live edition, and it saves the client carrying an edition id it has
no other use for.

`PUT`/`DELETE` rather than `POST /read` and `POST /unread`: setting a mark twice is the
same as setting it once, and the method should say so.

### Feeds and tags

```
GET    /api/feeds
POST   /api/feeds                        {url, priority?, tag_ids?}
GET    /api/feeds/{id}
PATCH  /api/feeds/{id}                   {priority?, title_override?, tag_ids?, article_window?}
POST   /api/feeds/{id}/read              {older_than} → {marked}
DELETE /api/feeds/{id}/read              → {marked}
DELETE /api/feeds/{id}

GET    /api/tags
POST   /api/tags                         {name, parent_id?, priority?}
PATCH  /api/tags/{id}                    {name?, parent_id?, priority?}
DELETE /api/tags/{id}
```

`POST /api/feeds/{id}/read` marks a feed's articles read. `older_than` is `day`, `week`,
`month`, or empty for the whole feed; anything else is a `400` rather than quietly meaning
everything.

It reaches **articles no page has shown yet**, and that is the point rather than a side effect:
`Candidates` never offers an article this person has read, so marking a feed's backlog read
keeps it off every later page. That is what makes following a publisher again — or one somebody
has been reading elsewhere — start from now instead of from its archive. Articles already read
are left alone rather than re-stamped, so this cannot reorder Recently read.

`DELETE` on the same place is the inverse: it forgets that anything from the feed was read, so
its articles are offered again. It takes no span — "unread the last week" is a question whose
answer would be hard to predict, since the record says when something was *read* rather than
how old it is. It does not touch `shown`, so putting a feed back does not make a page draw a
story it has already carried.

`/api/feeds` is a person's **subscriptions**, addressed by subscription id. The global
`feeds` row behind it is an implementation detail and is never exposed — see
[entities.md](entities.md). What a listing returns is the merge: the person's priority,
title override and tags, plus the fetcher's title, `last_success_at`, `failure_count` and
`last_error`.

`last_error` is returned because "this feed has gone quiet" without "and here is why" sends
somebody to logs they do not have.

`POST /api/feeds/discover` says what a URL turns out to be, without subscribing to
anything:

```jsonc
{ "candidates": [ { "url": "…", "title": "Posts", "type": "application/rss+xml" } ] }
```

A site usually names more than one feed — posts, comments, a podcast, one per category —
and handing somebody whichever came first in the markup is how they end up following a
comments feed they never wanted. So the interface asks: one candidate is subscribed to
directly, several open a picker, none is a `400` saying the page offers no feed.

The `title` is the publisher's own `<link title="…">`, which is the only thing that
distinguishes the candidates without fetching every one of them.

**When a page declares nothing, the usual addresses are tried** — `/feed`, `/.rss`, `/rss`,
`/index.xml`, `/atom.xml` and a few more, against both the directory that was pasted and
the site root. Plenty of sites serve a feed and never say so: Reddit's front page is a
script shell that declares nothing while `reddit.com/.rss` is a perfectly good Atom feed,
and the same is true of anything rendered client-side. Refusing those would refuse a large
part of the web.

The guesses run concurrently under a 10-second ceiling of their own — separate from the
30-second fetch timeout, because somebody is watching a spinner and every one of these is
a guess that will usually 404. A guessed address only counts if what comes back actually
parses as a feed, so a site answering 200 with its home page for every path offers
nothing, which is the honest answer.

`application/json` is **not** an accepted alternate type. JSON Feed's own type is
`application/feed+json`; plain `application/json` is what WordPress declares for the REST
representation of a page, so accepting it offered a `wp-json` endpoint as a feed on
essentially every WordPress site.

`POST /api/feeds` still fetches the URL before accepting it and follows the *first* feed a
page names — guessing beats refusing for a caller that did not ask to be consulted. The
response says which URL was actually subscribed to.

Subscribing to a feed somebody else already follows returns the existing global feed and
creates only the subscription. A duplicate subscription is `409`.

`PATCH /api/tags/{id}` refuses a `parent_id` that would create a cycle, with `400` and a
sentence naming the tag that closes it.

### Front pages

```
GET    /api/pages                        [{id, name, slug, is_main, edition_interval, edition_size,
                                           next_edition_at, max_article_age,
                                           include_tag_ids, exclude_tag_ids,
                                           include_feed_ids, exclude_feed_ids,
                                           publish_slug, published, indexable}]
POST   /api/pages                        {name, slug} → the page
GET    /api/pages/{ref}                  ref is an id or a slug
PATCH  /api/pages/{id}                   every field above, all optional
DELETE /api/pages/{id}
```

This was `/api/settings`, which held one person's cadence and size back when they had one front
page. Everything it carried was a fact about a page, so it did not gain a `/api/pages` beside
it — it became one.

`{ref}` is an id **or** a slug, because both are how a page gets named: the interface has the
slug in a URL and everything else holds the id. A page belonging to somebody else answers `404`
rather than `403` — whether a stranger keeps a page called `finances` is not the caller's
business either way.

The main page — the **Front Page**, at `/` with an empty slug — refuses `name` and `slug` with
`400`, and refuses `DELETE`. The interface shows no inputs for either rather than inputs that
fail.

**Four lists and no mode.** This was `tag_filter`/`feed_filter` — a mode per side plus one list
each — and the modes are gone. A page does not have a filter *state*; it has an opinion per tag
and per feed, and "including" and "excluding" are things it does at the same time rather than
alternatives. Anything on neither list is something the page has no opinion about.

The tags are a funnel: include any and the page draws only from those, then exclude drops what
carries that tag *afterwards*. The order is why there are two directions rather than a choice —
tags overlap, so a finance page that wants rid of its crypto half takes Finance and then drops
Crypto, and neither gesture alone says it. The feeds do not narrow the funnel, they overrule it
in both directions.

A tag on both sides is refused. The interface cannot express it either — one list, one switch
per name, three positions — so this is a backstop rather than a case anybody meets.

All four are always present in a response, `[]` when empty. In a request, **absent and `[]` are
different**: absent leaves the list alone, `[]` empties it. That is what lets one endpoint serve
both the dialog, which saves a whole page at once, and a single control nudged on its own.

Changing the interval rebases `next_edition_at` from the last composition, not from now, so
switching daily → hourly does not mean waiting a further hour.

**Changing what a page draws from composes it again**, in the same request — any of the four
lists, or `max_article_age`. A page that says it draws from one thing and displays
another is a page that looks broken until its next turn, which for a weekly page is six days. If
nothing can be composed under the new filter the page is emptied rather than left showing what
the old one chose. Cadence and size do *not* recompose: they describe the next composition, not
the one being read, and spending somebody's page on a slider would be a surprise.

The save never fails because composing did. A page that could not be composed is not a page
that was not saved.

### Publishing

```
PUT    /api/account/public-name          {name} → 200; {name: ""} unpublishes everything
PUT    /api/pages/{id}/publish           {slug, indexable?} → the page
DELETE /api/pages/{id}/publish           → the page, published: false
GET    /api/public/{person}/{page}       the page itself — no session required
```

`GET /api/public/{person}/{page}` is **the one endpoint that needs no session**, and every way
it can fail is a `404`: no such person, no such page, never published, taken down, an instance
that publishes nothing. A stranger has no business learning which, and an owner already knows.

It takes an *optional* session anyway. When one is present the body says `signed_in: true` and
the items carry that viewer's own read marks — joined against whoever is looking, never against
the owner. Whether the owner has read something is a fact about them, and publishing a page is
not an offer to publish that too.

The body carries the **edition's id**, and that is not incidental metadata: the client draws
every card's appearance from it. Seed a published page with anything else and a stranger gets
the same articles with none of the same faces, which is a second page that happens to have the
same contents. It was `generated_at` once, briefly, and that is exactly what happened.

`indexable` is the owner's answer and never the whole answer — the instance's ceiling is applied
on the way out, so a page stored as indexable on an instance that forbids indexing reads as
false. Where the instance forbids it, the interface does not offer the choice at all.

Changing the public name repoints every published page, because the address is built from the
name rather than stored beside the page. Clearing it takes them all down, in the same
transaction, and the response says how many so the interface can warn before rather than
report after.

### Admin

Every route requires `role == "admin"` and answers `403` otherwise.

```
GET    /api/admin/users                  [{id, username, role, created_at, disabled_at, feed_count}]
PATCH  /api/admin/users/{id}             {disabled: bool}
DELETE /api/admin/users/{id}
GET    /api/admin/invites                [{id, role, created_at, expires_at, accepted_at, username}]
POST   /api/admin/invites                {role} → {id, url, expires_at}
DELETE /api/admin/invites/{id}

GET    /api/admin/instance               {public_pages, public_indexing}
PUT    /api/admin/instance               both, and turning public_pages off takes every
                                         published page down rather than only stopping new ones

GET    /api/admin/images                 {pictures, measured, unmeasured, failures: [{reason, count}]}
POST   /api/admin/images/retry           {reason?} → {reset: n}; absent reason resets all
```

`POST /api/admin/invites` returns the full link, built from `BYSTANDER_PUBLIC_URL`, and it
is **the only time the token is ever readable**. The stored form is a hash, so a lost link
is reissued rather than recovered — which is the same stance as sessions and for the same
reason.

Deleting an invitation that has already been accepted is `409`. Disable the user instead;
the invitation row is the audit trail for where that account came from.

Refused for the caller's own account, and for the last remaining admin.

`PUT /api/admin/instance` is the instance's answer rather than a default for pages to inherit,
which is why turning publishing off takes down what is already up. Both start false: an
instance that serves nothing to strangers should not begin serving to strangers because it was
upgraded.

`/api/admin/images` exists because a failure to measure a picture used to be permanent, and the
fix left a question only an administrator can answer: which failures are worth re-asking now.
A reason with a `count` of zero is not listed. An empty `reason` counts pictures nothing has
asked about yet — a queue that has not caught up, not a failure. `POST .../retry` clears
`image_retry_at` for one reason, or for all of them, and the queue picks them up on its next
sweep.

## Handler conventions

- One file per resource in `internal/api`, one function per endpoint.
- Decode into a request struct with a size-limited body. Never decode into a map.
- Validate before touching the store; the store's `ErrInvalid` is the backstop, not the
  first line.
- `internal/store` errors map to status codes in exactly one place.
- A handler never writes a status code twice, and never writes a body after an error
  helper has run.

## Client naming

Actions are named mechanically from the route — `<method><PathSegmentsInPascalCase>`, with
`By<Param>` for a path parameter — so the mapping is reversible and nobody has to guess.
See [frontend.md](frontend.md).

```
getEdition                    GET    /api/edition
postEditionRegenerate         POST   /api/edition/regenerate
putEditionItemsByIdRead       PUT    /api/edition/items/{id}/read
patchFeedsById                PATCH  /api/feeds/{id}
postAdminInvites              POST   /api/admin/invites
putAdminSmtp                  PUT    /api/admin/smtp
```

Mail has a document of its own: [mail.md](mail.md).
