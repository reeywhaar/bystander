# Screenshots

The images in the project README. Real captures of the real reader — only the publications
behind them are invented.

| file | what it shows |
| --- | --- |
| `frontpage.png` | a front page: the slots, six headline faces, a couple of read cards |
| `feeds.png` | the feeds somebody follows, and what each is worth to them |
| `feed.png` | one feed's dialog: its name, its sections, how far back a page reaches |
| `pages.png` | the front pages somebody keeps, and the controls belonging to one of them |
| `page.png` | one front page's dialog: what it is called, where it lives, what it draws from |
| `read.png` | what has already been read, which is the only list in the product |
| `login.png` | the sign-in screen: an invitation, never a default password |

## Regenerating

```sh
docs/screenshots/capture.mjs
```

That builds the frontend and the binary, starts eight stand-in publishers and a reader
against them, subscribes to all eight through the reader's own API, makes a second front page
filtered to one section, composes both, marks a few articles read, drives headless Chromium
over the DevTools protocol, and overwrites the PNGs here. Everything it starts is stopped again on the way out, including on failure.

Needs `go`, `node`, and Chromium or Chrome. No Docker, no npm packages beyond the
frontend's own, and no Playwright — Chromium has a headless mode and Node has `fetch` and a
WebSocket client, which between them is the whole driver.

This was a bash script. It is Node because a shell script that has to parse JSON grows a
`node -e` helper to do it, and once the awkward half of the work is already in JavaScript the
shell is only there to hold the easy half badly.

It does need to bind `:80`, because that is where the reader listens and the port is not
configurable. Fine on macOS; on Linux that means root, or
`sudo setcap cap_net_bind_service=+ep` on the built binary. It takes a minute or so, most
of which is one deliberate wait — see below.

Five knobs, and somewhere to put the result:

```sh
WIDTH=1400 docs/screenshots/capture.mjs    # front page width, default 1240
HEIGHT=1800 docs/screenshots/capture.mjs   # how much of it to photograph, default 1500
VIEW=1000 docs/screenshots/capture.mjs     # how tall it thinks its window is, default 900
NARROW=960 docs/screenshots/capture.mjs    # everything else, default 880
THEME=dark docs/screenshots/capture.mjs    # default light
```

`OUT` moves where they land, so a look at the dark theme need not overwrite the committed
set: `THEME=dark OUT=/tmp/shots docs/screenshots/capture.mjs`.

**Do not drop `WIDTH` below 1100** without meaning to. That is where the grid folds from
four columns to three, and again at 820 to two — and a set of screenshots in which the
front page is two columns wide sells the whole idea short, because the layout *is* the
product.

The second front page is not decoration. Without it the reader shows no tab strip at all —
one page is not a set of tabs — so a capture with a single page would quietly photograph the
feature as though it did not exist. `capture.mjs` fails rather than continuing if that page
composes nothing.

The front page is the one shot taken at a fixed height rather than fitted to its content.
Twenty-eight articles is several thousand pixels of page; captured whole it becomes a
ribbon that a README renders two inches wide.

`HEIGHT` and `VIEW` are separate, and that is the point of them. `VIEW` is the window the page
believes it is in; `HEIGHT` is how far down the page the camera reaches. They used to be one
number, and the page believed it — no picture is drawn taller than 70vh, so telling it the
window was 1500px tall made every capped picture 1050px, half again what anybody with a real
screen sees, and the opening card's picture pushed its own headline out of the shot.
`captureBeyondViewport` is what allows the two to differ.

**The opening card is chosen, not accepted.** The capture re-rolls until the page opens on a
`wide` carrying the arcs plate with its picture above the story rather than beside it — see
`capture.mjs` for why each of the three matters — and falls back through weaker versions of
that rather than failing. Every draw it rejects is a real page; this is a photograph, and a
photograph is allowed to wait for the light.

## The pieces

**`stub.mjs`** — eight stand-in publishers on `:8811`, serving RSS, article pages and the
pictures on the cards. Node stdlib only.

Invented papers with invented stories, and that is deliberate rather than lazy: a
screenshot showing somebody else's headline is republishing it, and one showing an invented
headline under a real masthead is worse than that. The spread is chosen to show things the
reader actually does — The Wire Desk carries forty-two articles at priority 35, which is
the case the sampler exists for, and several articles carry no picture and no standfirst,
because those are laid out as briefs and a page of nothing but pictures is not the page
this reader produces.

The pictures are drawn rather than committed: the alternative is a licence to keep track of
or a folder of photographs with nothing to do with this project. They are abstract plates
in the reader's own two colours, and each carries its own `prefers-color-scheme` block —
an SVG in an `<img>` is a document of its own and honours it, so `THEME=dark` turns the
plates over with the page.

They are also deliberately faint. The first version was op-art: dense, high-contrast, and
the only thing on the front page anybody looked at, which is the opposite of what a picture
on a newspaper page does.

**`cdp.mjs`** — the DevTools protocol client, about forty lines of it. Shared, because
`capture.mjs` needs one `evaluate` of its own and two copies of a protocol handshake is two
things to fix when one of them is subtly wrong.

**`shoot.mjs`** — the capture sequence. Notable bits, all of which were bugs first:

- Every shot waits on `document.fonts.ready`. The headline faces are half of what these
  screenshots are for and they load with `font-display: swap`, so a capture taken a moment
  early is a capture of the fallback serif — and nothing else in the script would have
  noticed.
- It emulates `prefers-color-scheme` rather than trusting what headless Chromium reports,
  which is always light. `THEME` is therefore the one knob that decides what the whole set
  looks like.
- `fitHeight` shrinks the viewport *before* measuring, because a shell with a minimum
  height never reports a `scrollHeight` below the current viewport — measure at the height
  you are already at and you get that height back, padded with blank paper.
- `fitDialogHeight` measures the `<dialog>` itself and divides by `0.85`, because that is
  what its `max-h-[85dvh]` means: to show content of a given height whole, the viewport has
  to be taller than it by that factor. It used to look for the first descendant that scrolls
  and measure to the bottom of *its* children, which worked only while the sole such element
  was the last thing in the dialog — the page dialog has a tag picker capped at twelve rem in
  the middle of it, and everything below that, Save included, was cut off.
- Selectors are passed into the page as variables, never interpolated into a string. One
  containing a double quote turns the whole expression into a parse error, which surfaces
  as an immediate failure rather than a timeout.

**`capture.mjs`** — everything around it. Three parts are worth knowing about:

- **It waits for a fetch.** Adding a feed saves the articles that discovery already
  parsed, which is why there is a page to photograph at all — but it is the fetch job that
  records a *successful fetch*, and its first cycle is a minute after startup. Without the
  wait, every row on the feeds page says "not fetched yet" underneath a feed whose articles
  are on the front page.
- **It verifies the stub is its own.** A stub left running from an earlier attempt keeps
  the port, the new one dies with `EADDRINUSE` in the background where nothing notices, and
  the capture then quietly succeeds against stale content — which is exactly how a set of
  screenshots ends up showing something the code no longer does.
- **It asks the browser what was drawn.** Whether a card sets its picture beside the story is
  decided in the client from the edition's id and the article's, so the API cannot be asked and
  the browser starts before the page is composed rather than after. Recomputing that draw here
  would be a second copy of a rule in `voice.ts`, and it would go quietly wrong the first time
  one of them changed — which is also why `stub.mjs` publishes `/plates.json` instead of
  letting the capture work out a plate's composition from its id.
