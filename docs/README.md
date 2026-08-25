# Documentation

How this project is built, so a decision made once does not have to be re-argued.

These are rules and references, not a plan — what gets built in what order is not settled here.

They belong in the repository for a reason: a document explaining why a table is shaped the way
it is belongs beside the table, in the same history, reviewable in the same diff. That is also
what makes the rule below enforceable — a stale document is something a commit can be seen not
to have fixed.

| document | what it settles |
| --- | --- |
| [stack.md](stack.md) | Every dependency, its version, and why it is here |
| [conventions.md](conventions.md) | Naming, commits, comments, ids, time |
| [entities.md](entities.md) | The two databases, every table, every invariant |
| [edition.md](edition.md) | How a front page is selected and laid out |
| [api_design.md](api_design.md) | HTTP conventions and the full endpoint reference |
| [opml.md](opml.md) | Sharing feeds: the format, and why import happens twice |
| [backend.md](backend.md) | Go package layout and the rules each package follows |
| [frontend.md](frontend.md) | Islands, the API layer, query keys, styling |
| [deploy.md](deploy.md) | Image, environment, volumes, CI |
| [mail.md](mail.md) | SMTP, recovery addresses, and what is deliberately not sent |
| [screenshots/](screenshots/) | How the README's pictures are made, and by what |

## When these disagree with the code

The code is right and the document is stale. Fix the document in the same commit that
made it stale — a reference nobody trusts is worse than no reference, because it costs a
reader the time to find out.

That is the rule and it has been broken, which is what [meta.txt](meta.txt) is for. It records
the commit each of these was last checked against, so the question "what has happened since
anybody read this" is `git log <hash>..HEAD` rather than a re-read of everything. Update the
line when you update the document.
