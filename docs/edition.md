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

That refusal is now asked for rather than stumbled into. A scheduled turn composes from
whatever there is, repeats included; a re-roll passes `freshOnly` and writes nothing at all
when the fresh pool is empty, because handing back the page somebody just pressed the button
on with every card greyed is not a different page. The check happens before anything is
written — composing first and reporting failure afterwards would replace what they are
looking at and then tell them nothing happened.

A closed set of intervals rather than a cron expression: an arbitrary schedule is a
support burden with no matching demand, and four options fit in a segmented control.

## Selection

### In plain terms

Every feed is **asked, in turn, whether it wants to contribute** — and says yes with a
probability that is its priority. That is the whole algorithm; everything below is the detail.

```
1. Work out which feeds may appear on this page at all.
   Its tag filters and feed overrides decide this. Tags do nothing else.

2. Build one queue per feed, in three bands, newest first inside each:
      new      never shown on this page, never read
      unread   shown here before, never read
      read     dealt with

3. Fill the page a band at a time — all the new, then all the unread, then the read.

4. Within a band, a round is one pass over the feeds in a shuffled order.
   Each feed is offered a turn and takes it with odds set by its priority,
   handing over the article at the front of its queue.

5. If the feeds still holding something sum to less than 100, every one of them
   is scaled up until they do — so a round is expected to place an article.

6. Rounds repeat until the page is full, or until a round finds that no feed
   has anything left to be asked for.
```

Seeded from a number stored with the edition, so the same page can be composed again.

**Priority is odds, not an order.** A feed at 90 is asked as often as one at 10 and says yes
nine times as often, so it gets about nine times the places; neither is ever silenced, and
zero means never. What priority is *not* is a ranking — nothing here sorts feeds and takes
the top.

**The slider is linear, and does not depend on the company it keeps.** Two feeds at 50 and 25
sit at two to one whether the page holds those two or thirty others besides. Nothing is
normalised against a total, so there is no total to shift under them.

**Volume buys nothing.** A feed is asked once per round whether it has four articles waiting
or four hundred, so a publisher posting two hundred times a day is asked exactly as often as
one posting twice. This is
load-bearing rather than incidental: on a real subscription list, picking articles instead of
feeds gave two feeds set to 25 and 10 forty-one places out of ninety, purely because between
them they had a third of the articles.

**Tags decide eligibility and nothing else.** Whether a feed can appear on a page is a
question its tags answer. How much of the page it gets is a question only its own priority
answers. Tag priority used to weigh the draw as well, and that meant a feed carrying three
tags was drawn from three buckets and took a quarter of a page at the same slider setting as
a feed carrying one — which is not something anybody asked for.

**New first, everywhere.** Every new article from every feed is placed before any repeat from
any feed. A page with room left over and nothing new for it looks broken rather than honest,
so the later bands fill the rest — greyed if they were read, plain if they merely went past.
When all three bands are dry the page really is short, which is still the honest answer.

### How far back a feed reaches

Each subscription carries its own window — a day, a week, a fortnight, a month, a year, or
no limit — and an article older than it is not a candidate at all. Per feed rather than per
reader, because a news feed worth a day and a blog worth a year are exactly the pair one
number cannot serve.

How long articles are kept follows the longest window set on any feed, floored at thirty
days and capped at a year. Without that the longer windows would be a lie: a feed set to
reach back a year, with a month of articles kept, has nothing to reach into.

### Why not quotas

This used to allot each feed a quota — `size x priority / sum of priorities` — and fill the
page from the front of each queue. On paper that has exactly the right expectation, and a
share that is the share it claims on *every* page beats one that is right on average over a
month. It did not survive contact with real queues.

Shares almost never come out whole, so places are always left over. Giving them to the
largest fractions — the textbook answer — **silences**: a feed whose share is 0.4 of a place
has the same fraction on every page, loses every time, and never appears. So the leftovers
were drawn, weighted by those fractions, with a floor under anything allotted nothing.

The trouble is how many places ended up in that draw. A live front page of ninety, drawn from
fourteen feeds with anything new: the whole parts accounted for **twenty-seven** places and
**sixty-three** went to the leftover draw — because ten of those feeds had one to three
articles each, and every place their share could not use fell through to it. Seventy per cent
of the page was dealt by the tie-breaker.

