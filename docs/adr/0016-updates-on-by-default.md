# 0016: Updates are on by default

Status: **Proposed** (the user decided on 26 November 2026; this records it). Changes how 0009's rule "each
off until the user turns it on" applies to one feature, the direct download's update check (T23). 0009's
text is unchanged and stays accepted; every other network feature stays off until turned on.

## Context

ADR 0009 says no telemetry, every network call is listed, and each named feature is off until the user
turns it on. The updater followed that: on the first start it asked "Check for updates automatically?",
and "No thanks" left it off. In practice people who dismiss or ignore a question stay on old versions, and
the direct download's setup is not code-signed, so a fix reaches people only if they learn it exists.
The question is also a blocking decision on the common path, against AGENTS.md ("no blocking questions").

The check itself sends nothing about the person: one HTTPS `GET` of a public file on GitHub.

## Decision

- **Update checks are on unless the person turns them off.** There is no first-run question. A new
  install checks once a day while Glossa is open.
- **Old installs.** If the person explicitly answered "No thanks" (or turned the switch off), that stays:
  updates remain off. If they never answered the old question, updates turn on. (Stored as before in the
  `update.settings` setting: `enabled` is read as on when `asked` is not true.)
- **What is sent.** Once a day, and when the person presses "Check for updates", Glossa makes HTTPS `GET`
  requests with the header `User-Agent: Glossa` and nothing else: no cookies, no account, no identifier,
  no version of this app, no information about the computer or the library.
  - Stable channel: `https://github.com/Perlsowisdom/glossa/releases/latest/download/latest.json`.
  - Preview channel: `https://api.github.com/repos/Perlsowisdom/glossa/releases?per_page=10`, then the
    `latest.json` of the newest release that is not a draft (from this project's release downloads on
    github.com).
  GitHub sees the computer's IP address and the file asked for, as with any download. Nothing is downloaded
  to install until the person presses "Install and restart"; the installer's SHA-256 and Ed25519 signature
  are checked first, unchanged (T23).
- **A person is told, not interrupted.** A newer version shows a small notice with "Install and restart"
  and "Later", and a marker on the rail. Installing is always one click by the person, never automatic.
  A failed check says nothing outside Settings → Updates, which shows the last error.
- **Turning it off.** Settings → Updates → "Check for updates automatically" (or the command
  `update.settings.set {"enabled": false}`). Off means no request at all. The Microsoft Store build has no
  updater and sends nothing.
- **Channels.** Settings → Updates has Stable (full releases) and Preview (the newest release, test versions
  included). A build that is itself a pre-release starts on Preview, others on Stable, until the person
  chooses. A version that is not newer than the running one is never offered, so switching never downgrades.
- **Rate limits.** If GitHub answers 403 or 429 for a rate limit, Glossa waits (honouring `Retry-After` or
  `X-RateLimit-Reset`, at least until the next daily check) and says nothing.
- **Per device.** The update settings are not synced (they sit in `perDeviceSettings`), so one computer's
  choice is not copied to another.

## Consequences

- `docs/privacy.md` lists the Preview host (`api.github.com`) and says updates are on by default, and how
  to turn them off. AGENTS.md notes the exception.
- Anyone who wants no network traffic at all can turn the switch off once; the first-run question is gone,
  so the switch is the only place. The privacy page says so.
- The Preview channel adds one request (the release list) to a check; GitHub allows 60 unauthenticated API
  calls an hour per address, far above one a day.
- If this decision is later reversed, `enabled` can again be required to be explicit; stored answers
  already keep the old meaning.
