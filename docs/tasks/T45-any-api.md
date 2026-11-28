# T45: Rebuild the Any connection on the Any app's own API

Brief for one agent (Sonnet), 28 November 2026. T44 made Glossa find the user's Any app (port 31310) and connect
without a key. In the real app it then said "0 spaces are available". The coordinator checked why, with the
user's permission, reading only field names and the API description, never contents.

Read these first:
- `AGENTS.md`;
- ADR 0014 with both Any addenda;
- `docs/privacy.md` (the Any section);
- all of `internal/connect/any`;
- the Any card in `frontend/src/views` (`anycard.ts` and the Connections part of `settings.ts`);
- `frontend/e2e/any.e2e.mjs` and `fake-any.mjs`.

## What the coordinator found

- **Different API.** The user's Any app is not Anytype. It publishes its own OpenAPI 3.1 description, "Any API
  1.0", at `GET /v1/openapi.json`.
  - A copy is at
    `C:\Users\perlj\AppData\Local\Claude-Data\claude_pzs8sxrjxfjjc\side-session-notes\local_aaaf0e21-ee22-40b9-beab-ce8952ca21c1\any-openapi.json`.
  - Work from that file. **Do not contact the real Any**, and don't commit the file, because its licence is
    unknown. Write a short summary of the parts Glossa uses in `docs/sources/any-api.md`, in your own words.
  - The base path is `/v1`. The description's `servers` entry says port 2046, but the user's app listens on
    31310; keep T44's address finding.
- **No sign-in.** The description declares no security scheme. The real app answered 200 with no key.
- **Spaces:** `GET /v1/spaces` returns `{"spaces": [ {id, name, type, spaceType, status, ...} ]}`. There is no
  paging, and `limit` is ignored. Glossa expected `{"data": [...], "pagination": {...}}`, so it read 0 spaces.
- **Old addresses that fail on the real app:**
  - `GET /v1/spaces/{id}/objects` and `GET /v1/spaces/{id}/properties` answered 404.
  - Glossa's form of `POST /v1/spaces/{id}/search` answered 400.
- **The parts that matter in the description:**
  - `POST /spaces/{spaceId}/objects/query` takes `filter` (mongo-style), `sort` (e.g. `"-updatedAt"`), `limit`,
    `offset`, `includeTotal` and `projection`. It returns `{records, total, hasNext}`.
  - `POST /spaces/{spaceId}/search` takes `query` (required), `limit`, `mode` and `filter`. It returns
    `{hits: [...]}`.
  - `GET /spaces/{spaceId}/objects/{objectId}` returns `{objectId, record}`.
  - `GET` and `PUT /spaces/{spaceId}/objects/{objectId}/editor/{collection}/markdown` read and write the text.
    The read returns `{content, version}`. The write takes `{content, ifVersion}`, is diff-based and answers
    409 on a version conflict. The `collection` for a page body is most likely `editor_blocks`; confirm it
    from the description.
  - `POST /spaces/{spaceId}/objects` takes `{type, initialProperties, collections}` and returns `{objectId}`
    (201).
  - `GET /spaces/{spaceId}/types` returns `{types: [{id, name, description, xKey, builtIn}]}`.
  - Errors come back as `{error: {...}}` (`api.ErrorEnvelope`).
  - **Never call `POST /auth`, `DELETE /auth`, `/shutdown`, ACL, invite, delete or any other account or
    space-management address.** `POST /auth` creates or switches accounts.

## What to change

1. **Client (`client.go`):**
   - Rewrite the calls Glossa needs against this API: list spaces; list and search pages in a space, newest
     change first; read one page with its Markdown; create a page with a name and Markdown body; update its
     name and body, using `ifVersion` and turning a 409 into a plain "changed in Any since; try again" error.
   - Find the page type through `GET …/types`, matching `xKey` (for example `page`) and falling back to name.
     Cache it per space for the session.
   - Work out from the description how a record's name, type, archived/deleted state and last change are
     stored, and map them to Glossa's `Page`. Skip deleted and archived records.
   - Properties: Glossa used to set text properties (source and links back to Glossa). Keep that only if the
     description gives a safe, simple way to set a text property on a page without creating schema. If it
     doesn't, drop it. Glossa already keeps its links locally (`any_links`). Say which you chose and why.
   - Parse errors from `ErrorEnvelope`. Map 404/410 to `ErrNotFound`, 409 to the conflict error, and keep
     the not-running, rate-limit and bad-address errors.
2. **Sign-in:**
   - The API has no keys. Remove pairing (challenge and code) from the client, the commands and the card,
     and say in the card that Any needs no sign-in.
   - If an older install has a saved Any key, delete it from the credential store quietly on the next connect.
   - Keep `Probe` (`GET /v1/spaces`). A 401 or 403 now just means "Any refused Glossa", in plain words. Don't
     offer pairing.
3. **Probe and address finding:** keep T44's behaviour (31310, then 31009, the saved address, the Advanced
   field). The probe should check that the reply looks like this API (a `spaces` array), so another program
   on the port isn't taken for Any.
4. **Tests:**
   - **Go:** replace the fake server with one shaped from the description: spaces, types, objects/query,
     search, object get, markdown get/put with versions, create, and error envelopes. Cover:
     - the listing, newest first, skipping archived and deleted;
     - search;
     - get with Markdown;
     - create;
     - update, including a 409 conflict;
     - a missing page;
     - the page type looked up from types;
     - no key ever sent;
     - an old saved key deleted;
     - the probe rejecting a non-Any reply.
   - Add a test that fails if the client builds a request to `/auth`, `/shutdown` or any `DELETE`.
   - **Real app:** update `fake-any.mjs` and `any.e2e.mjs` to match. Spaces are listed, a note is sent, a page
     is imported, an update is sent, and the not-found and refused messages show. Take screenshots at 1536×816.
   - Run only the files you touch while working, then the full suite once at the end.
5. **Docs:**
   - `docs/privacy.md` (Any): what Glossa asks Any for and sends, and that no key is used.
   - `docs/sources/any-api.md`.
   - A dated "Addendum (Proposed)" to ADR 0014: Glossa talks to the Any app's own API, and pairing is removed.

## Rules

- No real network. **Never contact the user's real Any**, not even to check the API: use the saved
  description and fakes on free loopback ports. Never use the user's real Glossa library.
- Keys go in the credential store, never in logs.
- Permissive licences only.
- Run `npm ci` in `frontend/`; no junctions.
- Run long commands with timeouts, and close every process you start, including `glossa-e2e.exe` and e2e node
  processes.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check`, `xegesis-check` or `%LOCALAPPDATA%\Programs\Glossa`.
- Don't commit `frontend/dist` or the API description file. Don't push. Never `git stash`. Never switch
  branches.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`. End each commit message with a
  blank line, then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit after each step.
- Use `scripts\build.ps1` for any app build.
- Plain words in the interface.
