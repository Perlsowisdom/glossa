# The Any app's own API

Source of `internal/connect/any`. Written from the OpenAPI 3.1 description the Any app publishes about itself
("Any API 1.0", at `GET /v1/openapi.json`), which the user supplied as a file. The file itself is not kept in this
repository (its licence is not known); this is a short summary, in our own words, of the parts Glossa uses. Glossa
never contacts the real Any app in tests or while developing: tests use fakes on loopback ports shaped from this.

## Basics

- Base path `/v1`. The description names port 2046; the user's app listens on **31310**, and Glossa also tries 31009
  and any address typed in Settings (loopback only).
- No security scheme is declared: the local API checks no one yet. Errors come back as `{"error": {"code",
  "message", "details"}}`.
- Object ids in a path may be full or shortened; Glossa always uses the full id Any returned.

## What Glossa calls

| Call | Used for |
| --- | --- |
| `GET /spaces` | `{"spaces": [{id, name, type, spaceType, status, ...}]}`. No paging; deleted spaces are left out unless asked. Also the probe that finds Any: Glossa accepts an answer only if it has a `spaces` array. |
| `GET /health` | `{status, version, account, ...}`. Glossa keeps only `account`, to notice a different account since pairing. |
| `GET /spaces/{space}/types` | `{"types": [{id, name, description, xKey, builtIn}]}`. The page type is the one whose `xKey` is `page`, else the one named Page; remembered per space for the session. |
| `POST /spaces/{space}/objects/query` | Body `{filter, sort, limit, offset, includeTotal, projection}`; answer `{records, total, hasNext}`. Glossa filters by `any.type` and sorts newest change first (`-modifiedAt`); if the app refuses the sort it asks again without and sorts itself. |
| `POST /spaces/{space}/search` | Body `{query (required), limit (max 100), mode, filter}`; answer `{hits: [{objectId, data, ...}]}`. A page can come back more than once (one hit per chunk); Glossa lists it once, then reads its row. |
| `GET /spaces/{space}/objects/{id}` | `{objectId, record}`; 404 `object.not_found`, 410 `object.deleted`. |
| `GET /spaces/{space}/objects/{id}/editor/editor_blocks/markdown` | `{content, version}`: the page text. |
| `PUT` the same address | Body `{content, ifVersion}`, diff-based; 409 `markdown.conflict` when the version is stale (Glossa says "That page changed in Any since Glossa read it. Try again."). |
| `POST /spaces/{space}/objects` | Body `{type, initialProperties, collections}`; answer `{objectId}`. The name goes in `initialProperties` under owner `any`: `{"any": {"name": ...}}`; a top-level name is rejected. |

## Records

A record is keyed by owner, then property: the built-in values are under `any` (`any.name`, `any.type`,
`any.collections`). Glossa reads both the nested form and the dotted key. A page in the bin has `bin` among its
collections; deleted objects are purged (a get answers 410). Glossa treats both as not there. The last change is read
from `modifiedAt` (or `updatedAt`), at the top of the row or under `any`. The description does not spell these
row fields out, so this is the part most worth checking against a real answer.

## Not used, on purpose

- **Passages property.** The description sets property values by owner and property id, which would mean creating
  schema in the person's space. Glossa keeps the passages in its own links instead and sends only the title and text.
- **Renaming a page.** No address renames a page in a way the description makes plain, so the name is set when the
  page is made; later updates change the text.
- **`POST /auth`, `DELETE /auth`, `/shutdown`, access control, invites, deletes, and every other account or space
  management address.** Glossa never calls them; a test fails if the client builds such a request.
