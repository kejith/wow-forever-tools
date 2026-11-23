// The app's own update: the newest release of kejith/wow-forever-tools. The Windows installer of a newer
// version is downloaded into the app's data folder, compared with the release's SHA256SUMS.txt and started
// silently; it replaces the installation where it is and starts the app again.
// No Electron dependency (main.js hands in the folder, the version and what happens on a change of state),
// so the tests run all of it in Node against a stand-in for the GitHub API.
//
// The checksum catches a damaged or cut-off download and an installer that is not the one the release
// lists. It is no signature: whoever can publish a release of the repository can publish both files.
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { spawn } = require('node:child_process');
const { t } = require('./i18n');
const { API, compareVersions, request } = require('./releases');

const REPO = 'kejith/wow-forever-tools';
const CHECK_AGE_MS = 60 * 60 * 1000; // a start within the hour does not ask GitHub again
const MAX_INSTALLER_BYTES = 512 * 1024 * 1024;
const MAX_SUMS_BYTES = 64 * 1024;
const IDLE_MS = 30000; // a download without data for this long is given up
const VERSION = /^\d+\.\d+\.\d+$/;
const SHA256 = /^[0-9a-f]{64}$/;
const SUMS = 'SHA256SUMS.txt';
const INSTALLER = /^WoW-Forever-Tools-Setup-(\d+\.\d+\.\d+)\.exe$/;

const installerName = (version) => `WoW-Forever-Tools-Setup-${version}.exe`;
// The release page is built from the checked version, never taken from an answer.
const releaseUrl = (version, repo = REPO) => `https://github.com/${repo}/releases/tag/v${version}`;
const stateFile = (dir) => path.join(dir, 'update.json');

function readState(dir) {
  try { return JSON.parse(fs.readFileSync(stateFile(dir), 'utf8')); } catch { return null; }
}

// What a check remembers of a release; `sha256` only for a version newer than the running one.
function validLatest(latest) {
  return !!latest && VERSION.test(String(latest.version || '')) && Number.isInteger(latest.assetId)
    && Number.isInteger(latest.size) && latest.size > 0 && latest.size <= MAX_INSTALLER_BYTES
    && (latest.sha256 == null || SHA256.test(latest.sha256));
}

// The checksum of `name` in the text sha256sum writes: "<hex>  <name>" per line ("*<name>" in binary mode).
function parseSums(text, name) {
  for (const line of String(text).split(/\r?\n/)) {
    const m = line.match(/^([0-9a-fA-F]{64})\s+\*?(.+)$/);
    if (m && m[2].trim() === name) return m[1].toLowerCase();
  }
  return null;
}

function hashFile(file) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    fs.createReadStream(file).on('data', (chunk) => hash.update(chunk)).on('error', reject)
      .on('end', () => resolve(hash.digest('hex')));
  });
}

// Is `file` the installer the release lists: the same size and the same checksum?
async function matches(file, latest) {
  try {
    if (!latest?.sha256 || fs.statSync(file).size !== latest.size) return false;
    return await hashFile(file) === latest.sha256;
  } catch {
    return false;
  }
}

// ---------- GitHub ----------

// The newest release as { version, tag, publishedAt, size, assetId, digest, sumsId }. Its installer must carry
// exactly the name the release workflow gives it.
async function latestInstaller({ repo, api, fetchFn }) {
  const release = await (await request(fetchFn, `${api}/repos/${repo}/releases/latest`, 'application/vnd.github+json')).json();
  const tag = String(release.tag_name || '');
  const version = tag.slice(1);
  if (!tag.startsWith('v') || !VERSION.test(version)) throw new Error(t('Unexpected release name: {tag}', { tag: tag.slice(0, 40) }));
  const assets = Array.isArray(release.assets) ? release.assets.filter((a) => a && Number.isInteger(a.id)) : [];
  const asset = assets.find((a) => a.name === installerName(version));
  if (!asset || !Number.isInteger(asset.size) || asset.size <= 0) throw new Error(t('Release {tag} has no installer', { tag }));
  if (asset.size > MAX_INSTALLER_BYTES) throw new Error(t('The installer of release {tag} is too large', { tag }));
  const digest = (String(asset.digest || '').match(/^sha256:([0-9a-f]{64})$/) || [])[1] || null;
  const sums = assets.find((a) => a.name === SUMS);
  return { version, tag, publishedAt: typeof release.published_at === 'string' ? release.published_at : null,
    size: asset.size, assetId: asset.id, digest, sumsId: sums && sums.size <= MAX_SUMS_BYTES ? sums.id : null };
}

// The installer's checksum from the release's SHA256SUMS.txt. GitHub's own digest of the asset, where the
// API has one, must say the same.
async function checksumOf({ repo, api, fetchFn }, found) {
  const name = installerName(found.version);
  const missing = () => new Error(t('Release {tag} has no checksum for {name}', { tag: found.tag, name }));
  if (!found.sumsId) throw missing();
  const res = await request(fetchFn, `${api}/repos/${repo}/releases/assets/${found.sumsId}`, 'application/octet-stream');
  const text = Buffer.from(await res.arrayBuffer());
  const sha256 = text.length <= MAX_SUMS_BYTES ? parseSums(text.toString('utf8'), name) : null;
  if (!sha256) throw missing();
  if (found.digest && found.digest !== sha256) throw new Error(t('The checksums of release {tag} do not agree', { tag: found.tag }));
  return sha256;
}

