// Installing the addons the app works with (WoWMCP, GoldPerHour) and writing app requests. The addons live
// in their own repositories; the app installs the latest GitHub release (releases.js) or, for development,
// a local checkout. No Electron, so everything can be tested against a scratch folder: node addon.test.js
const fs = require('node:fs');
const path = require('node:path');
const { t } = require('./i18n');
const { latestRelease, tocVersion: versionOf, compareVersions } = require('./releases');

const REQUEST_FILE = 'AppRequest.lua';

// Every addon the app can install. Each one ships an AppRequest.lua stub that the app later overwrites with
// a request; `global` is the variable the addon reads from it. `env` names a local checkout that takes the
// place of the release.
const ADDONS = {
  wowmcp: { name: 'WoWMCP', repo: 'kejith/WoWMCP', env: 'WOWMCP_ADDON_DIR', global: 'WoWMCP_AppRequest', obsolete: ['ItemQueue.lua'] },
  goldperhour: { name: 'GoldPerHour', repo: 'kejith/GoldPerHour', env: 'GOLDPERHOUR_ADDON_DIR', global: 'GoldPerHour_AppRequest', obsolete: [] },
};

function spec(key) {
  const s = ADDONS[key];
  if (!s) throw new Error(t('Unknown addon: {key}', { key }));
  return s;
}

const installedAddonDir = (wowDir, key = 'wowmcp') => path.join(wowDir, 'Interface', 'AddOns', spec(key).name);
const tocName = (key) => `${spec(key).name}.toc`;
const tocVersion = (dir, key) => versionOf(dir, spec(key).name);

// Where the addon's files come from. A local checkout (opts.sources[key] from the app settings, or the
// environment variable) wins, so a change can be tried in the game without publishing a release. Otherwise
// the latest GitHub release, cached under opts.cacheDir. opts.force asks GitHub now instead of using the
// last answer.
async function resolveSource(key, opts = {}) {
  const s = spec(key);
  const local = opts.sources?.[key] || process.env[s.env];
  if (local) {
    const dir = path.resolve(local);
    if (!fs.existsSync(path.join(dir, tocName(key)))) throw new Error(t('Local source without {toc}: {dir}', { toc: tocName(key), dir }));
    return { kind: 'local', dir, repo: s.repo, version: tocVersion(dir, key) };
  }
  return latestRelease({ repo: s.repo, name: s.name, cacheDir: opts.cacheDir, api: opts.api, fetchFn: opts.fetchFn,
    maxAge: opts.maxAge, force: opts.force });
}

// Files the .toc lists, with forward slashes. A path that leaves the addon folder is refused: these names
// decide where the installer writes.
function tocFiles(dir, key) {
  const toc = fs.readFileSync(path.join(dir, tocName(key)), 'utf8');
  const files = toc.split(/\r?\n/).map((l) => l.trim().replace(/\\/g, '/')).filter((l) => l && !l.startsWith('#'));
  const bad = files.find((f) => f.startsWith('/') || f.includes(':') || f.split('/').includes('..'));
  if (bad) throw new Error(t('Path not allowed in {toc}: {path}', { toc: tocName(key), path: bad }));
  return files;
}

function requestText(wowDir, key) {
  try {
    // Only the assignment counts; the stub's comment shows an example request.
    const re = new RegExp(`^\\s*${spec(key).global}\\s*=`);
    return fs.readFileSync(path.join(installedAddonDir(wowDir, key), REQUEST_FILE), 'utf8')
      .split(/\r?\n/).filter((l) => re.test(l)).join('\n');
  } catch {
    return '';
  }
}

function readRequest(wowDir, key = 'wowmcp') {
  const text = requestText(wowDir, key);
  const id = text.match(/\bid\s*=\s*(\d+)/);
  if (!id) return null;
  const action = text.match(/action\s*=\s*"(\w+)"/);
  const out = { id: Number(id[1]), action: action ? action[1] : null };
  if (key !== 'wowmcp' && /\bsettings\s*=/.test(text)) out.settings = true;
  return out;
}

function changedFiles(source, target, key) {
  const changed = [];
  for (const f of [tocName(key), ...tocFiles(source, key)].filter((x) => x !== REQUEST_FILE)) {
    let a = null;
    let b = null;
    try { a = fs.readFileSync(path.join(source, f)); } catch { /* missing in source */ }
    try { b = fs.readFileSync(path.join(target, f)); } catch { /* missing in install */ }
    if (!a || !b || !a.equals(b)) changed.push(f);
  }
  return changed;
}

async function addonInfo(wowDir, key = 'wowmcp', opts = {}) {
  const target = installedAddonDir(wowDir, key);
  const installed = fs.existsSync(path.join(target, tocName(key)));
  const installedVersion = installed ? tocVersion(target, key) : null;
  let source = null;
  let sourceError = null;
  let changed = [];
  try {
    const found = await resolveSource(key, opts);
    changed = changedFiles(found.dir, target, key);
    source = found;
  } catch (err) {
    sourceError = err.message;
  }
  // Installed from a local checkout that is ahead of the release: nothing to update.
  const ahead = !!source && installed && changed.length > 0 && compareVersions(installedVersion, source.version) > 0;
  // Forever re-reads the .toc on /reload (tested 2026-11-15: new files and new SavedVariables load), so an
  // update never needs a game restart.
  return {
    key, name: spec(key).name, repo: spec(key).repo, installedDir: target, installed, installedVersion,
    source, sourceError, sourceDir: source?.dir || null, sourceVersion: source?.version || null,
    // Without a source there is nothing to compare against; an installed addon then counts as current.
    changed, ahead, upToDate: installed && (changed.length === 0 || ahead),
    auctionatorInstalled: fs.existsSync(path.join(wowDir, 'Interface', 'AddOns', 'Auctionator')),
    pendingRequest: readRequest(wowDir, key),
  };
}

