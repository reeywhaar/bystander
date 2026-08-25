# Mail

bystander does not deliver mail. It hands a message to a relay an operator already has —
their mail provider, or a sending service — and that relay does the rest. Everything here is
about the handing over.

Nothing sends mail yet except the test button. The relay exists first because the thing that
needs it — a recovery address somebody can actually recover through — is worthless without
it: an address stored against a relay that does not exist is a promise the product cannot
keep, and the person finds that out at the worst possible moment.

## Where the configuration lives

In the database, set from the admin interface, not in the environment.

An operator setting up mail gets it wrong two or three times — a port, a username that turns
out to be an address, a From the relay will not accept — and each correction should be a
form field and a test send, not a redeploy. The environment is the right place for things
that must be true before the process starts. This is not one of them.

The password is stored as written. There is no vault here to seal it under, and a reversible
scramble would only make it look protected: whoever can read `main.db` can already read the
session table and every password hash in it. The file is the boundary either way, and
pretending otherwise is worse than saying so.

One row, enforced. `singleton` is `UNIQUE` and `CHECK`ed, so a second configuration is a
constraint violation rather than a quiet question about which one is live.

## What is not configurable

**Whether the connection is encrypted.** `starttls` upgrades a plain connection, `implicit`
is TLS from the first byte, and there is no third option. A password crossing the network in
the clear is not a choice somebody should be able to make by accident, so a relay that does
not offer `STARTTLS` is refused by name rather than fallen back from — and the refusal says
to try implicit TLS on 465, because that is usually what is wrong.

**Whether credentials are required.** Relays that want none do exist. Accepting a blank
password would make "this relay needs no authentication" indistinguishable from "somebody
left the field empty", and the second is far likelier.

## Sending

`internal/mail` opens the socket; `internal/store` decides what the relay is. Nothing in the
store touches the network and nothing in `mail` touches the database.

Nothing is queued. A message goes out while the request that asked for it is still open, so
the caller learns whether the relay accepted it. That is the whole point of a test send, and
it is what any later caller wants too — a password-reset mail that failed silently is worse
than one that failed loudly. If bystander ever sends enough mail for that to hurt, a queue
is a change to this file, not a change to its callers.

A refusal is a `502` carrying the relay's own words. Not a `500`: everything on this side
worked and something upstream did not, and a `500` sends an operator through the wrong logs.
Not "sending failed" either — "the host was wrong", "the credentials were rejected" and "the
certificate did not verify" are three different afternoons.

`PLAIN` first, `LOGIN` when that is all the relay offers. `LOGIN` is the same credentials in
a sillier shape, and enough relays speak nothing else — Office 365 among them — that
refusing it would mean refusing to send at all. Both refuse to run over an unencrypted
connection or against a host that is not the one configured.

Messages are composed here rather than with a library: it is a dozen headers and a
quoted-printable body. Addresses go through `net/mail`, because a display name needs quoting
when it holds a comma and encoding when it holds anything outside ASCII, and either one done
by hand produces a header that parses as a different address than the one meant.

## Recovery addresses

An account carries one address or none, and **only an address somebody has proved they can
read**. Adding one is two steps: a code goes to the address and has to come back. Until it
does, the account has no recovery address at all — not a provisional one — so a flow
abandoned anywhere leaves exactly what was there before.

The first version of this stored whatever was typed. That is worse than storing nothing: a
typo points recovery at a stranger's inbox, and the owner finds out at the one moment they
cannot afford to. It is also the shape of an attack — a borrowed session sets an address of
its own and comes back for the account later, once resets exist.

**Two tables, not a `confirmed` column.** `user_recovery` holds proved addresses;
`recovery_pending` holds attempts. A nullable column is one forgotten `WHERE` clause away
from an unproved address being treated as proved, and confirming is a move from one table to
the other, so the only address recovery can read is one that was proved.

The code is eight characters of Crockford's base32 — no `I`, `L`, `O` or `U`, because it is
read off one screen and typed into another. Forty bits is not a key, and does not need to
be: it is bounded to five attempts, expires in fifteen minutes, and authorises nothing but
the address it went to. It is stored hashed like every other secret here, so the only way to
know a code is to be the recipient — which is what the tests rely on rather than working
around.