// Asks for the newest release, at most every `maxAge` ms (`force` asks now), and remembers the answer in
// `<dir>/update.json`. Never throws: without an answer the last one counts, with `error` as the reason.
// -> { latest: { version, tag, publishedAt, size, assetId, sha256 } | null, available, checkedAt, error? }
async function check({ dir, current, repo = REPO, api = API, fetchFn = fetch, maxAge = CHECK_AGE_MS, force = false }) {
  const saved = readState(dir);
  const known = saved?.repo === repo && validLatest(saved.latest) ? saved.latest : null;
  const newer = (latest) => !!latest && compareVersions(latest.version, current) > 0;
  const lastCheck = known ? Number(saved.checkedAt) || null : null;
  // A remembered release newer than this version always has its checksum; one without is asked for again.
  const usable = known && (!newer(known) || known.sha256);
  if (usable && !force && Date.now() - (lastCheck || 0) < maxAge) return { latest: known, available: newer(known), checkedAt: lastCheck };
  try {
    const found = await latestInstaller({ repo, api, fetchFn });
    const latest = { version: found.version, tag: found.tag, publishedAt: found.publishedAt, size: found.size, assetId: found.assetId,
      sha256: newer(found) ? await checksumOf({ repo, api, fetchFn }, found) : null };
    const checkedAt = Date.now();
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(stateFile(dir), JSON.stringify({ repo, checkedAt, latest }, null, 2));
    return { latest, available: newer(latest), checkedAt };
  } catch (err) {
    const error = err.notFound ? t('{repo} has no release yet.', { repo }) : err.message;
    return { latest: usable ? known : null, available: !!usable && newer(known), checkedAt: lastCheck, error };
  }
}

// Downloads the installer of `latest` to `<dir>/WoW-Forever-Tools-Setup-<version>.exe` and returns the path.
// The file gets its name only after size and checksum fit; a download that is already there is kept.
async function download({ dir, latest, repo = REPO, api = API, fetchFn = fetch, onProgress = () => {}, idleMs = IDLE_MS }) {
  const name = installerName(latest.version);
  if (!validLatest(latest) || !latest.sha256) throw new Error(t('Release {tag} has no checksum for {name}', { tag: latest.tag, name }));
  const file = path.join(dir, name);
  if (await matches(file, latest)) return file;
  fs.mkdirSync(dir, { recursive: true });
  const part = `${file}.part`;
  const abort = new AbortController();
  let timer;
  const alive = () => { clearTimeout(timer); timer = setTimeout(() => abort.abort(), idleMs); };
  const hash = crypto.createHash('sha256');
  let received = 0;
  let handle;
  try {
    alive();
    // The asset is addressed by its id on the API host, never by a URL taken from the response.
    const res = await request(fetchFn, `${api}/repos/${repo}/releases/assets/${latest.assetId}`, 'application/octet-stream', abort.signal);
    handle = await fs.promises.open(part, 'w');
    try {
      for await (const chunk of res.body) {
        alive();
        received += chunk.length;
        if (received > latest.size) break; // more than the release lists: not the announced file
        hash.update(chunk);
        await handle.write(chunk);
        onProgress(received, latest.size);
      }
    } catch (err) {
      const reason = abort.signal.aborted ? t('no data for {n} s', { n: Math.round(idleMs / 1000) }) : err.cause?.code || err.code || err.name || err.message;
      throw new Error(t('The download of the update stopped ({reason})', { reason }));
    }
    await handle.close();
    handle = null;
    if (received !== latest.size || hash.digest('hex') !== latest.sha256) throw new Error(t('The downloaded installer does not match its checksum.'));
    fs.renameSync(part, file);
  } finally {
    clearTimeout(timer);
    abort.abort(); // ends a download that was left early
    if (handle) await handle.close().catch(() => {});
    fs.rmSync(part, { force: true });
  }
  cleanup(dir, null, name);
  return file;
}

// Removes what is no longer needed in the update folder: unfinished downloads, and installers of versions
// that are not newer than `current` (the update is done). `keep` names the one file that stays whatever
// its version; with it and without `current` every other installer goes.
function cleanup(dir, current, keep = null) {
  let names;
  try { names = fs.readdirSync(dir); } catch { return; }
  for (const name of names) {
    if (name === keep) continue;
    const version = (name.match(INSTALLER) || [])[1];
    const stale = name.endsWith('.part') || (version && (!current || compareVersions(version, current) <= 0));
    if (!stale) continue;
    // Right after an update the installer still runs and Windows does not let go of its file: it stays
    // for the next tidying, and the other files go now.
    try { fs.rmSync(path.join(dir, name), { force: true }); } catch { /* next time */ }
  }
}

// ---------- installer ----------

