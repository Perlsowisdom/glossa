// A fake GitHub for the updater checks in the real app: the stable manifest, the release list that the
// Preview channel reads, and each release's latest.json. The app is started with GLOSSA_UPDATE_ENDPOINT
// and GLOSSA_UPDATE_RELEASES_URL pointing here (see app.mjs), so a test never reaches GitHub. The
// installer is served too, so a test can see that nothing asked for it.

import { createServer } from "node:http";

const HASH = "ab".repeat(32);
// Well formed (64 bytes) but made by nobody, so it can never verify against any key.
const SIGNATURE = Buffer.alloc(64).toString("base64");

export async function startFakeReleases() {
  const state = { stable: null, releases: [], mode: "ok" };
  const requests = [];
  let base = "";
  const manifest = (version) => ({
    version, url: `${base}/dl/Glossa-${version}-windows-x64-setup.exe`, sha256: HASH, signature: SIGNATURE,
    notes: `## What is new\n- Notes for ${version}\n- A second line`,
  });
  const srv = createServer((req, res) => {
    const path = new URL(req.url, "http://x").pathname;
    requests.push(path);
    const json = (body) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(body)); };
    const gone = (code, headers = {}) => { res.writeHead(code, headers); res.end(); };
    if (state.mode === "fail") return gone(500);
    if (state.mode === "limit") return gone(429, { "Retry-After": "3600" });
    if (path === "/stable/latest.json") return state.stable ? json(manifest(state.stable)) : gone(404);
    if (path === "/api/releases") {
      return json(state.releases.map((r) => ({
        tag_name: `v${r.version}`, draft: !!r.draft, prerelease: r.version.includes("-"),
        assets: [{ name: "Glossa-setup.exe", browser_download_url: `${base}/dl/setup` }, ...(r.manifest === false ? [] : [{ name: "latest.json", browser_download_url: `${base}/dl/v${r.version}/latest.json` }])],
      })));
    }
    const m = /^\/dl\/v([^/]+)\/latest\.json$/.exec(path);
    if (m) return json(manifest(m[1]));
    if (path.endsWith("-setup.exe")) { res.writeHead(200); return res.end("installer"); }
    return gone(404);
  });
  await new Promise((ok) => srv.listen(0, "127.0.0.1", ok));
  base = `http://127.0.0.1:${srv.address().port}`;
  return {
    endpointUrl: `${base}/stable/latest.json`,
    releasesUrl: `${base}/api/releases`,
    requests,
    // set(stable, releases, mode) replaces what the fake publishes: the newest full release, the
    // release list ({version, draft?, manifest?}) and how it answers ("ok", "fail" or "limit").
    set({ stable = null, releases = [], mode = "ok" } = {}) { Object.assign(state, { stable, releases, mode }); },
    // asked says how many requests were made for a path, or for any path with a prefix.
    asked: (prefix) => requests.filter((p) => p.startsWith(prefix)).length,
    reset() { state.stable = null; state.releases = []; state.mode = "ok"; requests.length = 0; },
    close: () => new Promise((ok) => { srv.closeAllConnections?.(); srv.close(ok); }),
  };
}
