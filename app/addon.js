// Installing the WoWMCP addon and writing app requests. Pure file operations, no Electron, so they can be
// tested against a scratch folder: node addon.test.js
const fs = require('node:fs');
const path = require('node:path');

const ADDON_SOURCE = path.resolve(__dirname, '..', 'addon', 'WoWMCP');
const REQUEST_FILE = 'AppRequest.lua';
const OBSOLETE_ADDON_FILES = ['ItemQueue.lua'];

const installedAddonDir = (wowDir) => path.join(wowDir, 'Interface', 'AddOns', 'WoWMCP');

function tocVersion(dir) {
  try {
    const toc = fs.readFileSync(path.join(dir, 'WoWMCP.toc'), 'utf8');
    return (toc.match(/^## Version:\s*(.+)$/m) || [])[1]?.trim() || null;
  } catch {
    return null;
  }
}

function tocFiles(dir) {
  const toc = fs.readFileSync(path.join(dir, 'WoWMCP.toc'), 'utf8');
  return toc.split(/\r?\n/).map((l) => l.trim()).filter((l) => l && !l.startsWith('#'));
}

function readRequest(wowDir) {
  try {
    const text = fs.readFileSync(path.join(installedAddonDir(wowDir), REQUEST_FILE), 'utf8');
    const id = text.match(/\bid\s*=\s*(\d+)/);
    const action = text.match(/action\s*=\s*"(\w+)"/);
    return id ? { id: Number(id[1]), action: action ? action[1] : null } : null;
  } catch {
    return null;
  }
}

function addonInfo(wowDir, source = ADDON_SOURCE) {
  const target = installedAddonDir(wowDir);
  const installed = fs.existsSync(path.join(target, 'WoWMCP.toc'));
  const sourceFiles = ['WoWMCP.toc', ...tocFiles(source)].filter((f) => f !== REQUEST_FILE);
  const changed = [];
  for (const f of sourceFiles) {
    let a = null;
    let b = null;
    try { a = fs.readFileSync(path.join(source, f)); } catch { /* missing in source */ }
    try { b = fs.readFileSync(path.join(target, f)); } catch { /* missing in install */ }
    if (!a || !b || !a.equals(b)) changed.push(f);
  }
  // A changed file list in the .toc only takes effect after restarting the game, not after /reload.
  let restartNeeded = false;
  if (installed) {
    try {
      restartNeeded = tocFiles(target).join('|') !== tocFiles(source).join('|')
        || !/SavedVariables: WoWMCP_Account/.test(fs.readFileSync(path.join(target, 'WoWMCP.toc'), 'utf8'));
    } catch { restartNeeded = true; }
  }
  return {
    sourceDir: source, installedDir: target, installed,
    sourceVersion: tocVersion(source), installedVersion: installed ? tocVersion(target) : null,
    changed, upToDate: installed && changed.length === 0, restartNeeded,
    auctionatorInstalled: fs.existsSync(path.join(wowDir, 'Interface', 'AddOns', 'Auctionator')),
    pendingRequest: readRequest(wowDir),
  };
}

function installAddon(wowDir, source = ADDON_SOURCE) {
  const target = installedAddonDir(wowDir);
  const before = addonInfo(wowDir, source);
  fs.mkdirSync(target, { recursive: true });
  for (const f of ['WoWMCP.toc', ...tocFiles(source)]) {
    // Keep a request the app already wrote; only ship the empty stub on first install.
    if (f === REQUEST_FILE && fs.existsSync(path.join(target, f))) continue;
    fs.copyFileSync(path.join(source, f), path.join(target, f));
  }
  for (const f of OBSOLETE_ADDON_FILES) fs.rmSync(path.join(target, f), { force: true });
  return { ...addonInfo(wowDir, source), restartNeeded: before.restartNeeded || !before.installed };
}

function writeRequest(wowDir, action, opts = {}) {
  const target = installedAddonDir(wowDir);
  if (!fs.existsSync(path.join(target, 'WoWMCP.toc'))) throw new Error('Addon ist nicht installiert.');
  const previous = readRequest(wowDir);
  // Seconds since 1970, but always above the previous id so two quick clicks never share one.
  const id = Math.max(Math.floor(Date.now() / 1000), (previous?.id || 0) + 1);
  const fields = [`id = ${id}`, `action = "${action === 'sync' ? 'sync' : 'items'}"`];
  if (opts.full) fields.push('full = true');
  const body = '-- Written by the WoWMCP desktop app. The client re-reads this file on /reload; a request runs once.\n'
    + `WoWMCP_AppRequest = { ${fields.join(', ')} }\n`;
  fs.writeFileSync(path.join(target, REQUEST_FILE), body);
  return { id, action };
}

module.exports = { ADDON_SOURCE, installedAddonDir, addonInfo, installAddon, writeRequest, readRequest };
