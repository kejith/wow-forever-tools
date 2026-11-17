// Installing the addons the app ships (WoWMCP, GoldPerHour) and writing app requests. Pure file operations,
// no Electron, so they can be tested against a scratch folder: node addon.test.js
const fs = require('node:fs');
const path = require('node:path');

const REQUEST_FILE = 'AppRequest.lua';

// Every addon the app can install. Each one ships an AppRequest.lua stub that the app later overwrites with
// a request; `global` is the variable the addon reads from it.
const ADDONS = {
  wowmcp: { name: 'WoWMCP', global: 'WoWMCP_AppRequest', obsolete: ['ItemQueue.lua'] },
  goldperhour: { name: 'GoldPerHour', global: 'GoldPerHour_AppRequest', obsolete: [] },
};

function spec(key) {
  const s = ADDONS[key];
  if (!s) throw new Error(`Unbekanntes Addon: ${key}`);
  return s;
}

const sourceDir = (key = 'wowmcp') => path.resolve(__dirname, '..', 'addon', spec(key).name);
const ADDON_SOURCE = sourceDir('wowmcp');
const installedAddonDir = (wowDir, key = 'wowmcp') => path.join(wowDir, 'Interface', 'AddOns', spec(key).name);
const tocName = (key) => `${spec(key).name}.toc`;

function tocVersion(dir, key) {
  try {
    const toc = fs.readFileSync(path.join(dir, tocName(key)), 'utf8');
    return (toc.match(/^## Version:\s*(.+)$/m) || [])[1]?.trim() || null;
  } catch {
    return null;
  }
}

function tocFiles(dir, key) {
  const toc = fs.readFileSync(path.join(dir, tocName(key)), 'utf8');
  return toc.split(/\r?\n/).map((l) => l.trim()).filter((l) => l && !l.startsWith('#'));
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

function addonInfo(wowDir, key = 'wowmcp') {
  const source = sourceDir(key);
  const target = installedAddonDir(wowDir, key);
  const installed = fs.existsSync(path.join(target, tocName(key)));
  const sourceFiles = [tocName(key), ...tocFiles(source, key)].filter((f) => f !== REQUEST_FILE);
  const changed = [];
  for (const f of sourceFiles) {
    let a = null;
    let b = null;
    try { a = fs.readFileSync(path.join(source, f)); } catch { /* missing in source */ }
    try { b = fs.readFileSync(path.join(target, f)); } catch { /* missing in install */ }
    if (!a || !b || !a.equals(b)) changed.push(f);
  }
  // Forever re-reads the .toc on /reload (tested 15.11.2026: new files and new SavedVariables load), so an
  // update never needs a game restart.
  return {
    key, name: spec(key).name, sourceDir: source, installedDir: target, installed,
    sourceVersion: tocVersion(source, key), installedVersion: installed ? tocVersion(target, key) : null,
    changed, upToDate: installed && changed.length === 0,
    auctionatorInstalled: fs.existsSync(path.join(wowDir, 'Interface', 'AddOns', 'Auctionator')),
    pendingRequest: readRequest(wowDir, key),
  };
}

function installAddon(wowDir, key = 'wowmcp') {
  const source = sourceDir(key);
  const target = installedAddonDir(wowDir, key);
  const before = addonInfo(wowDir, key);
  fs.mkdirSync(target, { recursive: true });
  for (const f of [tocName(key), ...tocFiles(source, key)]) {
    // Keep a request the app already wrote; only ship the empty stub on first install.
    if (f === REQUEST_FILE && fs.existsSync(path.join(target, f))) continue;
    fs.copyFileSync(path.join(source, f), path.join(target, f));
  }
  for (const f of spec(key).obsolete) fs.rmSync(path.join(target, f), { force: true });
  return { ...addonInfo(wowDir, key), firstInstall: !before.installed };
}

function nextId(wowDir, key) {
  // Seconds since 1970, but always above the previous id so two quick clicks never share one.
  return Math.max(Math.floor(Date.now() / 1000), (readRequest(wowDir, key)?.id || 0) + 1);
}

function writeFile(wowDir, key, fields) {
  const target = installedAddonDir(wowDir, key);
  if (!fs.existsSync(path.join(target, tocName(key)))) throw new Error(`${spec(key).name} ist nicht installiert.`);
  const body = '-- Written by the WoWMCP desktop app. The client re-reads this file on /reload; a request runs once.\n'
    + `${spec(key).global} = { ${fields.join(', ')} }\n`;
  fs.writeFileSync(path.join(target, REQUEST_FILE), body);
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
  if (action != null && !GPH_ACTIONS.includes(action)) throw new Error(`Unbekannte Aktion: ${action}`);
  const settingsLua = settings ? gphSettingsLua(settings) : [];
  if (!action && !settingsLua.length) throw new Error('Nichts zu senden.');
  const id = nextId(wowDir, 'goldperhour');
  const fields = [`id = ${id}`];
  if (action) fields.push(`action = "${action}"`);
  if (settingsLua.length) fields.push(`settings = { ${settingsLua.join(', ')} }`);
  writeFile(wowDir, 'goldperhour', fields);
  return { id, action: action || null, settings: settingsLua.length > 0 };
}

// Withdraws a request the game has not read yet: the stub again, so nothing runs on the next /reload.
function clearGphRequest(wowDir) {
  const target = installedAddonDir(wowDir, 'goldperhour');
  if (!fs.existsSync(path.join(target, tocName('goldperhour')))) throw new Error('GoldPerHour ist nicht installiert.');
  fs.copyFileSync(path.join(sourceDir('goldperhour'), REQUEST_FILE), path.join(target, REQUEST_FILE));
  return { cleared: true };
}

module.exports = {
  ADDONS, ADDON_SOURCE, sourceDir, installedAddonDir, addonInfo, installAddon, writeRequest, readRequest,
  writeGphRequest, clearGphRequest, gphSettingsLua,
};
