# Subscription lists

Sharing feeds, and taking somebody else's.

## The format, verified

OPML 2.0. Checked against the spec's own example files rather than memory —
`hosting.opml.org/dave/spec/subscriptionList.opml` and `.../category.opml`, since
`opml.org/spec2.opml` serves a JavaScript shell to every path.

```xml
<opml version="2.0">
  <head>…title, dateCreated, ownerName — all optional…</head>
  <body>
    <outline text="…" title="…" type="rss" xmlUrl="…" htmlUrl="…" category="…"/>
  </body>
</opml>
```

`xmlUrl` is the only attribute that really matters. `text` is the one OPML requires;
`title` repeats it because half the readers in the world use one and half use the other.

## Flat, with the tags in `category`

OPML has two ways to say which group a feed belongs to, and we use one of them.

**Nested outlines** are what every reader renders as folders — but nesting is a tree, so a
feed lives in exactly one folder. A feed here can carry several tags.

**The `category` attribute** has no such limit. From the spec's own example:

```xml
<outline text="The Mets are the best team in baseball."
         category="/Philosophy/Baseball/Mets,/Tourism/New York"/>
```

Comma-separated, each value a slash-delimited path — which is exactly this model: several
tags per feed, each one possibly nested. So the writer emits a **flat** list and puts the
whole truth in `category`.

**The reader is more forgiving than the writer.** A file from any other reader will use
folders, and refusing it because we would have written it differently is a poor way to meet
somebody's subscriptions. So `Decode` accepts both and takes the ancestor folders as
categories when a feed names none of its own. When a file carries both, the attribute wins:
it is the one that was not forced to choose a single path.

### Punctuation

OPML defines no escaping, so a tag called `Art, Design` would split into two. `%2C`, `%2F`
and `%25` are percent-encoded within a segment and decoded on the way back. Nothing else is
touched, so the common case stays readable — `/News/World` — and the uncommon one survives
a round trip instead of quietly becoming two tags.

### Priority and reach

OPML has no field for either, and an outline "may contain any number of arbitrary
attributes", so they ride as `bystanderPriority="90"` and `bystanderReach="86400"`. Every
other reader ignores both.

Deliberately not namespaced: a namespace would be more correct and would mean fighting
`encoding/xml` over prefixes to gain nothing, since a name nobody else would pick is
already unambiguous.

Reach is seconds, and it travels because it is part of what a list recommends — a news wire
worth a day and a blog worth a year are not the same suggestion, and a list that arrived
without them would quietly put every feed on the default week.

**"Did not say" is −1, not zero**, for both. Zero is a real reach — no limit — so it has to
be distinguishable from an attribute that was never written; `omitempty` would otherwise drop
it. A file naming a reach outside 0…1 year is read as not having said. One inside that range
but not among the reaches this program offers is carried as far as the import, which settles
it to the default there: refusing a whole subscription over a number nobody can honour would
be the wrong trade.

### Dates

`Mon, 02 Jan 2006 15:04:05 GMT` — **not** `time.RFC1123`, which writes the zone's name and
so says `UTC`. Every date in the spec's examples says GMT, as does every date in HTTP, and
a parser matching the literal is not wrong to.

## Import happens twice

An import is somebody else's decisions arriving in bulk: which feeds, filed under which
names, at which priorities. Applying that unseen is how a person ends up with a taxonomy
they did not choose and cannot easily unpick.

So `POST /api/feeds/import/preview` reads the file and answers the two questions worth
asking first — **what do I already follow**, and **which of these tags are mine** — and
changes nothing. Feeds already followed are matched on the *canonical* URL, so a list
saying `http://` for something followed over `https` is not offered twice.

Then `POST /api/feeds/import` subscribes to exactly what it is sent, and has no opinion
about what was in the file. Unticking a feed is not sending it; unticking a tag is leaving
it out of `tag_paths`. That is what keeps "what I chose" and "what happened" the same
thing.

**Feeds you already follow are not offered.** The server refuses a second subscription and
reports it as skipped, so a row for one would be a choice that does nothing — worse than no
row, because it reads as filing about to happen. They are counted in a line above the list
instead, so a list that overlaps heavily does not simply look shorter than the one that was
sent.

