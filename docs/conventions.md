# Conventions

## Commit messages

**One line. No body, no trailers, ever.**

A full declarative sentence saying what the change accomplishes. Capitalized, no trailing
period, no prefix, no conventional-commit tag, no ticket number, and no `Co-Authored-By`.

```
Let an administrator revoke an invitation that nobody has accepted
Bind a read mark to its edition, so discarding one discards the other
Stop a prolific feed taking the page when the dice agree with it
Say which feed failed, and for how long it has been failing
```

Two clauses joined by "so" or "and" are common and welcome — the second says why the first
was worth doing. What the message must not be is a label: not `fix: feeds`, not
`update store`, not `wip`.

The single line is not a length limit fighting the explanation; it is where the
explanation goes. If a change needs three paragraphs of justification, those paragraphs
belong in a comment beside the code they justify, where somebody reading that code will
actually meet them — a commit body is read once, by whoever runs `git log`, and never
again.

A change that genuinely cannot be said in one sentence is usually two changes.

## Comments

Comments explain **why**, never what. A comment restating the line under it is noise; a
comment recording the reason a line is written the way it is prevents somebody
"simplifying" it back into a bug.

Package doc comments are expected and are the right place for the argument a package
exists to make — not what the package contains, which is readable, but why it is a package.

When a decision has a real alternative, say what the alternative was and what it cost.
That is the sentence that is impossible to reconstruct later.

## Identifiers

Prefix plus 26 characters of Crockford base32 over 16 bytes: a 6-byte big-endian
millisecond timestamp then 10 random bytes. ULID's layout, so ids sort chronologically
and `ORDER BY id` is a time order. Crockford's alphabet omits `I`, `L`, `O` and `U`, so
an id cannot be misread between similar glyphs or accidentally spell something.

| prefix | entity |
| --- | --- |
| `p_` | principal |
| `i_` | invite |
| `t_` | tag |
| `f_` | feed |
| `s_` | subscription |
| `a_` | article (feed item) |
| `e_` | edition |

Ids are opaque and never parsed back. The prefix is for the human reading a log line.

## Time

- `main.go` pins `time.Local = time.UTC`. The image carries no zone database, so a named
  `TZ` would resolve to UTC regardless — pinning it makes that a decision rather than a
  side effect of what the runtime happens to contain.
- Stored as **Unix seconds in an `INTEGER` column**. Not text, not milliseconds.
- Rendered in the reader's zone by the browser, and only there.

## Go

- `gofmt` clean; CI fails on anything it would rewrite.
- Tests beside sources as `*_test.go`. No `tests/` directory.
- Errors wrap with `%w` and name what was being done: `fmt.Errorf("fetch %s: %w", url, err)`.
- One error vocabulary, in `internal/store`, that handlers map onto status codes:
  `ErrNotFound`, `ErrConflict`, `ErrInvalid`. Three, because a fourth would need a status
  code nobody has wanted yet.
- `context.Context` first parameter on anything that can block.
- Injectable clocks: a constructor takes `now func() time.Time` so tests drive expiry
  without sleeping.

## TypeScript

- Prettier, default settings, `npm run format`.
- Components are `PascalCase.tsx`; everything else is `camelCase.ts`.
- Tests beside sources as `*.test.ts` / `*.test.tsx`.
- Imports use the `@app/*` alias, never `../../`. An import that names where a module *is*
  stops depending on where the importer sits.
- `strict`, `noUncheckedIndexedAccess`, `noUnusedLocals`, `noUnusedParameters`.

## Naming things in the product

The words below mean one thing each, in code, in the API and in the interface:

| word | meaning |
| --- | --- |
| **feed** | A URL that publishes items. Global, shared between subscribers |
| **subscription** | One person's relationship to a feed: their priority, their tags |
| **item** | One article as the feed published it |
| **front page** | One of a person's pages: what it is called, where it lives, what it draws from |
| **Front Page** | The one front page everybody has, at `/`. Capitalised, because it is a name |
| **edition** | The fixed set of items on a front page right now |
| **slot** | How prominently an item is laid out: lead, feature, standard, brief |
| **principal** | An account, admin or user |

"Article" is the reader-facing word for an item, and appears only in interface copy.
Never "post", never "entry", never "story".

### front page, Front Page, edition

Three words that are easy to run together, and the distinction is worth holding.

A **front page** is the standing thing: its name, its address, how often it is composed, how
much it holds, and which feeds and tags it may draw from. A person has one or more. It is
`pages` in the database and `Page` in Go, because "page" is the shorter word and the table is
not the place for prose — but the word out loud, in comments and in interface copy, is *front
page*.

The **Front Page** is the one everybody has and nobody can remove or rename. It is served at
`/` rather than at `/f/:slug`, its slug is the empty string, and `is_main` marks it. Written
capitalised, because at that point it is a name rather than a description — the same way a
newspaper's front page is *the* front page and its other sections are called something.

An **edition** is one composition of a front page: the fixed set of articles it is showing
now. Front pages persist; editions are replaced. "A new page has been composed" is about an
edition; "your Front Page" is about the thing that keeps being composed.

Never "feed page", never "channel", never "board", never "view". A reader has front pages.
