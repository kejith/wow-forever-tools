// WoW Forever Tools, the desktop app: main process.
// Talks to the game only through files: it reads the SavedVariables the WoWMCP and GoldPerHour addons and
// Auctionator write on /reload or logout, and it writes AppRequest.lua into the installed addons, which the
// client loads on the next /reload. All calculations run in the bridge: the Go binary wowtools
// (`wowtools bridge <command>`, sim/ in the repo), shipped in the installer next to app.asar.
// The bridge also writes AppData.lua into the WoWMCP addon: the lists the addon shows in the game.
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

// The bridge and its data (scripts/data): in the installed app next to app.asar (extraResources), otherwise in
// the checkout.
const ROOT = app.isPackaged ? process.resourcesPath : path.resolve(__dirname, '..');
// Under WSL and Linux the Windows install is reached through /mnt/c.
const DEFAULT_WOW = process.platform === 'win32'
  ? 'C:\\Program Files (x86)\\World of Warcraft\\_forever_' : '/mnt/c/Program Files (x86)/World of Warcraft/_forever_';

// The data folder keeps the name it had before the app was renamed, so settings and downloaded addons stay.
app.setPath('userData', path.join(app.getPath('appData'), 'wowmcp-app'));
// Downloaded game data and icons (the bridge command icons). The installed app cannot write next to its
// resources, and "cache" is taken: Chromium keeps its own Cache folder in userData and Windows ignores case.
const cacheDir = () => (app.isPackaged ? path.join(app.getPath('userData'), 'game-data') : path.join(ROOT, 'scripts', 'cache'));
const argValue = (name) => (process.argv.find((a) => a.startsWith(`--${name}=`)) || '').split('=')[1] || '';

// Item icons reach the page as wowicon://<FileDataID>.png, served from the cache the bridge command icons fills.
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
  capture: !!argValue('capture'), captureChar: argValue('char'),
});

function saveSettings() {
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true });
  fs.writeFileSync(settingsFile(), JSON.stringify(settings, null, 2));
}

// ---------- Bridge ----------
// wowtools answers one command with one JSON object: `wowtools bridge <command> --wow … --lang …`. Found once
// per run: settings.wowtoolsPath, WOWTOOLS_BIN, the installed app's resources/bin, a local build in sim/.

function wowtoolsCandidates() {
  const exe = process.platform === 'win32' ? 'wowtools.exe' : 'wowtools';
  const list = [];
  if (settings.wowtoolsPath) list.push(settings.wowtoolsPath);
  if (process.env.WOWTOOLS_BIN) list.push(process.env.WOWTOOLS_BIN);
  if (app.isPackaged) list.push(path.join(process.resourcesPath, 'bin', exe));
  list.push(path.join(ROOT, 'sim', exe)); // a local build: cd sim && go build -o wowtools ./cmd/wowtools
  return list.filter((p) => fs.existsSync(p));
}