**Every tag you own is offered under every feed**, not only the ones the list mentioned.
Filing a stranger's feed is the moment you actually know where it belongs, and the
alternative is importing it and going to find it again. The ones the list named and you
already have are ticked — that is a match, not a decision. The ones it named that you do
not have sit after a divider, dashed, and **unticked**: a taxonomy should arrive because
somebody asked for it, not because it came in the post.

Some consequences worth knowing:

- **A tag path is created whole.** `News / World` creates `News` too — half a path is not a
  place.
- **An existing tag keeps its own priority.** The file's priority applies to feeds, not to
  a taxonomy somebody already built. Importing a list where `Art` sits at 90 does not
  disturb the `Art` you set to 20.
- **Importing the same list twice is quiet.** Already following it is the ordinary outcome,
  reported as `skipped` rather than as a failure, and no tag is duplicated.
- **No feed is fetched during an import.** A hundred subscriptions would be a hundred
  outbound requests and several minutes of somebody watching a spinner. The title from the
  file stands in until the poller gets to it, which is within the minute.

## Adding a site is the same screen

A site's markup usually names several feeds, and the picker for choosing between them is
the same component as the import plan — checkboxes, All/None, and every tag you own under
each feed.

That is not tidiness for its own sake. After "where did these come from" the question is
identical both times: **which of them do I want, and filed under what.** Two screens would
have drifted, and one of them would have been the one nobody remembered to fix.

So `POST /api/feeds/discover` answers in the same shape `POST /api/feeds/import/preview`
does, and both go on to `POST /api/feeds/import`. Adding a site therefore gained per-feed
tagging for free — previously you added a feed and then went to find it again.

One feed found and no choice to make still goes straight in without a dialog, counted
*after* dropping what is already followed: a site whose other feed you took last week does
not open a picker with one row in it.

## Sharing has two shapes

Because there are two people on the other end.

**As a list** — names, addresses and the tags they were filed under, built client-side.
For somebody reading a message, and the reason the tags are there at all: a stranger's feed
is opaque until you know what its owner considered it to be.

**As OPML** — for their reader, which does not want prose.

## Sharing by link

A file was the only way to hand somebody a list, and a file is the wrong object between two
phones: save it, find it, paste it back, and each of those three steps fails differently.
A link skips all of them.

`POST /api/shares` stores **the OPML the export already builds** and returns a URL. Not a
reference to what somebody currently reads: a snapshot, because unfollowing something later
is not a reason for somebody else's link to change under them. Storing the document rather
than a list of feed ids also means one code path decides what "my feeds" means — which title
wins, how a tag path is spelled — instead of two that would drift.

`GET /api/shares/{token}` decodes it and runs it through the same `plan` an imported file
goes through, so the answer is `previewFeed` and the screen is the picker that already
exists. Taking any of it goes through the ordinary import endpoint, so **opening a link
subscribes nobody to anything**.

A session is required at both ends. This is a list of what somebody reads handed to another
person on this instance, not published to whoever guesses the URL.

The token is 32 random bytes and only its hash is stored — the same stance as sessions and
invitations. Expiry is a week, and an expired link answers exactly as an unknown one does:
whether a URL was ever real is not a question to answer for somebody trying enough of them.
The sweep that prunes articles prunes dead shares too, which is housekeeping rather than
enforcement — the check on open is what enforces it.

Shares live in `main.db`, not `derived.db`, despite expiring in a week. The disposable half
is rebuildable from the feeds; a share is not rebuildable from anything, and a link somebody
sent to a friend dying because a cache was rebuilt is a promise broken by an implementation
detail.

### The three shapes

`ShareDialog` offers a link, a list, and OPML, in that order — three shapes because there are
three people on the other end. The link is first because it is the one that works phone to
phone.

Every link goes through `CopyBox`, which is where the three ways off this machine live:
**Copy** works everywhere and reaches nothing but this device; **Send** hands it to the
system share sheet, which is how a link actually gets to a phone — AirDrop, a message — and
is feature-detected because `navigator.share` needs a secure context that a self-hosted
instance on plain http will not have; **QR** shows a code, for when the other device is in
the room and neither of the first two will do.

The QR is `qrcode-generator` rendered as one SVG path — black on white regardless of theme,
because scanners expect dark on light and enough of them fail on an inverted code. Its test
decodes it with `jsqr`, a different library: asserting that some rectangles were drawn would
prove nothing about the only thing it is for.