Attempts are **replaced, never accumulated**. Starting again is what somebody does when the
mail did not arrive, and two live codes for one account is two chances at the same guess.
Five wrong answers throws the attempt away rather than locking anything: a lockout is a state
somebody has to wait out, and starting again is faster and no weaker.

**One address, one account, held by whoever proved it last.** A unique index enforces it, and
confirming takes the address off whichever account had it. Whoever can read that inbox today
is who recovery through it would actually reach — a work address gets reassigned, somebody
moves out of a shared one — and refusing instead would refuse the person who really can read
it while leaving the one who cannot on record. It concedes nothing: anybody able to prove
control of that inbox could already recover the account attached to it. The displaced account
is not told, because the only address on file for them is the one they just lost; the takeover
is logged instead.

**A code that could not be sent leaves nothing waiting.** The pending row is written before
the send, so the code exists before it travels — but a send that fails deletes it again.
Otherwise the page says it is waiting on a code that never left, and the way out of that
state is the button somebody just watched fail. A failed *change* leaves the address that
already worked, which is why dropping an attempt is its own store method rather than a flag
on the one that forgets everything.

Starting is refused outright when no relay is configured, before anything is written.
`GET /api/account` carries `mail_configured` so the page can say why the button is disabled;
whether this instance can send mail is not a secret from the people whose recovery depends
on it.

Nothing sends to a proved address yet — that is the reset flow, which does not exist.

```
postAccountRecovery         POST   /api/account/recovery           sends a code, records nothing
postAccountRecoveryConfirm  POST   /api/account/recovery/confirm   the only step that changes anything
deleteAccountRecovery       DELETE /api/account/recovery           forgets both
```

One refusal — "that code is wrong or has expired" — covers wrong, expired, exhausted and
absent alike. Which of the four it was tells a caller something about an account that may not
be theirs, and tells the owner nothing they could not learn by trying again.

## The API

Administrators only. A relay's hostname and username are infrastructure.

```
getAdminSmtp        GET    /api/admin/smtp        the relay, without its password
putAdminSmtp        PUT    /api/admin/smtp        the whole configuration at once
deleteAdminSmtp     DELETE /api/admin/smtp        forget it
postAdminSmtpTest   POST   /api/admin/smtp/test   one real message, saved or merely typed
```

`PUT` rather than `PATCH`, because these fields only make sense together: a host changed
without the credentials that go with it is a relay nobody can reach, and saving half of one
breaks sending in a way the form hides.

The password is write-only. It is never in a response, and an empty one in a request means
"keep the stored one" — so correcting a port does not mean retyping a secret the page never
showed. The first save is the exception: there is nothing to keep, so a blank field there is
a mistake rather than an instruction.

The test **sends** rather than merely connecting. A relay will happily accept a login and
then refuse the From address, and an operator who saw "connected" would find that out later,
from somebody who could not get in.

It also takes an optional `relay`, and that is the important part: given one, it tries those
settings and writes nothing. Without it the only way to find out whether a password works is
to save it, and by then the configuration it replaced is gone. Typed settings face exactly
the checks a save would make — `store.ValidateSMTP` is called by both — so a test cannot pass
against something the database would then refuse.

## The interface

The page states what is configured; changing it happens in a dialog. That split is what
makes trying-before-saving possible, and it is why this is not a form sitting open on a
page: a form you edit in place has nowhere to put "try this" that is not also "commit this".

The dialog holds everything until Save, opens on what is stored, and is mounted only while
it is open — so it never shows what was typed and abandoned last time, and a failed test does
not greet somebody the next time they open it. Its buttons sit in `Modal`'s footer rather
than at the end of the form: this is a tall form on a short screen, and a Save that has
scrolled out of sight reads as a form with no way to finish it.

## What is actually tested

`internal/mail` runs against a relay the test starts itself, with a certificate it makes
itself, over both TLS modes:

- a message that arrives is one a relay would accept — encoded subject, quoted display name,
  CRLF throughout, no bare newline anywhere
- `STARTTLS` really upgrades, proven by the second `EHLO` rather than by the command going out
- a relay that does not offer `STARTTLS` gets no credentials at all
- a refusal comes back with the relay's own words in it
- a relay that accepts a connection and then says nothing does not hold the request open