function execJson(file, args, timeout) {
  return new Promise((resolve, reject) => {
    execFile(file, args, {
      cwd: ROOT, timeout, maxBuffer: 64 * 1024 * 1024, windowsHide: true,
      env: { ...process.env, WOW_TOOLS_CACHE: cacheDir() },
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

// The first candidate that answers `bridge --list`, found once per run of the app.
let bridgeBin;
function findBridge() {
  bridgeBin ||= (async () => {
    for (const bin of wowtoolsCandidates()) {
      try {
        if (Array.isArray(await execJson(bin, ['bridge', '--list'], 10000))) return bin;
      } catch { /* not runnable: try the next one */ }
    }
    return null;
  })();
  return bridgeBin;
}

let workingWowtools;
async function runBridge(command, opts = {}) {
  const args = ['bridge', command, '--wow', settings.wowDir, '--lang', language];
  if (opts.char) args.push('--char', opts.char);
  if (opts.top) args.push('--top', String(opts.top));
  if (opts.maxPrice) args.push('--max-price', String(opts.maxPrice));
  if (opts.preview !== undefined) args.push('--preview', String(opts.preview));
  if (opts.ownedAtAhPrice) args.push('--owned-at-ah-price');
  if (['leveling', 'dungeon'].includes(opts.profile)) args.push('--profile', opts.profile);
  if (['gain', 'price'].includes(opts.sort)) args.push('--sort', opts.sort);
  if (['on', 'off'].includes(opts.engine)) args.push('--engine', opts.engine); // auto is the bridge's default
  if (Number(opts.fightSeconds) > 0) args.push('--fight-seconds', String(Number(opts.fightSeconds)));
  if ([0, 1, 2, 3].includes(opts.targetDelta)) args.push('--target-delta', String(opts.targetDelta));
  // Without it the engine decides how many kills the fight has.
  if (typeof opts.killsPerMinute === 'number' && opts.killsPerMinute >= 0) args.push('--kills-per-minute', String(opts.killsPerMinute));
  if (opts.consumables) args.push('--consumables');
  if (['dps', 'hps', 'ttoom'].includes(opts.metric)) args.push('--metric', opts.metric);
  if (opts.method === 'montecarlo') args.push('--method', 'montecarlo');
  if (typeof opts.profession === 'string' && opts.profession) args.push('--profession', opts.profession);
  if (Array.isArray(opts.ids)) args.push('--ids', opts.ids.map(Number).filter((n) => n > 0).join(','));
  // Screenshots only look: nothing is written into the game folder in capture mode.
  if (command === 'ingame' && opts.write === true && !argValue('capture')) args.push('--write');
  const bin = await findBridge();
  if (!bin) return { error: t('The bridge wowtools was not found. Reinstall the app, or build it: cd sim, go build -o wowtools ./cmd/wowtools') };
  try {
    const result = await execJson(bin, args, 120000);
    workingWowtools = bin;
    return result;
  } catch (err) {
    return { error: err.message };
  }
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

// ---------- Lists in the game ----------
// The WoWMCP addon shows the app's results in the game: a window with the lists, Auctionator shopping lists,
// tooltip lines and vendor hints (the bridge command ingame). The bridge computes them for every character and writes
// AppData.lua into the installed addon, which the client loads on the next /reload. That happens at the
// start, whenever the game has saved (the watcher) and after an addon install, unless settings.sendToGame is
// false; the file is only rewritten when its content changed. One run at a time.

let gameDataQueue = Promise.resolve();
let gameDataRun = null;
const sendsToGame = () => settings.sendToGame !== false;

function syncGameData(write = sendsToGame()) {
  const run = async () => {
    const result = { ...(await runBridge('ingame', { write })), automatic: sendsToGame() };
    win?.webContents.send('game-data', result);
    return result;
  };
  gameDataQueue = gameDataQueue.then(run, run);
  gameDataRun = gameDataQueue;
  return gameDataRun;
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
      debounce = setTimeout(() => {
        syncGameData(); // new scan or snapshot: new lists for the game
        win?.webContents.send('data-changed', { file });
      }, 1500);
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
  try {
    const result = await addon.installAddon(settings.wowDir, addonKey(key), addonOpts());
    if (addonKey(key) === 'wowmcp') syncGameData(); // a new addon may be the first one that can show lists
    return result;
  } catch (err) {
    return { error: err.message };
  }
});
// The last sync, or the one that is running; the first call starts one.
ipcMain.handle('gamedata:get', () => gameDataRun || syncGameData());
ipcMain.handle('gamedata:send', () => syncGameData(true));
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
  const { activeLanguage, clientLocale, version, capture, ...rest } = patch || {};
  const before = `${settings.wowDir}|${sendsToGame()}`;
  Object.assign(settings, rest);
  saveSettings();
  applyLanguage();
  startWatcher();
  if (before !== `${settings.wowDir}|${sendsToGame()}`) syncGameData();
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
// Next to the screenshots goes report.json with the error each page showed, if any. With --require=<pages>
// (comma separated) the app exits with code 1 when one of these pages shows an error or never finishes;
// CI uses that as a smoke test of the built app.
async function runCapture(dir) {
  const required = argValue('require').split(',').filter(Boolean);
  let failed = [];
  try {
    failed = await capturePages(path.resolve(app.isPackaged ? process.cwd() : __dirname, dir), required);
  } catch (err) {
    console.error(`capture failed: ${err.stack || err}`);
    app.exit(1);
    return;
  }
  for (const line of failed) console.error(`capture: ${line}`);
  app.exit(failed.length ? 1 : 0);
}

async function capturePages(out, required) {
  fs.mkdirSync(out, { recursive: true });
  const pages = ['connection', 'character', 'writs', 'upgrades', 'rotations', 'recipes', 'consumables', 'pricehistory', 'goldperhour'];
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const report = { language, version: app.getVersion(), packaged: app.isPackaged, wowtools: null, pages: {} };
  // The Connection page shows the lists for the game. They arrive after the page; wait for them like for
  // any other page's data (the window hears the finished sync through "game-data").
  await (gameDataRun || syncGameData());
  await wait(300);
  for (const page of pages) {
    await win.webContents.executeJavaScript(`window.__showPage(${JSON.stringify(page)})`);
    let idle = false;
    for (let i = 0; i < 600 && !idle; i++) {
      idle = await win.webContents.executeJavaScript('window.__idle === true');
      if (!idle) await wait(500);
    }
    await wait(400);
    // "error": the page as a whole failed. "notices": error boxes inside a page that otherwise rendered,
    // e.g. no Auctionator scan yet.
    const { error, notices } = await win.webContents.executeJavaScript(`({
      error: (document.querySelector('#content > .notice.error') || {}).textContent || null,
      notices: [...document.querySelectorAll('#content .notice.error')].map((el) => el.textContent),
    })`);
    report.pages[page] = { idle, error, notices: notices.filter((text) => text !== error) };
    const image = await win.webContents.capturePage();
    fs.writeFileSync(path.join(out, `${page}.png`), image.toPNG());
    if (page === 'connection') await captureScrolled(out, wait, '#lists', 'connection-lists');
    if (page === 'character') {
      // A class that heals has the heal table below the character window.
      await captureScrolled(out, wait, '#c-heal', 'character-healing');
      await captureScrolled(out, wait, '#c-heal .heal-grid', 'character-healing-ranks');
      // A class that deals damage with spells has the damage analysis: the two rotations, the spell table,
      // the other fights with the stat weights.
      await captureScrolled(out, wait, '#c-dmg', 'character-damage');
      await captureScrolled(out, wait, '#c-dmg .dmg-spells', 'character-damage-spells');
      await captureScrolled(out, wait, '#c-dmg .dmg-grid', 'character-damage-fights');
      // The consumables for a fight: the best set, then the cards per kind.
      await captureScrolled(out, wait, '#c-cons', 'character-consumables');
      await captureScrolled(out, wait, '#c-cons .cons-grid', 'character-consumables-kinds');
    }
    if (page === 'upgrades') await captureUpgradeDetails(out, wait);
    if (page === 'rotations') await captureRotationDetails(out, wait);
    if (page === 'writs') await captureScrolled(out, wait, '#crates', 'writs-crates');
    if (page === 'recipes') await captureHelp(out, wait);
    if (page === 'pricehistory') await captureHelp(out, wait, '.ph-chart .hit:last-of-type', 'pricehistory-day');
    if (page === 'consumables') {
      await captureScrolled(out, wait, '.cons-fight .cons-grid', 'consumables-kinds');
      await captureScrolled(out, wait, '#c-buffs', 'consumables-buffs');
    }
    if (page === 'goldperhour') {
      // Open the first finished session, then shoot settings and history.
      await win.webContents.executeJavaScript('document.querySelector("[data-gph-open]")?.click()');
      await wait(600);
      await captureScrolled(out, wait, '#gph-settings', 'goldperhour-history');
    }
  }
  report.wowtools = workingWowtools || null; // the Go bridge that answered the commands it has
  fs.writeFileSync(path.join(out, 'report.json'), `${JSON.stringify(report, null, 2)}\n`);
  const failed = [];
  for (const page of required) {
    const result = report.pages[page];
    if (!result) failed.push(`unknown page ${page}`);
    else if (!result.idle) failed.push(`${page} did not finish`);
    else if (result.error) failed.push(`${page}: ${result.error}`);
  }
  return failed;
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

// One explanation behind a "?" (by default the first one of a table header on the page), or the popover of
// any other element with data-help.
async function captureHelp(out, wait, selector = '#content th .help', name = 'help') {
  if (!await win.webContents.executeJavaScript(`window.__showHelp(${JSON.stringify(selector)})`)) return;
  await wait(300);
  const image = await win.webContents.capturePage();
  fs.writeFileSync(path.join(out, `${name}.png`), image.toPNG());
  await win.webContents.executeJavaScript('document.body.click(); document.querySelector(".content").scrollTop = 0');
}

// The rotations page: the played fight of the first card that has one, opened, the fights against several
// enemies, and the second guide of a class with two (a priest heals and deals damage).
async function captureRotationDetails(out, wait) {
  if (await win.webContents.executeJavaScript('(() => { const el = document.querySelector("details.rot-timeline"); if (!el) return false; el.open = true; el.scrollIntoView(); return true; })()')) {
    await wait(300);
    const image = await win.webContents.capturePage();
    fs.writeFileSync(path.join(out, 'rotations-timeline.png'), image.toPNG());
    await win.webContents.executeJavaScript('document.querySelector("details.rot-timeline").open = false; document.querySelector(".content").scrollTop = 0');
  }
  await captureScrolled(out, wait, '.rot-card[data-key="pack"]', 'rotations-enemies');
  await captureScrolled(out, wait, '.rot-guide + .rot-guide', 'rotations-second');
}

async function captureUpgradeDetails(out, wait) {
  // What the combat model does with the spells and talents: the list is closed by default.
  if (await win.webContents.executeJavaScript('(() => { const el = document.querySelector("details.coverage"); if (!el) return false; el.open = true; el.scrollIntoView(); return true; })()')) {
    await wait(300);
    const image = await win.webContents.capturePage();
    fs.writeFileSync(path.join(out, 'upgrades-coverage.png'), image.toPNG());
    await win.webContents.executeJavaScript('document.querySelector("details.coverage").open = false; document.querySelector(".content").scrollTop = 0');
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
    await win.webContents.executeJavaScript('document.querySelector("#wtip").classList.remove("show")');
  }
}

app.whenReady().then(() => {
  loadSettings();
  serveIcons();
  createWindow();
  startWatcher();
  syncGameData();
});
app.on('window-all-closed', () => app.quit());
