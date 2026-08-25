# Editions

An edition is one person's front page: a fixed set of articles, in fixed positions, that
does not change until the next one replaces it.

Everything about it is decided on the server at generation time and stored. The browser
renders; it does not choose. That is what makes the page somebody leaves the page they
come back to.

## Cadence

Each principal picks an interval — hourly, six-hourly, daily or weekly — held in
`settings.edition_interval`, with the next due time in `settings.next_edition_at`.

A scheduler ticks every minute, takes the principals whose time has come, and generates.

`POST /api/edition/regenerate` generates immediately and resets the clock from that
moment, so a manual regeneration does not leave a stale timer about to fire.

### A manual regeneration is a re-roll, not a page turn

The two are different events and they spend the pool differently.

A **scheduled turn is time passing**. You had your day with that page; it is gone, articles
and read marks alike, and nothing on it comes back. That is the product's promise.

A **manual regeneration is not that**. Nothing has elapsed — somebody has asked for a
different arrangement of what their feeds have already published — so it first returns the
current page's **unread** articles to the pool before composing. Read articles stay spent:
those are dealt with either way.

Without this the button works exactly once. The first press consumes every article the
feeds have published; the second answers "nothing new has been published", which is true
and useless at precisely the moment somebody is setting an instance up, tuning priorities
and wanting to see what changed. Priorities are probabilities, so re-rolling is the only
way to see what a priority actually does.

The one case a re-roll cannot help with is a page that has been read through with nothing
new behind it. That answers `409` and says so.

A closed set of intervals rather than a cron expression: an arbitrary schedule is a
support burden with no matching demand, and four options fit in a segmented control.

## Selection

Weighted sampling, seeded from the edition id so a generation can be replayed exactly
when something looks wrong.

### How far back a feed reaches

Each subscription carries its own window — a day, a week, a fortnight, a month, a year, or
no limit — and an article older than it is not a candidate at all. Per feed rather than per
reader, because a news feed worth a day and a blog worth a year are exactly the pair one
number cannot serve.

How long articles are kept follows the longest window set on any feed, floored at thirty
days and capped at a year. Without that the longer windows would be a lie: a feed set to
reach back a year, with a month of articles kept, has nothing to reach into.

### The buckets

A **bucket** is a tag together with the subscriptions tagged directly with it. Every tag
that has at least one subscription is a bucket, weighted by its `priority`. Subscriptions
with no tag at all form one implicit bucket weighted at 50.

**`parent_id` does not participate in selection.** It groups tags in the manage interface
and nothing more. Sampling is flat over the buckets, which is the plain reading of "first
tag priority, then feed priority" and the one whose behaviour a person can predict from
the numbers in front of them.

