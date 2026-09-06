# Relays

Some publishers refuse this instance. Geo-fenced, behind a bot wall, or rate-limiting the
address it fetches from — the shape is the same: the feed is fine, and *we* are the problem.
A relay is somewhere else to ask from.

**Somewhere else is the whole requirement, and it is easy to get wrong.** A proxio container on
the same compose network as bystander goes out from the same address, so a publisher refusing
this instance refuses the relay identically. It tests fine against anything unrestricted and
never once helps with the feeds it was set up for. A relay has to be on a network the publisher
will answer — a host in the region the feed is licensed for, a tunnel that comes out somewhere
else.

An administrator configures them at `/admin/relays`. Nothing is on by default and nothing is
inferred: an instance with no relays fetches directly or not at all.

## The order a request is tried in

```
1. Whatever reached this publisher last time, if that relay is still
   configured and switched on.

2. Directly.

3. Each relay in turn, highest priority first.
```

Step 2 always happens when there is no fresh route, and that is deliberate. **A relay is a way
past a refusal, not a way of doing business.** Routing everything through one would put a third
party in front of every publisher for the sake of the handful that block us.

Step 1 is what stops that being expensive. A publisher that blocks this instance blocks it every
time, so on a feed checked hourly the direct attempt is one request an hour spent confirming
something already known.

### What counts as a refusal

Only answers that might be about *who is asking*:

| | |
| --- | --- |
| a transport error | DNS, refused, timed out, TLS — nothing was reached at all |
| `403 Forbidden` | the ordinary geo-block and bot wall |
| `451 Unavailable For Legal Reasons` | |
| `429 Too Many Requests` | this address specifically |

Everything else is the publisher's answer about the URL rather than about us. A `404` is missing
from every address and a `500` is broken from every address; retrying those through each relay
would cost a request per relay per fetch cycle, forever, to arrive at the answer already in hand.

### What is reported when nothing works

The publisher's own answer, not the last relay's. "The feed answered 403" is something an
operator can act on; "the relay in Frankfurt answered 502" is about the relay, and recorded
against the feed it would be shown beside it as though the publisher had said it.

### Priority

0..100, highest tried first, defaulting to 100 — the same scale and the same direction as a
feed's priority, because two numbers in one product that both run 0..100 and disagree about
which end is which would be a small cruelty. It is not the same *kind* of number: a feed's is a
probability of being drawn, this is an ordering.

One thing it does not borrow. A feed at 0 never appears; a relay at 0 is tried last. Never is
the switch, which keeps its credential so it can be switched back on.

## The two kinds

They are not variations on each other — they work at different layers, which is why the form
asks for different things.

| | proxio | SOCKS5 |
| --- | --- | --- |
| what changes | the request's URL | the connection under it |
| address | `https://proxio.example.com` | `socks5://socks.example.com:1080` |
| credential | one opaque token | a username and a password |
| client | the ordinary one | one per endpoint, kept for reuse |

