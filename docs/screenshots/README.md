# Screenshots

The images in the project README. Real captures of the real reader — only the publications
behind them are invented.

| file | what it shows |
| --- | --- |
| `frontpage.png` | a front page: four slots, six headline faces, two read cards |
| `feeds.png` | the feeds somebody follows, and what each is worth to them |
| `feed.png` | one feed's dialog: its name, its sections, how far back a page reaches |
| `settings.png` | how often a page turns and how much is on it |
| `read.png` | what has already been read, which is the only list in the product |
| `login.png` | the sign-in screen |

## Regenerating

```sh
docs/screenshots/capture.sh
```

That builds the frontend and the binary, starts eight stand-in publishers and a reader
against them, subscribes to all eight through the reader's own API, composes a page, marks
a few articles read, drives headless Chromium over the DevTools protocol, and overwrites
the PNGs here. Everything it starts is stopped again on the way out, including on failure.

Needs `go`, `node`, and Chromium or Chrome. No Docker, no npm packages beyond the
frontend's own, and no Playwright — Chromium has a headless mode and Node has a WebSocket
client, which between them is the whole driver.

It does need to bind `:80`, because that is where the reader listens and the port is not
configurable. Fine on macOS; on Linux that means root, or
`sudo setcap cap_net_bind_service=+ep` on the built binary. It takes a minute or so, most
of which is one deliberate wait — see below.

Four knobs, and somewhere to put the result:

```sh
WIDTH=1400 docs/screenshots/capture.sh    # front page width, default 1240
HEIGHT=1800 docs/screenshots/capture.sh   # front page height, default 1500
NARROW=960 docs/screenshots/capture.sh    # everything else, default 880
THEME=dark docs/screenshots/capture.sh    # default light
```

`OUT` moves where they land, so a look at the dark theme need not overwrite the committed
set: `THEME=dark OUT=/tmp/shots docs/screenshots/capture.sh`.

**Do not drop `WIDTH` below 1100** without meaning to. That is where the grid folds from
four columns to three, and again at 820 to two — and a set of screenshots in which the
front page is two columns wide sells the whole idea short, because the layout *is* the
product.

The front page is the one shot taken at a fixed height rather than fitted to its content.
Twenty-eight articles is several thousand pixels of page; captured whole it becomes a
ribbon that a README renders two inches wide. `HEIGHT` is set to show the lead, both
features and the top of the standard row, which between them is every slot the grid has.

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

**`shoot.mjs`** — the Chromium driver. Notable bits, all of which were bugs first:

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
- `fitDialogHeight` measures the dialog's children rather than its `scrollHeight`. Once the
  viewport is tall enough the content stops overflowing and `scrollHeight` collapses to the
  viewport height.
- Selectors are passed into the page as variables, never interpolated into a string. One
  containing a double quote turns the whole expression into a parse error, which surfaces
  as an immediate failure rather than a timeout.

**`capture.sh`** — everything around it. Two parts are worth knowing about:

- **It waits for the poller.** Adding a feed saves the articles that discovery already
  parsed, which is why there is a page to photograph at all — but it is the poller that
  records a *successful fetch*, and its first cycle is a minute after startup. Without the
  wait, every row on the feeds page says "not fetched yet" underneath a feed whose articles
  are on the front page.
- **It verifies the stub is its own.** A stub left running from an earlier attempt keeps
  the port, the new one dies with `EADDRINUSE` in the background where nothing notices, and
  the capture then quietly succeeds against stale content — which is exactly how a set of
  screenshots ends up showing something the code no longer does.