The alternative — descending the tree, so a parent damps everything beneath it — is a
real design and is written down in [Open questions](#open-questions). It is not what is
built.

### Drawing one item

1. **Pick a bucket**, weighted by tag priority.
2. **Pick a subscription** within that bucket, weighted by subscription priority.
3. **Take the newest unshown item** from that subscription's feed.
4. Record it, and repeat.

Repeat until the edition holds `settings.edition_size` items or no candidates remain.

Priority is a **probability of being drawn, not a sort order**. A feed at 90 appears more
often than one at 10 across editions without ever silencing it — which is what the draft
asks for and what a strict ordering would fail to give. Two feeds at 50 and 50 are not
tied; they are equally likely, which over a week looks like variety rather than a fixed
running order.

Priority `0` means never. It is a real setting — how somebody keeps a feed subscribed but
out of rotation — and zero-weight entries are **removed from the pool before sampling**
rather than drawn and discarded, so an all-zero configuration terminates instead of
spinning.

### Guards

- **No per-feed cap, and none is needed.** There was one, on the stated grounds that a
  prolific publisher should not be able to take the page. It cannot: a draw picks a *feed*
  and then takes one article from it, so a feed with five hundred articles is drawn exactly
  as often as one with five at the same priority. Volume buys nothing; only priority does.

  The cap was guarding against something the sampler already makes impossible, and it was
  not free. Any cap expressed as a fraction of the page flattens the mix whenever there are
  few enough feeds that the caps alone can fill it — at a fifth and the default page of
  sixty, that was **everybody following five feeds or fewer**, for whom the priority
  sliders did precisely nothing. A feed's share of the page is now its share of the
  weights, which is what the slider says it is.

- **Empty buckets are dropped** as they exhaust, and a bucket whose every subscription is
  capped or dry is dropped with them. The pool only ever shrinks, so the loop terminates.
- **A page with room left over is filled from what has been seen.** When the fresh pool
  runs dry and the page is not full, the rest is drawn from articles this person has
  already been shown — by the same weighting, so a page that has to repeat itself repeats
  the feeds somebody said they cared about.

  This used to be a refusal: "never padded with articles already shown". It was the wrong
  call. A half-empty page reads as broken rather than principled, and those articles were
  going to be pruned unread either way.

  Two things keep it honest. **Seen-but-never-read comes first** — an article that went
  past unread is closer to new than one somebody has already finished with. And **a repeat
  keeps its read mark**: `ReplaceEdition` carries `read_at` over from the month-long
  record, so something read last week arrives greyed rather than pretending to be new.
  That is the one thing a page that repeats itself must not do.

  When both pools are dry the page really is short, which is still the honest answer.

### A subscription in several tags gets several chances

A feed tagged both "Art" and "World News" sits in two buckets and is drawn from either.
This is a genuine effect, not an accident: tagging a feed more broadly does make it
appear more often. The per-feed cap is what keeps it from becoming the page.

## Layout

Each selected item is given a `slot` — how many of the page's sixteen tracks it takes.

| slot | tracks | rendering |
| --- | --- | --- |
| `lead` | 16 | The full width of the page |
| `wide` | 12 | Leaves four, which only a column can fill |
| `feature` | 8 | Half the page |
| `standard` | 4 | An ordinary column |
| `brief` | 4 | A column, title and source only |

Sixteen tracks and not four. On a four-track grid the only widths available are 4, 2 and 1,
all of which divide four, so every row tiles perfectly and `grid-auto-flow: dense` never has a
hole to backfill — measured, and it had not moved a single card. Twelve is the width that does
not tile: a row holding one has four tracks left, which only a column can fill, so `dense` has
to reach past the next article to find one. That reaching is the mechanism; a page that
backfills is a page with a shape instead of rows.

An item becomes a `brief` regardless of anything else when it has no image and no usable
summary. A card sized for a picture that has no picture is the thing that makes a page look
broken, and demoting it is cheaper than styling around it.

### Which cards are widened, and how wide

Two questions, answered separately, and keeping them apart is what stops either from being
decided for the wrong reason.

**Which** is a question about the page. Roughly one card in four gets more than its column,
the first one always — a front page that opened with four narrow cards would have nothing to
look at first. The rest go one per band, drawn inside the band rather than at a fixed offset,
which is a pattern a reader picks up well before they could name it. One in eight was tried and
is too thin: on a page of twenty-eight that is three wide cards and twenty-five identical
quarters, which reads as a uniform page with a couple of accidents in it. Much past a quarter
and wide is the norm, which is the same problem wearing the other hat.

This replaced a rule that gave rank 0 the lead and ranks 1–4 the features. Rank is draw order
out of a weighted sample, not an editor's judgement of importance, so there was never anything
to preserve by stacking prominence at the top — and it ran the page big to small and then left
forty cards identical.

**How wide** is a question about the article, and the picture is the part of it with a shape.

- A **band** — wider than 5:2 — is drawn towards `lead` and `wide`. Across a quarter-page
  column a 10:1 picture is sixty-five pixels of photograph over a headline, which does not read
  as a small picture, it reads as a mistake.
- An **upright** — taller than it is wide — is always `feature`, the narrowest of the three.
  Width costs an upright picture height it cannot spend: across the full page it is bounded at
  70vh and what survives is a slice through the middle of it. Eight tracks is also the width at
  which a picture can be set beside its story rather than above it, which is the one
  arrangement an upright is better in than a square.
- Anything else, and anything **nothing has measured**, draws as it always did. An unmeasured
  page is laid out exactly as this laid pages out before any of this existed.

On top of that, a band is never left in a column at all, whether or not the page picked it out:
`feature` is a floor rather than a prize. It does mean a page of bands comes out wider than one
card in four, and that is the correct answer to a page of bands — the alternative is a column
of slivers, chosen so that a rule about landmarks could hold on a page that has none.

### What the client decides

The client renders slots. It does not compute them, measure anything, or run a layout pass —
which is why the page does not reflow after paint and why two loads of the same edition are
identical.

It does decide everything *else* about how a card looks — the face, whether it is boxed and in
what, how many columns the standfirst is set in, the crop for an unmeasured picture, whether
the picture sits beside the story, where the rules fall. All of it comes off one seeded stream
per card, keyed on the **edition id and the article id together**, so it is identical on every
load, in every tab, and for every viewer of a published page. See
[frontend.md](frontend.md).

The line between the two is not "layout versus rendering". It is: the server decides what is on
the page and how much room each thing gets, because those are facts about the page that have to
be the same everywhere and have to be frozen; the client decides what it looks like, because a
seed makes that identical without storing it. Whether the line is in the right place is an
open question — see below.

## Committing

One transaction against `derived.db`:

1. Insert the new edition.
2. Insert its items and their slots.
3. Insert the `shown` rows, `INSERT OR REPLACE` — an article can legitimately be shown again
   after its previous record was pruned, and re-stamping the date is exactly right.

Nothing is deleted. Older editions are simply no longer the newest for their page, and
`currentEditions` picks the newest per `page_id` — so a crash mid-generation leaves somebody
with yesterday's page rather than with nothing, and it does so without a delete that has to
happen first.

This used to begin by deleting the previous edition, forced by a unique index on
`editions.principal_id` back when a person had exactly one page. Both the index and the delete
are gone; see [entities.md](entities.md#editions).

**Nothing about reading is written here.** It used to be — the mark was copied forward from
`read_articles` so an article shown again did not come back looking new — and that copy was the
whole problem: a second place for a fact with one home, which every write then had to remember
to keep in step.

## Read marks

`PUT /api/edition/items/{id}/read` writes a row in `read_articles`; `DELETE` removes it. The
interface greys the card in place.

A read item **does not move, collapse or disappear**. Where an article sits on the page is
how somebody remembers where they were, and rearranging under them would be the
unread-count problem wearing a different hat.

**A mark is a fact about a person and an article**, and nothing else — not about the edition it
was made on, not about the page. So it survives the page turning, it greys the article on every
other page currently carrying it, and it works on a page somebody else published, because the
join is against whoever is looking. It ends when the feed is unfollowed. There is one endpoint
for all of that and there used to be two; see
[entities.md](entities.md#edition_items) for what the second one was and why it went.

It has a second job beyond greying a card: an article somebody has read is never offered to any
of their pages again — `Candidates` excludes it — which is what stops a story coming back a
year later as though it were new.

## Open questions

- **Hierarchical descent.** Weighting by the path — sample among root tags, then among
  that tag's children — would let a parent damp everything beneath it, which is a natural
  thing to want from "World News" at 10. It costs the flat model's predictability and
  raises a question the flat model does not have: what weight the subscriptions filed
  directly on a parent should carry against its children.
- **Recency.** Step 3 takes the newest unshown item. An older item from a slow feed
  therefore waits behind nothing, but a fast feed's backlog is never reached — it is
  always pruned before its turn. Whether that is right depends on what feeds people
  actually add.
- **Edition size of 60** was a guess and has been checked against a real instance; see
  [entities.md](entities.md#open-questions).
- **Slots are frozen at compose time** and now depend on a picture's measured shape, which
  arrives asynchronously — so a page composed before its pictures were measured keeps the slots
  it chose, and only a recomposition fixes it. Whether layout should move to the client instead
  is genuinely open: it would follow the measurements, and it would also let a slot respond to
  the viewport, which a stored one cannot. Against it: the page would then relaminate as
  measurements land, which is the page-moving-under-the-reader problem this design spends most
  of its effort avoiding.
