// WoWMCP desktop app: main process.
// Talks to the game only through files: it reads the SavedVariables the WoWMCP addon and Auctionator write
// on /reload or logout, and it writes AppRequest.lua into the installed addon, which the client loads on the
// next /reload. All calculations run in the Python scripts via scripts/app_bridge.py.

const { app, BrowserWindow, ipcMain, shell } = require('electron');
const { execFile } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const addon = require('./addon');

const REPO = path.resolve(__dirname, '..');
const BRIDGE = path.join(REPO, 'scripts', 'app_bridge.py');
const DEFAULT_WOW = 'C:\\Program Files (x86)\\World of Warcraft\\_forever_';

let win;
let settings = {};
const settingsFile = () => path.join(app.getPath('userData'), 'settings.json');

function loadSettings() {
  try {
    settings = JSON.parse(fs.readFileSync(settingsFile(), 'utf8'));
  } catch {
    settings = {};
  }
  settings.wowDir ||= process.env.WOW_FOREVER_DIR || DEFAULT_WOW;
}

function saveSettings() {
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true });
  fs.writeFileSync(settingsFile(), JSON.stringify(settings, null, 2));
}

// ---------- Python bridge ----------

function uvCandidates() {
  const list = [];
  if (settings.uvPath) list.push(settings.uvPath);
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
      cwd: REPO, timeout: 180000, maxBuffer: 64 * 1024 * 1024, windowsHide: true,
      env: { ...process.env, PYTHONIOENCODING: 'utf-8' },
    }, (err, stdout, stderr) => {
      if (err && err.code === 'ENOENT') return reject(err);
      const text = stdout.trim();
      if (!text) return reject(new Error((stderr || String(err || 'keine Ausgabe')).trim().split('\n').slice(-3).join('\n')));
      try {
        resolve(JSON.parse(text.split('\n').pop()));
      } catch {
        reject(new Error(`Ungültige Antwort der Bridge: ${text.slice(0, 200)}`));
      }
    });
  });
}

let workingUv;
async function runBridge(command, opts = {}) {
  const args = ['run', '--quiet', BRIDGE, command, '--wow', settings.wowDir];
  if (opts.char) args.push('--char', opts.char);
  if (opts.top) args.push('--top', String(opts.top));
  if (opts.maxPreis) args.push('--max-preis', String(opts.maxPreis));
  if (opts.vorschau !== undefined) args.push('--vorschau', String(opts.vorschau));
  if (opts.besitzZumAhPreis) args.push('--besitz-zum-ah-preis');
  if (['leveln', 'dungeon'].includes(opts.profil)) args.push('--profil', opts.profil);
  if (['gewinn', 'preis'].includes(opts.sort)) args.push('--sort', opts.sort);
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
    return { error: 'uv wurde nicht gefunden. Installieren: winget install astral-sh.uv' };
  }
  return { error: lastErr ? lastErr.message : 'Bridge fehlgeschlagen' };
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
      if (!file || !/(WoWMCP|Auctionator)\.lua$/i.test(file)) return;
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
ipcMain.handle('addon:info', () => addon.addonInfo(settings.wowDir));
ipcMain.handle('addon:install', () => {
  try { return addon.installAddon(settings.wowDir); } catch (err) { return { error: err.message }; }
});
ipcMain.handle('request:create', (_e, action, opts) => {
  try { return addon.writeRequest(settings.wowDir, action, opts); } catch (err) { return { error: err.message }; }
});
ipcMain.handle('wow:running', () => wowRunning());
ipcMain.handle('open:path', (_e, p) => shell.openPath(p));
ipcMain.handle('settings:get', () => ({ ...settings, repo: REPO }));
ipcMain.handle('settings:set', (_e, patch) => {
  Object.assign(settings, patch || {});
  saveSettings();
  startWatcher();
  return settings;
});

// ---------- Window ----------

function createWindow() {
  win = new BrowserWindow({
    width: 1280, height: 860, minWidth: 980, minHeight: 640, backgroundColor: '#14110f',
    title: 'WoWMCP', autoHideMenuBar: true,
    webPreferences: { preload: path.join(__dirname, 'preload.js'), contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  win.loadFile(path.join(__dirname, 'renderer', 'index.html'));
  const capture = process.argv.find((a) => a.startsWith('--capture='));
  if (capture) win.webContents.once('did-finish-load', () => runCapture(capture.split('=')[1]));
}

// "npm run capture": renders every page once and saves screenshots, for checking the UI without clicking.
async function runCapture(dir) {
  const out = path.resolve(__dirname, dir);
  fs.mkdirSync(out, { recursive: true });
  const pages = ['connection', 'character', 'writs', 'upgrades'];
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
  }
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
  }
  app.quit();
}

app.whenReady().then(() => {
  loadSettings();
  createWindow();
  startWatcher();
});
app.on('window-all-closed', () => app.quit());