And the tie-breaker is not priority. It is the *fractional part* of a share, which is a
sawtooth: `frac(90x10/500)` is 0.8 while `frac(90x25/500)` is 0.5, so the feed at 10 drew at
23.5% a place against 14.7% for the feed at 25, and the feed at **5** drew best of all at
26.5%. Every feed at 50 worked out to exactly 9.00 places, a fraction of zero, and was floored
to the 0.01 minimum meant to stop feeds being silenced by arithmetic — which silenced the
entire priority-50 cohort instead.

What that looked like from the outside: the Guardian, at priority **10**, took 24 of 90 places
on a real front page, a mean of 26.7 over 400 seeds, against 20.4 for Hacker News at **25**.
Under the round robin the same queues give 13.5 and 34.3 — a ratio of 2.54 where the sliders
say 2.5.

There is no arithmetic in the round robin to get wrong. A feed is asked, or it is not.

### Lifting the odds

If every feed on a page is set to 1, a round places an article one time in a hundred and a page
of ninety takes about nine thousand rounds. It is the same page — the odds are relative, and
scaling all of them by one constant cancels out of every ratio between them — arrived at a
hundred times more slowly.

So when the feeds still holding something sum to less than 100, they are all scaled up until
they do. Ten feeds at 1 are asked as ten feeds at 10. Never scaled down: where the sliders
already promise a full round, they are used as they are.

Measured against the unscaled version over 400 seeds on a real front page and seven synthetic
ones, the two agree on every feed to within the noise of the draw, and composing that front page
went from 215µs to 76µs. One feed alone at priority 1 went from 418µs to 9µs.

**It is not a termination guarantee**, and the arithmetic looks enough like one to be worth
saying so. Ten feeds at 10% leave a round empty about a third of the time, and nothing stops
empty rounds recurring. What the lift bounds is the *rate*: the chance of an empty round is
largest when the odds are spread thinnest — n feeds at 1/n, or (1-1/n)^n — which climbs towards
1/e and never reaches it. Under 37%, whatever anybody sets. Whether the loop stops is still the
backstop's job.

### When a feed runs out

Nothing is redistributed. Its neighbours go on being asked at their own rate, and the page
takes more rounds to fill — the same page, arrived at more slowly.

Quotas had to redistribute, because a feed that could not fill its share would otherwise leave
the page short. But the places always came back to whoever still had something, and whoever
still has something is the firehose — so priority stopped governing the moment the quiet feeds
ran dry, which on a real subscription list is within two rounds.

A page still comes up short when every queue is dry. That is the honest answer rather than
something to pad.

### Guards

- **Zero means never.** A feed at zero is dropped before the rounds begin, rather than left in
  at odds of zero — there is then nothing to ask, and no way for it to pick up a place.
- **One article, one place.** A feed's three bands are one queue, and an article placed in an
  earlier pass is stepped over in a later one. What makes two rows the same article is the
  link, not the id: a piece carried by a feed and by its mirror is two rows and one article.
- **Termination.** A pass ends on a round where no feed had anything left *that could go on
  the page* — which is not the same as a round that placed nothing. Empty rounds are ordinary
  and the page is still composed; a feed whose whole queue is pieces another feed already
  placed has nothing to offer, and must not read as though it had.
- **The backstop.** Every feed in the running has odds above zero and cursors only move
  forward, so the loop cannot sit in a state with nothing left to happen — but that is
  termination with probability one, not termination. A run of 10,000 rounds that place nothing
  ends a pass. With the odds lifted an empty round is at most a 1-in-e event, so a page that
  still had something to draw would have to lose that bet ten thousand times running.
- **The order is redrawn every round**, so no feed permanently gets first refusal on a link
  two feeds both carry. Under a fixed order, Ploum.net's one new article — the same URL Hacker
  News and Lobsters had both picked up — lost that race on every seed, and the feed never
  appeared at all.

### Bands are positions in a queue, not separate pools

This is worth stating because it was a bug for as long as it was not true. The new articles
and the repeats used to be two independently built pools, each carrying its own tag buckets,
and a feed missing from one silently vanished from the other. A page of comics read right
through reached one feed out of five and came back with four articles out of a possible
sixty-one.

Two related things were wrong with the same shape. "Shown" is per page and "read" is per
person: an article read on one page has never been shown on another, so it must not arrive
there as though it were new — but it was also excluded from the repeats, which only held
articles with a `shown` row *for that page*. Five of the comics page's sixty-six articles were
in neither pool and could not appear at all.

One queue with three bands has no room for either mistake.

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
of their pages as *new* again — it drops to the last band, behind everything unread — which is
what stops a story coming back a year later as though it were fresh. It can still return as a
repeat when a page has nothing else, and it arrives greyed when it does.

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
