# T44: Any connection finds the app (port and sign-in)

Brief for one agent (Sonnet), 27 November 2026. The user reported that Glossa does not recognise Any.

Read these first:
- `AGENTS.md`;
- ADR 0014 (connections, with its Any addendum);
- `docs/privacy.md` (the Any section);
- `internal/connect/any` (`client.go`, `any.go`, `commands.go`);
- `internal/connect` (`connect.go` and `service.go`);
- the Connections part of `frontend/src/views/settings.ts`;
- the Any e2e file and its fake server.

## What the coordinator found on the user's computer (27 November)

- **Port:** `internal/connect/any/client.go` uses `DefaultURL = "http://127.0.0.1:31009"`, which is
  Anytype's API port. The user's Any app (`any.exe`) listens on **`127.0.0.1:31310`**. Nothing listens
  on 31009, so the test says "The Any app is not running".
- **No key:** Any's API on 31310 answered `GET /v1/spaces` with **200 and no key**. The user's own Any
  MCP server is set to `ANY_API_URL=http://127.0.0.1:31310/v1` and uses no key.
  - Glossa today requires pairing first: a challenge, a code, then an API key.
  - It is not known whether this Any build supports `/v1/auth/challenges` at all. The coordinator did
    not call it, because that would show a pairing prompt in the user's app.
- **Version header:** the server accepted `Anytype-Version: 2025-05-20`. Glossa sends `2025-11-08`;
  keep it unless the fake shows a reason to change.

## What to change

1. **Finding Any:**
   - With no address saved, try `http://127.0.0.1:31310` first, then `http://127.0.0.1:31009`. Probe
     with a cheap GET that has no side effects, such as `GET /v1/spaces?limit=1`, with a short timeout
     (about 1 s each).
   - Remember the address that answered, per device.
   - Settings → Connections → Any gets an **Address** field, folded under "Advanced" or "Not found?".
     It is still loopback only, as the existing guard in `client.go` requires. Validate it and say
     plainly when it isn't on this computer.
   - When neither port answers, the message is: "Glossa couldn't find the Any app on this computer.
     Open Any, or enter its address under Advanced."
2. **Signing in, only when asked:**
   - Try the API with no key first.
   - If Any answers 401 or 403, offer pairing, using the existing challenge-and-code flow.
   - If it answers 200, mark the connection ready with no key. Show "Connected (Any didn't ask for a
     key)" or similar plain words.
   - A key already saved is still sent. If a saved key gets a 401, say the key is no longer accepted
     and offer to pair again.
3. **Tests:**
   - **Go:** extend the fake Any server so it can listen on either port and require a key or not.
     Cover:
     - finding 31310, falling back to 31009, and neither answering;
     - a saved address;
     - no key needed;
     - a key needed, with pairing offered;
     - a stale key;
     - the loopback guard on the Address field.
   - **Real app:** extend the Any e2e checks:
     - a keyless fake on a non-default port is found and connects;
     - the Address field works;
     - the not-found message.
   - Take screenshots at 1536×816. Run only the files you touch, then the full suite once at the end.
4. **Docs:**
   - `docs/privacy.md` (Any): the two ports it tries, and that it may connect with no key when Any
     allows it.
   - An addendum to ADR 0014. The user accepted ADR 0014 on 26 November, so add a dated
     "Addendum (Proposed)" for this change.

## Rules

- No real network, and **never call the user's real Any**. Tests use the fake server on free loopback
  ports only. Never use the user's real Glossa library.
- Keys go in the credential store, never in logs.
- Permissive licences only.
- Run `npm ci` in `frontend/`; no junctions.
- Run long commands with timeouts, and close every process you start, including `glossa-e2e.exe` and
  e2e node processes.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check`, `xegesis-check` or `%LOCALAPPDATA%\Programs\Glossa`.
- Don't commit `frontend/dist`. Don't push. Never `git stash`. Never switch branches.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`. End each commit message
  with a blank line, then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit after each step.
- Use `scripts\build.ps1` for any app build.