**proxio** is [reeywhaar/proxio](https://github.com/reeywhaar/proxio): `GET /proxy?url=…&token=…`,
which fetches the address and hands back what it got. The stored address is the host only —
the path is this program's business, so pasting the whole example URL still works and the
credential in it is moved out of the address rather than left on screen.

It also sets `X-Proxio-Error` when a failure is its own rather than the target's, and that
header is load-bearing. Without it a relay refusing a stale token (`401`) and a publisher
demanding a login (`401`) are indistinguishable — and since `401` is not a status worth
retrying, the relay's own refusal would be handed back as the publisher's answer, recorded
against the feed, and stop every relay after it from being tried. One mistyped token would
quietly break every feed that needed a relay.

**SOCKS5** replaces the dialer. The request is untouched, so a response coming back through one
already points at the publisher. `socks5h` is accepted and behaves identically: the underlying
dialer sends the hostname rather than resolving it locally, which is socks5h's semantics, so the
scheme names the behaviour rather than selecting it.

Clients are cached per endpoint so connections can be reused, keyed by what the connection
actually depends on — address, username, password, timeout — rather than by the relay's id. An
id would be the tempting key and would be wrong exactly when it mattered: correcting a password
would go on using the old one out of the cache.

## Remembering what worked

`proxy_routes` in derived.db maps a registrable domain to the relay that last reached it.

Keyed by **eTLD+1 rather than by host**, because a publisher serving its feed from `www` and its
pictures from an images subdomain is one publisher with one block, and learning those separately
means paying for the lesson twice. eTLD+1 is what makes that generalisation safe — it groups
`foo.example.com` with `bar.example.com` and does not group two unrelated sites sharing `.co.uk`.

In derived.db because it is not a setting, it is something the machine noticed. Losing it costs
one failed direct request per domain. It also means no foreign key to `proxies`, which lives in
main.db, so a row naming a deleted relay is expected rather than impossible.

A route is thrown away — and the ladder starts from the top — when it names a relay that has
been deleted or switched off, when the list will not read, or when that relay fails.

### A route is permanent, and why that is right

There is no expiry. A route records the way to reach a publisher, and for the case this exists
for that does not lapse: "available in the US only" is the publisher's licensing, not a state
that lifts. Re-probing the direct request on a timer would spend one failed request per blocked
domain per period — and not always the cheap kind, since a block that manifests as a silent drop
burns the whole thirty-second timeout — to re-learn something that changes on the order of never.

What ends a route is **the relay failing**. It is dropped there and then, and the very next thing
tried is the direct request, so a block that has lifted is noticed the first time the relay has a
bad moment.

That covers everything being wrong. It does not cover an operator knowing something the instance
cannot — a publisher has lifted a restriction, a relay has moved countries, the whole thing was
set up to try and is now in the way — so each relay has a **Reset**, which forgets every
publisher learned through that one and leaves the relay in place. The list says how many that is,
because a relay carrying nothing and a relay carrying forty are different decisions. Deleting a
relay takes its routes with it.

### The one refusal that is not learned

A route can only ever be created when the direct request failed **and** a relay succeeded. A
transient fault fails both ways — a publisher having a bad hour refuses the relay too — so
nothing is written down. Every refusal that gets as far as creating a route is therefore
structural by construction.

With one exception. `429 Too Many Requests` is counted per caller, so the relay sails through,
and the instance would learn a permanent lesson from a temporary one: an afternoon's rate limit
becoming a route through somebody else's machine forever. A `429` is relayed for that fetch and
not remembered.

## Credentials

A token is write-only, exactly like the SMTP password. It is never sent to the browser, so the
list cannot hand one back out and an empty field on save means "the one already stored" — which
is what makes it possible to correct a label without retyping a secret nobody can see.

Two places would otherwise write one down, and both are guarded:

- **`FinalURL`.** A response relayed through proxio has a `Request` pointing at the relay, whose
  address ends `?url=…&token=…`. Read as the address arrived at, that credential would be stored
  on the feed and used as the base for resolving every relative link in it. A relayed response
  is pointed back at the publisher before anything reads it.
- **The log.** `net/http` wraps every transport failure in a `*url.Error` that prints the address
  it dialled. Logged as it comes, one unreachable relay writes the token into the log file on
  every attempt. The cause is kept and the address dropped; the relay is named separately.

Switching a relay off keeps its credential so it can be switched back on. Deleting does not, and
the dialog says so.

## Where it lives

| | |
| --- | --- |
| `internal/feeds/proxy.go` | the ladder, the routes, what counts as a refusal |
| `internal/feeds/socks.go` | the SOCKS5 client and its cache |
| `internal/store/proxies.go` | the relays, and what is safe to show |
| `internal/api/proxies.go` | `/api/admin/proxies`, including test and reset |
| `web/src/apps/admin/ProxiesPage.tsx` | the screen |

Feed fetching, discovery and picture measuring all go through the same ladder. Measuring is
worth a note: a relay fixes the *shape* a picture is stored with, which decides how wide its
card is laid out — but the reader's browser loads the picture directly, so a blocked picture is
still blocked on the page.
