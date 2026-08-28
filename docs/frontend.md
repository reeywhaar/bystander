# Frontend

React 19, TanStack Query 5, Tailwind 4, TypeScript 7, Vite 8. Versions and rationale in
[stack.md](stack.md).

## Islands

Five Vite entries, not one SPA with routes:

| entry | audience | contains |
| --- | --- | --- |
| `login.html` | nobody yet | The landing page at `/`, login, invitation acceptance, password recovery |
| `index.html` | a reader | The front pages |
| `manage.html` | a subscriber | Feeds, tags, pages, what has been read |
| `admin.html` | an administrator | Users, invitations, mail, publishing, pictures |
| `public.html` | a stranger | Somebody's published page |

The split exists because these are five applications with five audiences. `login.html` is
the only document an unauthenticated visitor loads: it is a few kilobytes rather than the
whole interface, and somebody opening an invitation link who has never heard of this
instance gets a page about accepting an invitation rather than the shell of an application
they cannot use.

**`/` belongs to two of them.** Somebody with a session gets the reader; anybody else gets
`login.html` and its landing page, and the server chooses by whether the request carried a
session cookie — the only routing decision that reads anything but the path, and argued for in
[backend.md](backend.md#serving-the-spa). Before it did, a stranger typing the address got the
reader's shell, a bundle they cannot use, a `401` and a whole-document redirect to a login
form, without a word about what they were looking at on the way.

That is also why signing in from the landing page *navigates* rather than re-rendering: the
document itself is the wrong one once a session exists, and only the server can swap it.
Signing out goes to `/` for the same reason — with the cookie cleared, that is the landing
page, which is a better thing to hand somebody who has just left than a form asking them to
come back.

`public.html` is there for the same reason and is the strongest case for it. A stranger
following a shared link should be handed a page — not the shell of a product they have no
account for, half of whose controls would refuse them. It renders the same cards the reader
does, from the same components, with two things absent rather than disabled: no way to compose
a page, and no way to mark anything read unless the request carried a session. A control that
exists and says no is still advertising what an account would let you do.

`admin.html` is not merely hidden from non-admins — its code is never sent to them. Route
guards are a correctness measure; not shipping the bundle is a smaller attack surface and
a smaller download.

React-router lives **inside** each island for its own sub-routes. Nothing routes between
islands in JavaScript; a link between them is an ordinary `<a href>` and a fresh document.

`vite.config.ts` names all five inputs, so a missing one is a hard error rather than a
quietly smaller build. The Dockerfile asserts each is non-empty afterwards.

Which shell a navigation receives is decided server-side by prefix, in one function. See
[backend.md](backend.md#serving-the-spa).

## Icons

Named the way SF Symbols names things, minus the dots that JSX will not take:

    noun[.qualifier][.fill]        person.fill      eye.slash      arrow.right
    Noun[Qualifier][Fill]Icon      PersonFillIcon   EyeSlashIcon   ArrowRightIcon

The parts read outside in — what it is, then which of it, then how it is drawn — so an
alphabetical listing of `src/components/icons/` groups the variants of one thing together
instead of scattering them under whatever adjective came to mind. `FilledPersonIcon` sorts
under F.

**`Fill` is load-bearing, not decoration.** An unsuffixed name is the outlined cut. The two
are not interchangeable: a stroke at 1.3 on a sixteen-unit grid reads lighter than the type
beside it, a silhouette reads at the same weight, and which one a place wants depends on
whether the icon is standing next to a word or standing alone. Leaving the suffix off a
filled icon is how a codebase ends up with two `PersonIcon`s that look nothing alike.

Every icon takes `className` and nothing else, is `1em` square, and paints with
`currentColor` — so it takes the size and colour of whatever it is set beside rather than
holding either of its own. None of them carry a label: they sit next to the word they stand
for, so they are `aria-hidden` and the text does the talking.

**A cut is a component, not a prop.** `<PersonIcon mode="fill">` reads better at the call
site and is the wrong shape: it obliges every icon to draw both cuts or to accept a prop
with one legal value, and today not one of them has two — Person is filled, Eye is
outlined, GitHub is a trademark with no outlined form at all. It would also put both sets
of path data in every chunk that imports either, which is the same reason `src/api/actions`
exports functions instead of static methods. SF Symbols draws the same line: `person` and
`person.fill` are two symbols, not one symbol with a parameter. If a noun ever genuinely
needs both cuts in this codebase, a prop is the better answer for *that* icon — but adding
it before then buys nothing and promises something untrue.

**Brand marks are not symbols and do not follow this.** `GitHubIcon` is somebody's
trademark, recognised as one silhouette, and there is no outlined cut of it to distinguish
— it keeps its own name and is filled because that is what it is.

**A mark beside a word goes inline, never in a flex row.** This has cost two visible bugs
now, in the masthead's username and in its GitHub link, and both looked fine alone and wrong
in company:

```tsx
<span><PersonFillIcon className="mr-[0.2em] inline align-[-0.125em]" />{username}</span>
```

A flex container takes its baseline from its first flex item; an SVG has no baseline, so one
is *synthesised* from the bottom margin edge of the box. Put the icon first and the whole
label is then aligned by the bottom of the mark rather than by its type — so in any row that
is `items-baseline`, the word drops. Seven pixels at 14px type, measured; enough that
everybody sees it and nobody can name it. `items-center` on the outer row does not fix it,
it just moves the near-miss.

Inline, the text carries the baseline and the mark is placed against it like any other inline
object, which is what `align-[-0.125em]` is for. `UserLabel` exists for exactly this and
`UserLabel.test.tsx` and `GuestMasthead.test.tsx` both assert it, because it is invisible in
review and obvious on screen. A padded button is the exception — `flex items-center` is right
there, since nothing outside it shares a baseline with its contents.

## The API layer

Four files under `src/api`. The layering is the point and is not negotiable:

| file | responsibility |
| --- | --- |
| `transport.ts` | The only file in the project that mentions `fetch`. Returns a raw `Response`, never a parsed body |
| `dispatcher.ts` | Runs a request, parses, throws `ApiError`. Carries the `AbortSignal` |
| `request.ts` | `ApiRequest`, `ApiAction<T>`, `createApiAction` |
| `provider.tsx` | `ApiProvider`, `useApiDispatcher`, `useApiCall` |

**Nothing above `transport.ts` mentions a header, a credential or a URL origin.** Two
rules keep that honest: a transport returns a `Response`, and nothing outside that file
imports `fetch`.

### Actions are curried

```ts
export type ApiAction<T> = (dispatcher: ApiDispatcher) => Promise<T>;
```

`getEdition()` builds the action; handing it a dispatcher runs it. That buys two things
over returning a bare request object:

- Post-processing is ordinary code whose result type TypeScript infers, rather than a
  `select` callback typed by hand and cast at the boundary.
- Composing actions is just calling them — an action can await another without any of it
  leaking into the dispatcher.

Note what an action's signature does **not** contain: an `AbortSignal`. Cancellation rides
on the dispatcher through `withSignal`, and signals *merge* rather than replace, so
whichever aborts first wins. No action has to accept, name or forward a signal it does not
care about, and forgetting to thread one through becomes impossible rather than merely
unlikely.

### Naming

`<method><PathSegmentsInPascalCase>`, with `By<Param>` for a path parameter. The mapping is
reversible, so nobody guesses in either direction. Examples in
[api_design.md](api_design.md#client-naming).

One file per resource under `src/api/actions/`, one exported **function** per endpoint —
functions rather than static methods on a class, because a bundler can drop an exported
function a chunk never references while a class stays alive as one unit. The login screen
has no business carrying the code that builds an admin request.

### Errors

`dispatcher.ts` throws `ApiError` carrying the status and the server's sentence. Data
components suspend and their nearest boundary renders the failure. A component that
swallowed an error and rendered nothing would leave somebody looking at an empty page
unable to tell "no feeds" from "this broke".

`401` is the one status handled centrally: the island sends the visitor to `/login`.

## Queries

Every query key lives in one `qk` object under `src/queries/keys.ts`. Nothing outside that
file writes a key.

Two things follow, and the second is why the file exists. A key cannot be typo'd at a call
site; and an invalidation names the same constant the query does, so it cannot address a
key no query uses. TanStack matches by prefix and says nothing when a prefix matches
nothing, which makes that failure completely silent.

```ts
export const qk = {
  me: ["me"] as const,
  edition: ["edition"] as const,
  feeds: ["feeds"] as const,
  feed: (id: string) => ["feeds", id] as const,
  tags: ["tags"] as const,
  settings: ["settings"] as const,
  adminUsers: ["admin", "users"] as const,
  adminInvites: ["admin", "invites"] as const,
};
```

The hierarchy does the invalidation work by construction: `["feeds"]` is a prefix of every
single feed, so a write that changes the listing also refreshes the detail.

One hook per thing the interface reads, in `src/queries/hooks.ts`, each threading
TanStack's `AbortSignal` through to the request so a superseded fetch stops rather than
racing the fetch that replaced it. Declared once rather than at each `useQuery`, because
the same read happens in several places and four declarations of one query are four
chances for them to disagree.

## The front page

A sixteen-track CSS grid with five named slot spans — `lead` 16, `wide` 12, `feature` 8,
`standard` and `brief` 4 — narrowing onto the same sixteen tracks under `1100px`, `820px` and
`560px` rather than changing the track count, so there is one grid to reason about instead of
four.

**No measurement pass and no masonry library.** Slots were decided server-side and stored
(see [edition.md](edition.md#layout)), so the layout is static: the page does not reflow
after paint, and two loads of the same edition are identical.

**One component renders it, shared by the reader and a published page.** `PageGrid` takes the
edition id and the items and does the whole grid — the seeded styles, the voice assignment
over the sequence, where the rules fall. It is shared rather than written twice because the two
are not similar screens, they are the same page seen by two people: a copy of this logic that
seeded itself differently produced a published page with the same articles and none of the same
faces, which is exactly what happened when the public endpoint did not send the edition's id.

Read cards grey out **and desaturate** in place. They do not move, collapse or disappear —
where an article sits is how somebody remembers where they were. Opacity alone was not
enough: it left the accent-coloured links inside a summary still coloured, which is the one
thing on a card that says *look here*, and left a photograph reading as a photograph.

It does not come back under the pointer. Read is done with, and a card that brightened
whenever the mouse crossed it would put the page back in the business of asking for
attention.

### Six headline faces, assigned by id

A newspaper keeps a handful of display faces and picks between them story by story. That is
most of what makes a sheet of forty unrelated headlines read as one publication rather than
as a list, which is exactly the problem here.

So `styles.css` defines six **voices** — `didone`, `antique`, `workhorse`, `slab`, `gothic`,
`humanist` — one per *genre* of display face rather than six variations on one. Nothing is
gained by faces that are nearly the same: the whole effect depends on somebody being able to
tell at a glance that this headline is not set like that one.

Three parts, and the split is the point:

| where | what it decides |
| --- | --- |
| `lib/voice.ts` | which article gets which voice |
| `styles.css` (`.headline`, `.slot-*`, `.voice-*`) | what each voice looks like, and how big |
| `ArticleCard` | applies `headline voice-${voice}` and sets no size of its own |

**The face is a pure function of the article's id.** Not stored beside the slot, and the
difference is worth stating: a slot *has* to be stored, because it comes out of a weighted
draw that cannot be repeated. A voice is a hash, so storing it would buy the same answer at
the price of a column, a migration and an API field. What matters is that it is stable —
two loads of one edition are identical, which is the same promise the fixed layout makes.

**No two headlines in a row share a face.** That is the one rule a newspaper's headline
typography actually has, and a plain hash breaks it about one time in six, which does not
read as chance — it reads as the page having failed to notice. It is therefore a fact about
the *sequence*, which is why `assignVoices` takes the whole list and `ArticleCard` receives
a `voice` prop rather than working one out for itself.

**The slot decides the size and the voice only scales it**, via `--headline-size` and
`--voice-scale`. Two reasons. Prominence stays the server's decision — a lead is a lead
whichever face it landed in — and the faces genuinely disagree about how big twenty pixels
is: Oswald is condensed and tall, Bitter wide and short, Old Standard TT so much wider than
the rest that at one size it took three lines where the others took two. The multipliers
were measured against a real page. There is a test asserting the `<h2>` carries no Tailwind
size utility, because one would win the cascade and take the voice's scale with it.

The faces are self-hosted woff2 under `src/fonts`, with `@font-face` rules **generated** by
`scripts/fetch-fonts.sh` into `fonts/fonts.css` — generated because each subset's
`unicode-range` comes back from Google beside the URL it belongs to, and transcribing those
by hand is how a font ends up downloaded for a page with no glyph in it, or not downloaded
for a page that has one. `fonts.css` is in `.prettierignore` for the same reason.

`font-display: swap`, which is a choice between three ways of being wrong for a moment:
`block` hides the text, and on a page that is almost entirely headlines that is a blank
front page; `optional` never swaps, so the first visit — the one where somebody decides what
this looks like — gets none of the typography the page is about. `swap` reflows once, on a
cold cache, before anybody has started reading. That is a load event, not the page reacting
to being looked at, which is the rule below that it might otherwise break.

**Nothing on the page moves.** No transitions, no colour shifts, no controls that appear
when the pointer arrives — a newspaper does not react to being looked at, and the whole
argument for this reader is that it is a page rather than an application. The two
concessions are a headline that underlines under the pointer, which is the least a link
can do and still read as one, and the focus ring, which is not decoration.

The read control is therefore always present and always at half weight. A control that
exists only on hover cannot be found by somebody who does not already know it is there,
and cannot be reached by touch at all.

Marking read is optimistic against the query cache and reconciled on the response.

**Opening an article marks it read, however it was opened.** That means `onAuxClick` beside
`onClick` on every link to an article:

- A plain click and a modified one (cmd- or ctrl-click for a background tab) both dispatch
  `click`, so React's `onClick` sees them.
- **Middle click does not.** Browsers dispatch `auxclick` for it, and `onClick` maps only to
  `click` — so without `onAuxClick` the one gesture people use to open a stack of articles
  at once was the single way of opening one that left it unread. On a front page that is
  the common gesture, not an edge case.
- The handler checks `button === 1`, because `auxclick` fires for the right button too and
  raising a context menu is not reading.

Tests dispatch `new MouseEvent("auxclick", { button, bubbles: true })` rather than reaching
for `fireEvent.auxClick`, which is not in Testing Library's typed event map. `bubbles` is
load-bearing: React attaches its listener at the root, not to the element.

`summary` is sanitized HTML from the server and rendered with `dangerouslySetInnerHTML`.
That is safe **only** because sanitizing happens at ingest, in Go, once — see
[backend.md](backend.md#internalfeeds). Never sanitize here instead; a second sanitizer
is a second thing to be wrong.

## The page is irregular on purpose

Four things vary per article — the headline's face, the standfirst's size, whether the card is
boxed, and how wide it is — and the reason is **memory, not decoration**.

The page is fixed until the next one is composed, and the point of that is somebody being able
to come back to an article they half-remember. Identical cards defeat it: fifty things that
look the same give a reader one landmark, and "somewhere in the middle" is all they can recall.
A story that is visibly wider than its neighbours, or boxed, or set in a face nothing else on
the page uses, is a thing that can be looked *for*. Where an article sits is half of finding it
again; what it looked like is the other half.

Everything below follows from that, including how the variations are drawn.

### One seeded stream per card

`styleFor(editionID, articleID)` seeds a small generator (mulberry32 over an FNV-1a of the two
ids) and takes every draw off it in a fixed order: face, type size, box, line, width, ink,
inset, columns. Two properties, both wanted:

- **Stable within a page.** The seed is two ids that do not change while the page is up, so
  nothing moves on reload, in a second tab, or after something is marked read. A landmark that
  moves is not a landmark.
- **New on the next page.** An article that survives into tomorrow's edition is dealt a
  different hand, because the edition id changed. An article boxed in perpetuity would be a
  fact about the article rather than about the page it is on.

This replaced six values cut out of one 32-bit hash at hand-chosen offsets — `h >>> 4`,
`h >>> 8`, `h >>> 12` — which meant tracking which bits were spent and hoping two draws had
not overlapped. Successive draws off a generator are independent without anybody keeping
count. Every card makes every draw, including the ones it will not use: a card that skipped
its frame draws because it is unboxed would shift every draw after them.

### Boxes are drawn from a bag

`LINE_BAG` holds the line styles with nulls mixed in, and one draw decides both whether there
is a box and what it is drawn in. A bag rather than a rate plus a second draw, because the
ratio is then something you can count in the source rather than work out.

One card in five is boxed. It ran at half for a while, on the reasoning that a box is not one
mark — line, weight, ink and inset are all drawn separately, and on a real page 24 boxes came
out as 21 distinguishable objects. That much is true, and it still was not the right number:
at half, being boxed is as ordinary as not being boxed, so the box stops distinguishing
anything. Punctuation only works while most of the page is unpunctuated.

The border is mixed from `--ink-muted`, not from `--rule`. `--rule` is the paper's hairline
and already close to the paper, so mixing it down to a third left nothing on the page — the
faint end of the range was invisible and the range was two shades of almost-nothing. A palette
token rather than a colour of its own, so a box that is a faint warm grey on paper is a faint
warm grey on the dark page too; a value tuned against the light theme would vanish in the dark.

The padding belongs to the box and only to the box. It was briefly on every card, with a
transparent border on the unboxed ones, so a boxed story sat on the same gridlines as its
neighbours — but that gave every unboxed card an inset bought for nothing.

### Masonry, done inside the grid

A grid row is as tall as the tallest card in it, so a short story beside a long one left the
difference empty — measured at 29% of the page, with single voids up to 601px. `lib/masonry.ts`
packs it: `grid-auto-rows` is 8px and every card is told how many rows it spans, which lets
`dense` fill the space under a short card with a later one. That brought it to **13% empty,
worst void 181px**.

Nothing is positioned by hand, which is the point — the grid still decides placement, so the
drawn widths, the dense backfill and the full-width rules all keep working. A masonry
library would have had to reimplement each of them.

There is no CSS for this yet: `grid-template-rows: masonry`, `display: masonry` and `item-pack`
are all unsupported in Chromium 130, checked with `CSS.supports` rather than assumed.

**It is not non-deterministic**, which is how an earlier version of this document had it. The
same page at the same width with the same faces packs identically every time, because the
packing is a function of heights and the heights are a function of the content — reloading
reshuffles nothing. What a measurement pass costs is a *frame*, not repeatability: nothing has
a height until the browser has laid it out once, so the page settles one frame in. The widest
cards are at the top, so what is on screen at that moment moves least. Resizing does rearrange
things, and that is fine — a different width was always going to be a different page, and the
reader is the one doing it.

Two things it has to account for, both learned by getting them wrong:

- **`row-gap` must be zero.** A row gap applies between every one of the eight-pixel rows a
  card spans, so a forty-row card would carry forty gaps inside it. The space between rows is
  a `margin-bottom` on the cards instead — 2.75rem, and 2.5rem on a narrow screen.
- **The measurement must include those margins.** `getBoundingClientRect` stops at the border
  box, so a span computed from it leaves the margin outside the card's grid area and the next
  card packs straight against it. The page came out with the stories touching, top to bottom.

The `ResizeObserver` watches width only. Packing changes the grid's height, which would call
the observer straight back; it converges, but a loop that relies on converging is still a loop.

### Rows are further apart than columns

The gap between rows is bigger than between columns — 2.75rem against 1.75rem. They were equal,
which is what a grid of tiles wants rather than what a page of stories wants: two cards side by
side are separated by their column edges anyway, while two stacked have nothing between them
but the space.

It also settles the read control. That sits under its own story, and once cards began hugging
their content it was twelve pixels below its article and twenty-eight above the next one —
close enough to either to read as belonging to neither. It is six pixels now, against a row
gap of forty-four.

### A picture nobody has measured is drawn a shape; a measured one keeps its own

Two rules, and which applies depends on whether anything has looked at the file.

**Unmeasured** — the ordinary case for anything published in the last few minutes.
`aspect-ratio` is drawn per card from five steps: 5/3, 10/7, 5/4, 10/9, 1/1, the height running
from three fifths of the width to square. Everything used to be 16:9, which is a television's
shape and nothing else's, and a page of photographs cut to one ratio reads as a catalogue.
Landscape through to square and no further: these are crops of other people's photographs, and
turning one portrait would be recomposing a frame its photographer chose. `object-fit` varies
too, three `cover` to one `contain` — two honest answers to the same question when nobody here
has seen the picture. It pays off in an unplanned way on comics, which `cover` had been
cropping.

An unmeasured **lead** keeps 21/9 instead. It runs the width of the page, and the ladder goes
up to square, so a square guess there is a picture as tall as the page is wide above the story
it belongs to.

**Measured** — the picture's own ratio, filled. The ladder is what to do knowing nothing, and a
measurement is not nothing: a photograph that is nearly square cut to five-by-three loses a
third of itself and nobody here has looked at it to know whether the third mattered. This
applies to the lead too. The lead used to be given `aspect-[21/9]` whatever it measured, and it
never took effect — the inline `aspect-ratio` overrides the class — so the class was dead and
the comment above it was wrong for as long as both existed.

Two bounds sit over all of it:

- **Nothing is taller than 70vh**, measured or not, in any slot. A card whose picture fills the
  window stops being a card in a page of them, and the headline it belongs to is off screen
  when it is read. `vh` and not `dvh`: `dvh` follows a phone's toolbar as it hides, which would
  change every capped picture mid-scroll, and the masonry sets its row spans from measured
  heights — so that is not a nicer number, it is a page that reshuffles under the reader.
- **A picture more than two and a half times taller than wide is drawn square and filled.** A
  sliver cropped to fit a column is not a picture of anything. Squared rather than clamped to
  the bound, which sounds gentler and is not: a 1:4 shot held at the tallest allowed shape is
  still very nearly a 1:4 shot.

There is deliberately **no bound at the wide end**, and there was one — anything past 5:2 was
squared, which meant a panorama, the one shape whose entire point is its width, was cropped
hardest. A band is drawn as whatever it is. It is also never set beside a story: the aside
floor is nine rem, so a 10:1 picture in a 367px column, which wants to be 37px tall, would be
cropped to 144px and lose three quarters of the file to fill a hole it was never going to fill.

**Class names are written out in full.** Tailwind generates a class only if it can find the
whole name in the source, so `object-${fit}` produced neither `object-cover` nor
`object-contain` — the images briefly had no `object-fit` at all and were stretching. Checked
in the built CSS, not assumed.

### Rules between bands

A `.page-rule` spans all eight tracks. A newspaper breaks its page into bands, and the bands
are what make it scannable at arm's length. It is structural too: nothing can sit beside a
full-width element, so a rule closes the band above and bounds how far `dense` reaches when it
backfills — a hole near the top stops being filled from four screens down.

**A chance, not a cadence**, and a small one: two per cent per card, which is one or two on a
ninety-article page. Both halves of that were wrong at first. It began as a rule every five to
nine cards, which put eight of them on a fifty-card page *at even intervals* — and even
intervals are exactly what a page is trying not to be, since regular rules read as a table's
gridlines. Then it was a ten per cent chance, which is irregular but still a rule every ten
cards: often enough that it stops being a mark anybody sees, and a mark nobody sees divides
nothing.

The floor on band length lives in `ReaderPage`, not in the draw. A card caught between two
rules sits alone with three quarters of its row empty, and the per-card draw cannot know where
the last rule fell — so, like the no-two-faces-in-a-row rule, it is enforced over the sequence.

It gets `margin-block: 1.5rem` on top of the grid's own gap, so there is about twice the clear
space around it as between two ordinary cards. Without that it is a hairline sitting at exactly
the distance every other card is from its neighbour, which reads as one more edge rather than
as a break — a break has to be bigger than the gaps it divides.

### Sixteen tracks, and a width drawn per card

The grid is sixteen tracks. A card takes 4 to 12 of them, or all 16. Never 13, 14 or 15, and
never fewer than 4.

It used to be a four-track grid with widths of 4, 2 and 1. All three divide four, so every row
tiled exactly, `grid-auto-flow: dense` never had a hole to backfill, and it was measured to have
never moved a single card. Forty-six of fifty-one cards were the same width. That is a
spreadsheet with headlines on it.

Then it was sixteen tracks with four widths — 16, 12, 8 and 4 — which was better and still had
every card sitting on one of four sizes, all multiples of the narrowest.

Now the width is **drawn per card** rather than implied by the slot. The slot gives a floor and
the card's own seeded stream stretches it by nought, one or two tracks:

| slot | floor | widths |
| --- | --- | --- |
| lead | 16 | 16 |
| wide | 10 | 10, 11, 12 |
| feature | 7 | 7, 8, 9 |
| standard, brief | 4 | 4, 5, 6 |

The three stretching ranges meet without overlapping or leaving a hole, so every width from 4 to
12 is reachable and two features on a page are not the same width by default.

Two rules keep it honest:

- **Nothing is narrower than a quarter of the page** — four tracks. A card of one or two is a
  hundred pixels of squeezed text, and there is no story worth reading in that.
- **Nothing is wider than the page minus the narrowest card** — twelve. A card of 13, 14 or 15
  strands a remainder that nothing can ever fill.

#### What the second rule does not cover

No *single* card strands a gap, but a row can still add up to one — 6 + 7 leaves 3, and nothing
is narrower than 4. Measured on a ninety-article page at 1240px, against the even-only widths
that tile perfectly:

| widths | interior holes | unfillable interior | ragged right edge |
| --- | --- | --- | --- |
| drawn, 4–12 and 16 | 21% of slices | 7% | 59% |
| 4, 8, 12, 16 | 20% | 0% | 0% |

Interior holes do not move. What the drawn widths add is a ragged right edge — the last card in
a band stopping short of the margin — which is what a page of set type looks like anyway.

#### Why sixteen and not eight

Eight tracks were tried, with widths 2 to 6 and 8. Same rules, same shape, and the same 20% of
interior holes — but a stranded track was an eighth of the page rather than a sixteenth. At
1240px that is 149 pixels of white against 75.

**Nothing can absorb the remainder, and that was measured rather than assumed.** A card spans
whole tracks, so there is no half-track. `justify-content` looked like the answer and is not: on
a grid of `1fr` tracks it does nothing whatever, because `1fr` has already taken every pixel
there was to distribute — `stretch`, `space-between` and no value at all produce identical
layouts. With fixed tracks, where there *is* free space, it widens the gaps between tracks
rather than the cards, and still leaves the leftover track at the end. It also applies to the
whole grid rather than per row, and grid has no concept of "this row's leftover".

Flexbox would do it, with `flex-grow` on a row of cards. It cannot be used here because there is
no row: `dense` places each card at the first position it fits over eight-pixel rows, so two
cards side by side routinely start at different heights. There is no set of elements that *is* a
row to grow.

Twelve is what makes any of it irregular. A row holding one has four tracks left, which only a
single column can take, so the grid reaches past the next article to find one — and that
reaching is the whole mechanism.

Cards use `align-items: start` so they hug their content. It moves no whitespace — a row is
still as tall as its tallest card — but a boxed story that stretched would draw a frame around
four lines and a foot of nothing.

### Where the wide ones go

The page opens with weight: the first card is never a single column, and which of the three
wider slots it gets is drawn, so the top is not the same shape every time.

After that, width is **scattered rather than spent**. The old rule gave the widest slots to
ranks one through four and left everything below identical, so the page ran big to small and
then stayed small for forty cards. Rank is draw order out of a weighted sample, not an editor's
judgement, so there was never anything to preserve by stacking prominence at the front.

Two wide cards adjacent in reading order is fine and is not prevented. Adjacent there does not
mean side by side — `dense` decides that, and a twelve beside a four is a row that tiles
exactly.

**How wide each one goes is a separate question, and the picture answers it.** A band wider
than 5:2 is drawn towards `lead` and `wide`; an upright picture is always `feature`, the
narrowest of the three, because width costs it height it cannot spend. And a band is never left
in a column whether or not the page picked it out — `feature` is a floor there, not a prize,
since a 10:1 picture in a quarter of the page is sixty-five pixels of photograph over a
headline. All of that is decided server-side with the rest of the slot; see
[edition.md](edition.md#which-cards-are-widened-and-how-wide).

### Up to four columns for the widest bodies

A standfirst running the full width is a line of prose seven hundred pixels long, and the eye
loses its place coming back. Newspapers split the story rather than narrowing its column, which
spends the width instead of throwing it away with a measure cap.

The count is drawn from a bag — half the cards one column, a quarter two, and three and four
rare — and then held to what the slot is wide enough to carry: a lead four, a wide three, a
feature two, anything narrower one. Two on a phone whatever was drawn.

The bag is there because the first attempt drew one to seven and clamped to four, which reads
as the same idea and is not: four of the seven outcomes land on four, so more than half of every
long story came out in the widest setting there is. A bag cannot go wrong that quietly, because
the ratio is the thing you are looking at.

## Type size is unlocked from column width

A standfirst's size used to come from its slot: wide cards large, narrow cards small. That
gave every page exactly two sizes of prose on it, arranged biggest-first — a template rather
than a page.

Now the size is drawn per article from a four-step ladder in `styles.css`, by `proseStep` in
`lib/voice.ts`, and the slot only sets a **floor** under it. So a one-column card can be set
larger than the feature beside it, which is where the texture comes from, but a lead cannot
be set at fifteen pixels — a wide card in small type does not read as a choice, it reads as a
stylesheet that failed to load. Width raises the floor and never lowers the ceiling, which is
one `max()` in one place.

The ladder's range is bounded by what the *narrowest* card can carry, because any step has to
work in any slot now that the two are only half tied together.

`proseStep` takes a different slice of the same hash the voice takes, so a face and a size are
not locked together either — every Oswald headline over the same size of prose would be its
own pattern. It is a pure function of the id for the same reason the voice is: the layout is
fixed server-side so that where an article sits is how somebody remembers where they were, and
type that resized on reload would undo that.

The body face itself does **not** vary. Display faces vary story by story and body type is the
paper's single voice — that is what a newspaper does, and six body faces on one page is a
ransom note. It would also mean downloading six more families at a text weight to print the
one thing on the page that is read rather than scanned.

## Sliders save on release

A range input driven straight from server state does not work, and the way it fails is
worth writing down because both halves look reasonable on their own.

Binding `value` to the stored number means the thumb cannot move until a round trip
finishes: it travels one step and snaps back. Disabling the input while that request is in
flight — the obvious way to stop a drag firing a request per pixel — makes it worse, because
the control goes dead mid-gesture and the browser abandons the drag entirely.

`Slider` holds the position in local state while a finger is on it and commits on release:
`pointerup` for mouse and touch, `keyup` for the arrow keys, `blur` for anything that steals
focus mid-drag. One request per gesture, and the number beside the track follows the thumb
rather than the server.

Every slider goes through it — feed and tag priorities, and the size of a page.

## A feed's settings are a dialog, not a disclosure

Everything about one feed — its name, what it is filed under, how far back a page reaches
into it, and stopping following it — lives in one dialog opened from the name.

It was an accordion under the row, which put a page of controls between one feed and the
next and made the list unscannable while any of it was open. A feed's settings are
something you go and change, not something you want unfolding in the middle of a list you
are reading.

Priority is the exception and stays in the row: it is a dial somebody nudges while looking
at the whole list, not something they open a feed to change.

**Nothing in the dialog is written until Save.** Every control holds its value locally and
one request carries the lot. A dialog that saves as you touch it has no way to be closed
without consequences, and each toggle becomes a write the list underneath has to catch up
with — which is how a control ends up looking dead: the request went, and the dialog went
on showing the copy it opened with.

Cancel, Escape and the backdrop all leave the feed exactly as it was. Saving with nothing
changed is a close, not a write.

An empty name is refused rather than treated as a reset — a feed with no name at all is
not something anybody means to ask for. The way back is a dashed inline **Use publisher
title** under the field, which fills the field rather than saving, so it is a suggestion to
look at rather than a decision already taken. It disappears once the field already says
that.

A name equal to the publisher's title is stored as **no override at all**, whether it was
typed out or came from that button. There is no reason to keep an override that says the
same thing as the title it overrides.

## A recovery address is proved in a dialog, not typed into a field

Two steps, and only the second writes anything: an address, then a code that came back to it.
The dialog is what makes that possible — a field with a Save beside it has nowhere to put
"prove this" that is not also "commit this".

The address locks once a code is away, because changing it would leave the code pointing at
the old one, which reads as the flow being further along than it is. An attempt already in
flight is named on the page and resumed when the dialog reopens, rather than starting somebody
over on a code they are already holding.

With no relay configured the button is disabled and the page says why. A button that fails for
reasons the person pressing it can do nothing about is worse than no button.

## The masthead carries who you are, not the way out

The right of the band is `Settings`, `Admin` when it applies, and then a person icon with
the account's name, linking to `/manage/account`.

Sign out used to be that last item. It sat one slip away from being pressed by somebody
aiming at the link beside it, and in exchange for that risk it told nobody anything. It now
lives on the account page, which is where the other things you do to an account already are
— and the masthead spends the same space saying which account you are signed in as, which
is a question people actually have on a self-hosted instance they share with a household.

The manage island's masthead lost its `Signed in as …` subtitle in the same move. The band
was going to say the name either way, and twice is not better.

## A dialog holds everything behind it still

Opening one locks the scroll of whatever is behind it, and `scrollLock.ts` is the whole of it:
a counter per element, the previous inline styles remembered and restored, and the scrollbar's
width added as padding so the page does not shift sideways as it disappears.

Counted per element rather than a single global flag, because dialogs nest — a feed's settings
opens a confirmation, and the confirmation closing must not unlock the page while its parent is
still open. `Modal` puts its own `<dialog>` into a context, so a dialog opened inside another
locks *that* one rather than the document.

It was measured before it existed: a dialog over the front page let the page behind it scroll
600px on one screen and 894px on another, so closing it left somebody somewhere they had not
navigated to.

A scrolling dialog also keeps its footer fixed and puts the rule on the body, inside the
scroll — otherwise the separator scrolls away from the buttons it separates. The bottom space
in a scrolling list is a spacer element and not padding on the list: Firefox and Safari drop a
scroll container's bottom padding, so the padding is simply not there when it is most needed.

## One switch with three positions, not two lists

A page's filter is one list of tags and one of feeds, each name carrying a `StanceSwitch`:
left to drop it, right to take it, and the middle — where everything starts — to say nothing
about it.

It was going to be two lists, an included and an excluded, with each name disabled in the
other. Three positions on one control says the same thing without the second list, and without
the rule that keeps them from disagreeing: a switch cannot be in two positions, so a tag cannot
be included and excluded. The database says the same thing with a primary key.

Red left and green right, and both are the quiet variants of the accent pair — a switch is not
an alarm. The knob positions are measured from inside the border rather than the outer box,
which is why they are 1, 17 and 33 rather than thirds: against the outer box the knob sat three
pixels below centre. The neutral knob is `ink-muted` and not the rule colour, which was 1.69:1
against the track in dark mode and effectively invisible.

Beside each feed the list says whether it currently lands **in** or **out** under the tag
rules, tinted with the same quiet green and red. Otherwise "this one as well" and "this one
never" — the two useful things anybody wants to say about a publisher — would require working
out the tag rule first.

## A feed is shown before it is followed

`POST /api/feeds/preview` fetches a feed and returns its last ten articles without subscribing
to anything, and the dialog shows them the way a page would: the same card styling, in their
own scroll window so the Add button never has to be scrolled to.

A title and an address are not a description. A site offering "Posts", "Comments" and "Notes"
is three plausible names and one right answer, and the alternative is following one to find out
and then unfollowing it again.

The pictures there are shown whole, not cropped — `max-h-80`, `object-contain`. The page crops
to a drawn shape because a page is a composition; this is a sample, and a comic with its
punchline cut off answers the question wrongly.

**It is also where the feed gets filed**, in the one case where Add actually subscribes —
a single feed found from an address. Ten articles is the moment somebody knows where a feed
belongs, and adding it untagged meant finding it again in the list afterwards to say what they
already knew. The chips sit above the scroll window, not inside it: they are part of the same
decision as the Add below, so they must not be something you scroll away from.

The other two callers pass no `filing`, and both absences are load-bearing. Over the picker
each row already carries its own chips, and a second set would be two answers to one question.
For a feed already followed there is no Add at all, and the filing belongs to that feed's own
dialog.

`TagChips` is that row — the same component in the preview and in a feed's dialog, because
filing is one gesture wherever it happens. `FeedPlan`'s rows keep a second kind of chip,
dashed, for a tag a source named that nobody here has yet; that belongs where a list arrived
carrying a taxonomy somebody has to accept or refuse, and nowhere else.

Both chip rows offer **New tag**, which opens `NewTagDialog` over whichever dialog asked and
ticks what it makes. Without it, "no tags yet" is a dead end reached at precisely the moment
somebody knows the answer.

## A tag is three decisions, asked once

`NewTagDialog` takes the name, where it sits, and how often it appears. The field-and-a-button
at the top of the tags page could only take the first, so every tag arrived on its own at the
default weight and the other two were set afterwards from a row somebody had to find again.

A dialog rather than a wider form above the list, because the list *is* the page: three
controls sitting open at the top of it are three controls in the way, every time somebody comes
here to change a priority. The row still carries all three for changing one's mind later.

## Every dialog's buttons sit in the same place

`Modal` takes a `footer` and lays it out; no dialog arranges its own row. Six of them
arranging their own produced six arrangements — primary left, primary right, one pair pushed
apart by `justify-between` — and which button was Save became something to look for rather
than something to know.

The order is **whatever dismisses, then the one that acts**, along the right. An action that
belongs to neither group takes `mr-auto` and sits on the left: unfollowing a feed, saving the
share list as a file.

Buttons passed as `footer` rather than at the end of `children` for the same reason the
layout lives in `Modal` — the row is a property of being a dialog, not of what this
particular dialog is about.

## Sixteen-pixel form controls on a phone

iOS Safari zooms the page in when a control whose text is under 16px takes focus, and does
not zoom back out. Every input here is `text-sm`, which is 14px, so tapping any field left
somebody scrolled sideways on a page they then had to pinch their way out of.

One unlayered rule in `styles.css` sets every text-taking control to 16px below the `sm`
breakpoint. Unlayered is load-bearing: Tailwind emits utilities inside `@layer utilities`,
and an unlayered rule beats a layered one regardless of specificity — which is what lets one
rule override `text-sm` everywhere without `!important` or a change at every call site.

The other fix is `maximum-scale=1` in the viewport meta, which works by taking pinch-zoom
away from everybody permanently. Not a trade worth making to save two pixels of type.

## Components

- `src/components/ui/` — primitives: `Button`, `Field`, `Select`, `Segmented`, `Modal`,
  `Card`, `Alert`. Small and close to unstyled: they carry layout and state, not a look.

  `Select` exists because there were three hand-styled `<select>`s in three components, and
  three copies of a style are three chances for one to drift. It is also where the asymmetric
  padding lives: a browser draws the arrow *inside* the padding box rather than beside it, so
  even padding puts it hard against the border.

  `Segmented` is the same question when there are only two or three answers and the answer
  changes what the rest of the form is — a select hides its options behind a press, and seeing
  both at once is most of what makes such a form legible. It takes plain strings and an index,
  because the caller already has the meanings in an array to render from; `null` means nothing
  chosen yet, and an index past either end is clamped rather than lighting nothing. `block`
  fills the width, which is what stacked controls in a form want and what a control in a row
  of other controls does not.
- `src/components/` — shared composites.
- `src/apps/<island>/` — everything belonging to one island.

Icons are inline SVG components, one per file, not an icon font and not a package.

## Testing

Vitest plus Testing Library, tests beside sources.

A test renders a subtree against a recorded transport through `<ApiProvider>` — never
`vi.stubGlobal("fetch")`, which is global state two tests will eventually fight over. That
this is possible at all is the reason the dispatcher is injected rather than imported as a
module singleton.

Worth testing: the sampler's output rendering into the right slots, read-mark
optimism and rollback, tag cycle refusal surfacing as a message, and the invitation page's
three states — valid, expired, already accepted.
