// WoW Forever Tools, the desktop app: main process.
// Talks to the game only through files: it reads the SavedVariables the WoWMCP and GoldPerHour addons and
// Auctionator write on /reload or logout, and it writes AppRequest.lua into the installed addons, which the
// client loads on the next /reload. All calculations run in the Python scripts via scripts/app_bridge.py.
// The addons themselves come from their GitHub releases (addon.js, releases.js), cached in the app's data
// folder; settings.addonSources = { wowmcp: <folder>, goldperhour: <folder> } installs from a checkout instead.
// Language: settings.language (auto, en, de); auto follows the game client (textLocale in WTF/Config.wtf) and,
// without a client, the operating system. The bridge gets the same language, so its texts match the page.

const { app, BrowserWindow, ipcMain, net, protocol, shell } = require('electron');
const { execFile } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const addon = require('./addon');
const i18n = require('./i18n');

const { t } = i18n;

// The Python scripts: in the installed app next to app.asar (extraResources), otherwise in the checkout.
const ROOT = app.isPackaged ? process.resourcesPath : path.resolve(__dirname, '..');
const BRIDGE = path.join(ROOT, 'scripts', 'app_bridge.py');
// Under WSL and Linux the Windows install is reached through /mnt/c.
const DEFAULT_WOW = process.platform === 'win32'
  ? 'C:\\Program Files (x86)\\World of Warcraft\\_forever_' : '/mnt/c/Program Files (x86)/World of Warcraft/_forever_';

// The data folder keeps the name it had before the app was renamed, so settings and downloaded addons stay.
app.setPath('userData', path.join(app.getPath('appData'), 'wowmcp-app'));
// Downloaded game data and icons (scripts/icons.py). The installed app cannot write next to its scripts.
const cacheDir = () => (app.isPackaged ? path.join(app.getPath('userData'), 'cache') : path.join(ROOT, 'scripts', 'cache'));
const argValue = (name) => (process.argv.find((a) => a.startsWith(`--${name}=`)) || '').split('=')[1] || '';

// Item icons reach the page as wowicon://<FileDataID>.png, served from the cache scripts/icons.py fills.
protocol.registerSchemesAsPrivileged([{ scheme: 'wowicon', privileges: { standard: true, secure: true } }]);

let win;
let settings = {};
let language = 'en';
const settingsFile = () => path.join(app.getPath('userData'), 'settings.json');

function loadSettings() {
  try {
    settings = JSON.parse(fs.readFileSync(settingsFile(), 'utf8'));
  } catch {
    settings = {};
  }
  settings.wowDir ||= process.env.WOW_FOREVER_DIR || DEFAULT_WOW;
  applyLanguage();
}

// --lang=en|de on the command line (screenshots) wins over the setting; "auto" follows the game client.
function applyLanguage() {
  const wanted = argValue('lang') || settings.language;
  const fixed = i18n.LANGUAGES.includes(wanted) ? wanted : null;
  language = i18n.setLanguage(fixed || i18n.languageOf(i18n.clientLocale(settings.wowDir) || app.getLocale()));
}

const publicSettings = () => ({
  ...settings, language: i18n.LANGUAGES.includes(settings.language) ? settings.language : 'auto',
  activeLanguage: language, clientLocale: i18n.clientLocale(settings.wowDir), version: app.getVersion(),
});

function saveSettings() {
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true });
  fs.writeFileSync(settingsFile(), JSON.stringify(settings, null, 2));
}

// ---------- Python bridge ----------

function uvCandidates() {
  const list = [];
  if (settings.uvPath) list.push(settings.uvPath);
  // The installer ships uv; it fetches Python and the scripts' dependencies on the first call.
  const bundled = path.join(process.resourcesPath || '', 'bin', process.platform === 'win32' ? 'uv.exe' : 'uv');
  if (app.isPackaged && fs.existsSync(bundled)) list.push(bundled);
  list.push('uv');
  const local = process.env.LOCALAPPDATA;
  if (local) {
    const winget = path.join(local, 'Microsoft', 'WinGet', 'Packages');
    try {
      for (const dir of fs.readdirSync(winget)) {
        if (dir.startsWith('astral-sh.uv')) list.push(path.join(winget, dir, 'uv.exe'));
      }
    } catch { /* no winget packages */ }
  }
  if (process.env.USERPROFILE) list.push(path.join(process.env.USERPROFILE, '.local', 'bin', 'uv.exe'));
  return list;
}

function execUv(uv, args) {
  return new Promise((resolve, reject) => {
    execFile(uv, args, {
      cwd: ROOT, timeout: 300000, maxBuffer: 64 * 1024 * 1024, windowsHide: true,
      env: { ...process.env, PYTHONIOENCODING: 'utf-8', WOW_TOOLS_CACHE: cacheDir() },
    }, (err, stdout, stderr) => {
      if (err && err.code === 'ENOENT') return reject(err);
      const text = stdout.trim();
      if (!text) return reject(new Error((stderr || String(err || t('no output'))).trim().split('\n').slice(-3).join('\n')));
      try {
        resolve(JSON.parse(text.split('\n').pop()));
      } catch {
        reject(new Error(t('Invalid answer from the bridge: {text}', { text: text.slice(0, 200) })));
      }
    });
  });
}