// What the NSIS installer of electron-builder reads: --updated = an update (no pages, shortcuts stay as the
// user left them), /S = silent, --force-run = start the app when done. It installs where the app is, waits
// for the running app to exit and asks for elevation by itself if the app was installed for all users.
const installerArgs = (relaunch) => ['--updated', '/S', ...(relaunch ? ['--force-run'] : [])];

function launch(file, { relaunch = true, spawnFn = spawn } = {}) {
  return new Promise((resolve, reject) => {
    const failed = (err) => reject(new Error(t('The installer could not be started ({reason})', { reason: err.code || err.message })));
    let child;
    try {
      child = spawnFn(file, installerArgs(relaunch), { detached: true, stdio: 'ignore' });
    } catch (err) {
      failed(err);
      return;
    }
    child.once('error', failed);
    child.once('spawn', () => { child.unref(); resolve(); });
  });
}

// ---------- the updater ----------

// Holds what the window shows and runs the steps one at a time.
//   reason     why this app cannot install an update: 'source' (not the installed app), 'platform' (no
//              Windows), or null. It still checks and shows what is new.
//   automatic  () => download a found update right away and install it when the app closes?
//   onState    called with every new state:
//              { current, canInstall, reason, automatic, updated, phase, latest, checkedAt, progress, error }
//              phase: idle (never checked) | checking | current | available | downloading | ready | installing
//              latest: { version, tag, url, publishedAt, size } | null, progress: { received, total } | null
function createUpdater({ dir, current, reason = null, automatic = () => true, onState = () => {}, updated = false,
  launchFn = launch, ...source }) {
  const canInstall = !reason;
  const s = { current, canInstall, reason, updated: !!updated, phase: 'idle', latest: null, checkedAt: null, progress: null, error: null };
  let full = null; // the release as check() gave it, with asset id and checksum
  let file = null; // the downloaded installer, once it is there
  const state = () => ({ ...s, automatic: !!automatic(), latest: s.latest && { ...s.latest }, progress: s.progress && { ...s.progress } });
  const set = (patch) => { Object.assign(s, patch); const now = state(); onState(now); return now; };
  const busy = () => ['checking', 'downloading', 'installing'].includes(s.phase);
  cleanup(dir, current);
  // The installer starts the updated app before it exits itself, so its file is still locked now.
  if (updated) setTimeout(() => cleanup(dir, current), 60000).unref();

  async function checkNow(force = false) {
    if (busy()) return state();
    cleanup(dir, current);
    set({ phase: 'checking', error: null });
    const r = await check({ dir, current, force, ...source });
    full = r.latest;
    const latest = full && { version: full.version, tag: full.tag, url: releaseUrl(full.version, source.repo), publishedAt: full.publishedAt, size: full.size };
    const seen = { latest, checkedAt: r.checkedAt, error: r.error || null, progress: null };
    if (!r.available) return set({ ...seen, phase: full ? 'current' : 'idle' });
    const target = path.join(dir, installerName(full.version));
    if (canInstall && await matches(target, full)) {
      file = target;
      return set({ ...seen, phase: 'ready' });
    }
    set({ ...seen, phase: 'available' });
    return canInstall && automatic() && !r.error ? downloadNow() : state();
  }

  async function downloadNow() {
    if (s.phase !== 'available' || !canInstall || !full) return state();
    set({ phase: 'downloading', error: null, progress: { received: 0, total: full.size } });
    let told = 0;
    try {
      file = await download({ dir, latest: full, ...source, onProgress: (received, total) => {
        if (Date.now() - told < 250) return;
        told = Date.now();
        set({ progress: { received, total } });
      } });
      return set({ phase: 'ready', progress: null });
    } catch (err) {
      return set({ phase: 'available', progress: null, error: err.message });
    }
  }

  // Starts the installer; `started` tells the caller to quit the app so the installer can replace it.
  async function install({ relaunch = true } = {}) {
    if (s.phase !== 'ready' || !canInstall) return { ...state(), started: false };
    set({ phase: 'installing', error: null });
    try {
      // The file waited in the data folder, perhaps for days: it is checked again right before it runs.
      if (!await matches(file, full)) {
        fs.rmSync(file, { force: true });
        throw new Error(t('The downloaded installer does not match its checksum.'));
      }
      await launchFn(file, { relaunch });
      return { ...state(), started: true };
    } catch (err) {
      return { ...set({ phase: fs.existsSync(file) ? 'ready' : 'available', error: err.message }), started: false };
    }
  }

  return {
    state,
    check: checkNow,
    download: downloadNow,
    install,
    // A downloaded update goes in when the app closes, unless the user turned the automatic off.
    installsOnQuit: () => s.phase === 'ready' && canInstall && !!automatic(),
    // The switch "automatically" changed: tell the window, and fetch an update that was only shown so far.
    settingsChanged: () => (s.phase === 'available' && canInstall && automatic() ? downloadNow() : set({})),
  };
}

module.exports = { REPO, check, download, cleanup, launch, installerArgs, installerName, releaseUrl, parseSums, createUpdater };