async function installAddon(wowDir, key = 'wowmcp', opts = {}) {
  const source = await resolveSource(key, opts);
  const target = installedAddonDir(wowDir, key);
  const wasInstalled = fs.existsSync(path.join(target, tocName(key)));
  const files = [tocName(key), ...tocFiles(source.dir, key)];
  const missing = files.filter((f) => !fs.existsSync(path.join(source.dir, f)));
  if (missing.length) throw new Error(t('The source lacks files listed in the .toc: {files}', { files: missing.join(', ') }));
  fs.mkdirSync(target, { recursive: true });
  for (const f of files) {
    // Keep a request the app already wrote; only ship the empty stub on first install.
    if (f === REQUEST_FILE && fs.existsSync(path.join(target, f))) continue;
    fs.mkdirSync(path.dirname(path.join(target, f)), { recursive: true });
    fs.copyFileSync(path.join(source.dir, f), path.join(target, f));
  }
  for (const f of spec(key).obsolete) fs.rmSync(path.join(target, f), { force: true });
  return { ...(await addonInfo(wowDir, key, { ...opts, force: false })), firstInstall: !wasInstalled };
}

function nextId(wowDir, key) {
  // Seconds since 1970, but always above the previous id so two quick clicks never share one.
  return Math.max(Math.floor(Date.now() / 1000), (readRequest(wowDir, key)?.id || 0) + 1);
}

const REQUEST_NOTE = '-- Written by the WoW Forever Tools desktop app. The client re-reads this file on /reload; a request runs once.\n';

function writeFile(wowDir, key, fields) {
  const target = installedAddonDir(wowDir, key);
  if (!fs.existsSync(path.join(target, tocName(key)))) throw new Error(t('{name} is not installed.', { name: spec(key).name }));
  const value = fields ? `{ ${fields.join(', ')} }` : 'nil';
  fs.writeFileSync(path.join(target, REQUEST_FILE), `${REQUEST_NOTE}${spec(key).global} = ${value}\n`);
}

function writeRequest(wowDir, action, opts = {}) {
  const id = nextId(wowDir, 'wowmcp');
  const fields = [`id = ${id}`, `action = "${action === 'sync' ? 'sync' : 'items'}"`];
  if (opts.full) fields.push('full = true');
  writeFile(wowDir, 'wowmcp', fields);
  return { id, action };
}

// ---------- GoldPerHour ----------

const GPH_ACTIONS = ['start', 'stop', 'reset'];

// Only known settings with the right type reach the file; strings are limited to the allowed values, so
// nothing from the page can end up as Lua code.
function gphSettingsLua(s) {
  const out = [];
  if (typeof s.shown === 'boolean') out.push(`shown = ${s.shown}`);
  if (typeof s.resumeAfterReload === 'boolean') out.push(`resumeAfterReload = ${s.resumeAfterReload}`);
  if (typeof s.autoStart === 'boolean') out.push(`autoStart = ${s.autoStart}`);
  const scale = Number(s.scale);
  if (s.scale !== undefined && Number.isFinite(scale)) out.push(`scale = ${Math.min(2, Math.max(0.5, Math.round(scale * 100) / 100))}`);
  const items = Number(s.tooltipItems);
  if (s.tooltipItems !== undefined && Number.isFinite(items)) out.push(`tooltipItems = ${Math.min(30, Math.max(1, Math.round(items)))}`);
  if (['auctionator', 'vendor'].includes(s.priceSource)) out.push(`priceSource = "${s.priceSource}"`);
  if (Array.isArray(s.herbSpells)) {
    const ids = [...new Set(s.herbSpells.map(Number).filter((n) => Number.isInteger(n) && n > 0))];
    out.push(`herbSpells = { ${ids.join(', ')} }`);
  }
  return out;
}

function writeGphRequest(wowDir, { action, settings } = {}) {
  if (action != null && !GPH_ACTIONS.includes(action)) throw new Error(t('Unknown action: {action}', { action }));
  const settingsLua = settings ? gphSettingsLua(settings) : [];
  if (!action && !settingsLua.length) throw new Error(t('Nothing to send.'));
  const id = nextId(wowDir, 'goldperhour');
  const fields = [`id = ${id}`];
  if (action) fields.push(`action = "${action}"`);
  if (settingsLua.length) fields.push(`settings = { ${settingsLua.join(', ')} }`);
  writeFile(wowDir, 'goldperhour', fields);
  return { id, action: action || null, settings: settingsLua.length > 0 };
}

// Withdraws a request the game has not read yet: an empty request, so nothing runs on the next /reload.
function clearGphRequest(wowDir) {
  writeFile(wowDir, 'goldperhour', null);
  return { cleared: true };
}

module.exports = {
  ADDONS, resolveSource, installedAddonDir, addonInfo, installAddon, writeRequest, readRequest,
  writeGphRequest, clearGphRequest, gphSettingsLua,
};
