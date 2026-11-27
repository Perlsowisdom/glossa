# T32: Problem reports (crash and error log, reviewed reports)

Brief for one agent (Sonnet), with the user's decisions from 26 November 2026. Read these first:
- `AGENTS.md`;
- ADR 0009 (privacy), which this task carries out;
- `docs/privacy.md`;
- `app/main.go`, `cmd/glossa/main.go` and `internal/update` (how the app starts and opens links).

The user wants the desktop app ironed out before any mobile work. This task gives them a way to hear
about problems without any telemetry.

## The user's decisions

- **Where a report goes: a GitHub issue, opened in the browser.**
  - "Send" opens a pre-filled new-issue page on `github.com/Perlsowisdom/glossa`, using
    `runtime.BrowserOpenURL`. The person reads it again there and presses Submit themselves.
  - The app never sends anything itself and makes no network call for reports.
  - Issues are public, so the stripping rules below matter. Say plainly in the dialog that the issue
    will be public.
  - The issue URL has a length limit (keep it under about 7,000 characters). If the report is longer,
    shorten the log lines first, then the stack. Say so, and offer Copy so the full text can be pasted
    in.
- **Also: Copy and Save to file.** Save uses the system save dialog and writes a `.txt` file.
- **Nothing is sent automatically.** There is no "always send".

## What to build

### The local log (`internal/problems`, or a name that fits the code)

- A small rotating log file in the app's data folder (use `internal/appdir`). Suggested size: 1 MB,
  with one older file kept. Glossa's own messages go there through `log/slog`, plus anything it already
  prints.
- **Go crashes:**
  - Recover panics in command handlers and background goroutines, where that is safe. Log each one
    with its stack and return a plain error to the interface.
  - A crash that kills the process should leave a marker, so the next start knows.
- **Interface errors:** `window.onerror` and `unhandledrejection` in the frontend are sent to Go
  through a command (for example `problems.log`) and written to the log. Limit how many can come in
  a minute.
- **The log never holds private content in the first place,** where that is easy. For example, log a
  command's name and error but not its arguments.

### The report builder

- A report contains:
  - the app version and channel;
  - the operating system and version, and the architecture;
  - the WebView2 version, if it is cheap to read;
  - the error and its stack;
  - recent log lines (for example the last 200, then cut to fit);
  - an optional "What were you doing?" box the person fills in.
- **Stripping (ADR 0009).** A report must never contain:
  - note or document text, book titles, verse lists or search terms;
  - keys, tokens or account details;
  - file paths: replace the user's home folder, data folder and library path with `<home>`,
    `<data>` and `<library>`, and any other absolute path with `<path>`;
  - e-mail addresses, URLs with query strings, or long base64 or hex runs that may be secrets.

  Put the rules in one place, with table-driven Go tests, including Windows and macOS paths and
  `\\?\` paths.
- **The preview is the report.** The person sees exactly the text that will go out and can edit any
  part of it or delete lines. What is sent, copied or saved is what the box shows.

### The interface

- **Help → Report a problem.** Put it where Help items live now; if there are none, put it in
  Settings → About, or wherever the version is shown.
  - It opens the report dialog with recent log lines and an empty "What were you doing?" box.
- **After a crash or a new error:**
  - On the next start after a crash, or after an error the interface caught, show a quiet notice:
    "Glossa ran into a problem. Review a report?", with Review and Not now.
  - It is not a modal, and it shows once per problem.
  - Use the same notice style as the update notice (`#update-banner`), without overlapping it.
- **The dialog:**
  - A short line on what is in the report and what is never in it.
  - The editable text.
  - "Open as GitHub issue (public)", Copy, "Save to file…" and Cancel.
  - Plain words, no jargon, and keyboard-friendly.
  - Use the app's own dialog, not a native `confirm`.
- **Settings:** a "Keep a problem log on this computer" switch, on by default, and "Open the log
  folder". Turning the switch off stops writing and deletes the log.

### Docs

- `docs/privacy.md`: a section on problem reports. Cover:
  - what is logged locally;
  - what a report contains;
  - that it goes nowhere unless the person opens it as a GitHub issue and submits it, and that such
    issues are public.
- An addendum to ADR 0009, saying where reports go (decided 26 November 2026). Mark it Proposed.
- An e2e scope note in `docs/e2e-scope.md`.

## Tests

- **Go:** stripping (table-driven), log rotation, panic recovery that turns into an error, the crash
  marker, the issue URL builder (encoding and the length cut), and the frontend error rate limit.
- **Real app (`frontend/e2e/problems.e2e.mjs`):**
  - Report a problem from Help: the dialog shows the version and log lines, and no paths from the
    test library (check that the scratch library's path does not appear).
  - Editing the text changes what is copied. Check Copy through the clipboard, or through what the
    command receives.
  - Open as GitHub issue: catch the URL the app would open, without opening a browser. Check it
    points at the repo's new-issue page, holds the edited text, and stays under the limit.
  - A thrown interface error shows the notice once; Not now hides it.
  - A crash marker from an earlier run shows the notice on start.
  - Turning the log off deletes it.
  - Take screenshots at 1536×816.

## Rules

- No real network in tests, and no browser actually opened.
- Never use the user's real library: temp folders only.
- Permissive licences only (no GPL, AGPL, MPL or CPAL). No analytics or crash-reporting SDKs
  (Sentry and the like); write it ourselves.
- Keys and tokens never go in logs.
- Run `npm ci`; no junctions.
- Run long commands with timeouts, and close every process you start, including `glossa-e2e.exe`.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check`, `xegesis-check` or `%LOCALAPPDATA%\Programs\Glossa`.
- Don't commit `frontend/dist`. Don't push. Never `git stash`.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`. End commit messages with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit after each step.
- Run only touched e2e files while working, and the full suite once at the end.
- Use `scripts\build.ps1` for any app build.
