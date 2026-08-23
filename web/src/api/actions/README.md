# API actions

One file per resource, one exported function per endpoint.

## Naming

The function name is derived mechanically from the request, so the mapping is reversible
and nobody has to guess in either direction:

```
<method><PathSegmentsInPascalCase>
```

A path parameter becomes `By<Param>`.

| function                  | request                            |
| ------------------------- | ---------------------------------- |
| `getEdition`              | `GET /api/edition`                 |
| `postEditionRegenerate`   | `POST /api/edition/regenerate`     |
| `putEditionItemsByIdRead` | `PUT /api/edition/items/{id}/read` |
| `getFeeds`                | `GET /api/feeds`                   |
| `patchFeedsById`          | `PATCH /api/feeds/{id}`            |
| `postAdminInvites`        | `POST /api/admin/invites`          |

## Why functions rather than methods on a class

A bundler can drop an exported function that a chunk never references; static methods keep
the whole class alive as one unit. The login island has no business carrying the code that
builds an admin request — and with four separate bundles, that is not a hypothetical.