let workingUv;
async function runBridge(command, opts = {}) {
  const args = ['run', '--quiet', BRIDGE, command, '--wow', settings.wowDir, '--lang', language];
  if (opts.char) args.push('--char', opts.char);
  if (opts.top) args.push('--top', String(opts.top));
  if (opts.maxPrice) args.push('--max-price', String(opts.maxPrice));
  if (opts.preview !== undefined) args.push('--preview', String(opts.preview));
  if (opts.ownedAtAhPrice) args.push('--owned-at-ah-price');
  if (['leveling', 'dungeon'].includes(opts.profile)) args.push('--profile', opts.profile);
  if (['gain', 'price'].includes(opts.sort)) args.push('--sort', opts.sort);
  if (typeof opts.profession === 'string' && opts.profession) args.push('--profession', opts.profession);
  if (Array.isArray(opts.ids)) args.push('--ids', opts.ids.map(Number).filter((n) => n > 0).join(','));
  const candidates = workingUv ? [workingUv] : uvCandidates();
  let lastErr;
  for (const uv of candidates) {
    try {
      const result = await execUv(uv, args);
      workingUv = uv;
      return result;
    } catch (err) {
      lastErr = err;
      if (err.code !== 'ENOENT') break;
    }
  }
  if (lastErr && lastErr.code === 'ENOENT') {
    return { error: t('uv was not found. Install it with: winget install astral-sh.uv') };
  }
  return { error: lastErr ? lastErr.message : t('The bridge failed') };
}

// ---------- Item icons ----------
// Item ID -> FileDataID, remembered for the session. Unknown IDs go to the bridge in one batch, which also
// downloads the PNGs; IDs without an icon are not asked for again until the app restarts.

const iconOf = new Map();
let iconFallback = null;
let iconQueue = Promise.resolve();

function resolveIcons(ids) {
  const run = async () => {
    const unknown = [...new Set(ids.map(Number))].filter((id) => id > 0 && !iconOf.has(id));
    if (unknown.length) {
      const r = await runBridge('icons', { ids: unknown });
      if (r.error) return { error: r.error };
      for (const id of unknown) iconOf.set(id, r.icons?.[id] ?? null);
      if (r.fallback) iconFallback = r.fallback;
    }
    const icons = {};
    for (const id of ids) icons[id] = iconOf.get(Number(id)) ?? null;
    return { icons, fallback: iconFallback };
  };
  // One bridge call at a time, so two pages asking for the same icons do not download them twice.
  iconQueue = iconQueue.then(run, run);
  return iconQueue;
}

function serveIcons() {
  protocol.handle('wowicon', (request) => {
    const name = new URL(request.url).hostname;
    if (!/^\d+\.png$/.test(name)) return new Response('', { status: 404 });
    return net.fetch(pathToFileURL(path.join(cacheDir(), 'icons', name)).toString());
  });
}

// ---------- Game process ----------

function wowRunning() {
  return new Promise((resolve) => {
    execFile('powershell.exe', ['-NoProfile', '-Command',
      '(Get-Process -Name Wow -ErrorAction SilentlyContinue | ForEach-Object { $_.Path }) -join "|"'],
    { windowsHide: true, timeout: 15000 }, (err, stdout) => {
      if (err) return resolve({ running: false, unknown: true });
      const paths = stdout.trim().split('|').filter(Boolean);
      const forever = paths.some((p) => p.toLowerCase().includes('_forever_'));
      resolve({ running: forever, otherClient: !forever && paths.length > 0 });
    });
  });
}

// ---------- File watcher ----------

let watcher;
let debounce;
function startWatcher() {
  if (watcher) watcher.close();
  const dir = path.join(settings.wowDir, 'WTF', 'Account');
  try {
    watcher = fs.watch(dir, { recursive: true }, (_event, file) => {
      if (!file || !/(WoWMCP|Auctionator|GoldPerHour)\.lua$/i.test(file)) return;
      clearTimeout(debounce);
      // WoW writes several SavedVariables files in a row on /reload; wait until it is done.
      debounce = setTimeout(() => win?.webContents.send('data-changed', { file }), 1500);
    });
  } catch (err) {
    console.error('watch failed', err);
  }
}

// ---------- IPC ----------

ipcMain.handle('bridge', (_e, command, opts) => runBridge(command, opts));
const addonKey = (key) => (Object.hasOwn(addon.ADDONS, key) ? key : 'wowmcp');
const addonOpts = (force) => ({
  cacheDir: path.join(app.getPath('userData'), 'addons'), sources: settings.addonSources || {}, force: force === true,
});
ipcMain.handle('addon:info', (_e, key, opts) => addon.addonInfo(settings.wowDir, addonKey(key), addonOpts(opts?.force)));
ipcMain.handle('addon:install', async (_e, key) => {
  try { return await addon.installAddon(settings.wowDir, addonKey(key), addonOpts()); } catch (err) { return { error: err.message }; }
});
ipcMain.handle('gph:request', (_e, req) => {
  try { return addon.writeGphRequest(settings.wowDir, req || {}); } catch (err) { return { error: err.message }; }
});
ipcMain.handle('gph:clear', () => {
  try { return addon.clearGphRequest(settings.wowDir); } catch (err) { return { error: err.message }; }
});
ipcMain.handle('request:create', (_e, action, opts) => {
  try { return addon.writeRequest(settings.wowDir, action, opts); } catch (err) { return { error: err.message }; }
});
ipcMain.handle('icons', (_e, ids) => resolveIcons(Array.isArray(ids) ? ids : []));
ipcMain.handle('wow:running', () => wowRunning());
ipcMain.handle('open:path', (_e, p) => shell.openPath(p));
ipcMain.handle('settings:get', () => publicSettings());
ipcMain.handle('settings:set', (_e, patch) => {
  const { activeLanguage, clientLocale, version, ...rest } = patch || {};
  Object.assign(settings, rest);
  saveSettings();
  applyLanguage();
  startWatcher();
  return publicSettings();
});

// ---------- Window ----------

function createWindow() {
  win = new BrowserWindow({
    width: 1280, height: 860, minWidth: 980, minHeight: 640, backgroundColor: '#14110f',
    title: 'WoW Forever Tools', autoHideMenuBar: true,
    webPreferences: { preload: path.join(__dirname, 'preload.js'), contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  win.loadFile(path.join(__dirname, 'renderer', 'index.html'));
  const capture = process.argv.find((a) => a.startsWith('--capture='));
  if (capture) win.webContents.once('did-finish-load', () => runCapture(capture.split('=')[1]));
}

// "npm run capture": renders every page once and saves screenshots, for checking the UI without clicking.
// With --lang=en or --lang=de the pages come in that language, whatever the settings say.
async function runCapture(dir) {
  const out = path.resolve(__dirname, dir);
  fs.mkdirSync(out, { recursive: true });
  const pages = ['connection', 'character', 'writs', 'upgrades', 'recipes', 'goldperhour'];
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  for (const page of pages) {
    await win.webContents.executeJavaScript(`window.__showPage(${JSON.stringify(page)})`);
    for (let i = 0; i < 120; i++) {
      if (await win.webContents.executeJavaScript('window.__idle === true')) break;
      await wait(500);
    }
    await wait(400);
    const image = await win.webContents.capturePage();
    fs.writeFileSync(path.join(out, `${page}.png`), image.toPNG());
    if (page === 'upgrades') await captureUpgradeDetails(out, wait);
    if (page === 'writs') await captureScrolled(out, wait, '#crates', 'writs-crates');
    if (page === 'goldperhour') {
      // Open the first finished session, then shoot settings and history.
      await win.webContents.executeJavaScript('document.querySelector("[data-gph-open]")?.click()');
      await wait(600);
      await captureScrolled(out, wait, '#gph-settings', 'goldperhour-history');
    }
  }
  app.quit();
}

// Second shot of a long page, scrolled to `selector`; scrolls back to the top afterwards.
async function captureScrolled(out, wait, selector, name) {
  const found = await win.webContents.executeJavaScript(
    `(() => { const el = document.querySelector(${JSON.stringify(selector)}); if (el) el.scrollIntoView(); return !!el; })()`);
  if (!found) return;
  await wait(300);
  const image = await win.webContents.capturePage();
  fs.writeFileSync(path.join(out, `${name}.png`), image.toPNG());
  await win.webContents.executeJavaScript('document.querySelector(".content").scrollTop = 0');
}

async function captureUpgradeDetails(out, wait) {
  // The upgrades page is longer than the window: a second shot of the cards per slot below the shopping list.
  if (await win.webContents.executeJavaScript('!!document.querySelector(".slot-card")?.scrollIntoView()  || !!document.querySelector(".slot-card")')) {
    await wait(300);
    const image = await win.webContents.capturePage();
    fs.writeFileSync(path.join(out, 'upgrades-slots.png'), image.toPNG());
    await win.webContents.executeJavaScript('document.querySelector(".content").scrollTop = 0');
  }
  // One tooltip with comparison: the second line of the shopping list against the equipped item.
  if (await win.webContents.executeJavaScript('window.__showTip(1)')) {
    await wait(300);
    const image = await win.webContents.capturePage();
    fs.writeFileSync(path.join(out, 'tooltip.png'), image.toPNG());
    await win.webContents.executeJavaScript('document.querySelector("#wtip").classList.remove("show")');
  }
}

app.whenReady().then(() => {
  loadSettings();
  serveIcons();
  createWindow();
  startWatcher();
});
app.on('window-all-closed', () => app.quit());
