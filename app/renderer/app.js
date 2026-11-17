// WoWMCP desktop app: renderer. Plain DOM, no framework; all data comes from window.wow (preload.js).
const api = window.wow;

const PAGES = {
  connection: { title: 'Verbindung', render: renderConnection },
  character: { title: 'Charakter', render: renderCharacter },
  writs: { title: 'Handwerksverfügungen', render: renderWrits },
  upgrades: { title: 'AH-Upgrades', render: renderUpgrades },
  recipes: { title: 'Berufe und Rezepte', render: renderRecipes },
  goldperhour: { title: 'Gold pro Stunde', render: renderGoldPerHour },
};

const state = {
  page: 'connection',
  char: null,
  status: null,
  addon: null,
  gphAddon: null,
  game: null,
  cache: new Map(),
  lastLoad: null,
  writs: { top: 3, besitzZumAhPreis: false },
  upgrades: { maxPreis: '', vorschau: 4, top: 3, profil: '', sort: 'gewinn' },
  recipes: { skillLine: null, minListed: 3, onlyProfit: true, onlyLearnable: false },
  gph: { open: null, notice: null },
};

const $ = (sel) => document.querySelector(sel);
const content = $('#content');

// ---------- Formatting ----------

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function money(copper) {
  if (copper === null || copper === undefined) return '<span class="muted">–</span>';
  const g = Math.floor(copper / 10000);
  const s = Math.floor((copper % 10000) / 100);
  const c = copper % 100;
  const parts = [];
  if (g) parts.push(`${g}<span class="g">g</span>`);
  if (g || s) parts.push(`${g ? String(s).padStart(2, '0') : s}<span class="s">s</span>`);
  if (!g) parts.push(`${s ? String(c).padStart(2, '0') : c}<span class="c">c</span>`);
  return `<span class="money">${parts.join(' ')}</span>`;
}

function ago(ts) {
  if (!ts) return 'nie';
  const sec = Math.max(0, Date.now() / 1000 - ts);
  if (sec < 90) return 'gerade eben';
  if (sec < 3600) return `vor ${Math.round(sec / 60)} min`;
  if (sec < 86400 * 2) return `vor ${Math.round(sec / 3600)} h`;
  return `vor ${Math.round(sec / 86400)} Tagen`;
}

function when(ts) {
  if (!ts) return '–';
  return new Date(ts * 1000).toLocaleString('de-DE', { dateStyle: 'short', timeStyle: 'short' });
}

const quality = (q) => `q${q ?? 1}`;
const loading = (text) => `<div class="loading"><div class="spinner"></div>${esc(text)}</div>`;
const errorBox = (msg) => `<div class="notice error">${esc(msg)}</div>`;

// ---------- Item tooltips ----------
// Elements carry data-tip="<n>"; the tooltip data lives in a registry that is reset on every page render.

const QUALITY_HEX = ['9d9d9d', 'ffffff', '1eff00', '3d8fe8', 'b569f0', 'ff8000'];
let tips = [];
const tipBox = $('#wtip');

function tipAttr(tip, compare) {
  if (!tip || !tip.name) return '';
  tips.push({ tip, compare });
  return ` data-tip="${tips.length - 1}"`;
}

// Colours for exports from addon 1.1 (text only); 1.2 sends the game's own colour per line.
function guessColor(line) {
  if (/^(Anlegen|Benutzen|Trefferchance):/.test(line)) return '1eff00';
  if (/^".*"$/.test(line)) return 'ffd100';
  if (/Vorlage/.test(line)) return '8080ff';
  return 'ffffff';
}

function tipHtml(tip, head) {
  const lines = (tip.lines || []).map((line, i) => {
    const color = tip.colors?.[i] || guessColor(line);
    const [left, right] = line.split(' | ');
    return `<div class="wtip-line" style="color:#${esc(color)}"><span>${esc(left)}</span>${right ? `<span>${esc(right)}</span>` : ''}</div>`;
  }).join('');
  return `<div class="wtip-box">
    ${head ? `<div class="wtip-head">${esc(head)}</div>` : ''}
    <div class="wtip-name" style="color:#${QUALITY_HEX[tip.quality ?? 1] || 'ffffff'}">${esc(tip.name)}</div>
    ${tip.ilvl ? `<div class="wtip-ilvl">Gegenstandsstufe ${esc(tip.ilvl)}</div>` : ''}
    ${lines}
    ${tip.rebuilt ? '<div class="wtip-foot">Aus den Werten nachgebaut. Nach dem nächsten /reload kommt der Original-Tooltip.</div>' : ''}
  </div>`;
}

function placeTip(x, y) {
  const pad = 14;
  const { width, height } = tipBox.getBoundingClientRect();
  let left = x + pad;
  let top = y + pad;
  if (left + width > window.innerWidth - 8) left = Math.max(8, x - width - pad);
  if (top + height > window.innerHeight - 8) top = Math.max(8, window.innerHeight - height - 8);
  tipBox.style.left = `${left}px`;
  tipBox.style.top = `${top}px`;
}

document.addEventListener('mouseover', (e) => {
  const el = e.target.closest('[data-tip]');
  if (!el) return;
  const entry = tips[Number(el.dataset.tip)];
  if (!entry) return;
  tipBox.innerHTML = tipHtml(entry.tip) + (entry.compare ? tipHtml(entry.compare, 'Derzeit angelegt') : '');
  tipBox.classList.add('show');
  placeTip(e.clientX, e.clientY);
});
document.addEventListener('mousemove', (e) => {
  if (tipBox.classList.contains('show')) placeTip(e.clientX, e.clientY);
});
document.addEventListener('mouseout', (e) => {
  const el = e.target.closest('[data-tip]');
  if (el && !el.contains(e.relatedTarget)) tipBox.classList.remove('show');
});
window.__showTip = (index) => {
  // Used by "npm run capture" to screenshot a tooltip.
  const el = document.querySelectorAll('[data-tip]')[index];
  if (!el) return false;
  const r = el.getBoundingClientRect();
  el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, clientX: r.left + 10, clientY: r.top + 8 }));
  return true;
};

// ---------- Item icons ----------
// itemIcon() renders a placeholder with data-icon="<itemId>"; hydrateIcons() asks the main process for the
// FileDataIDs (the first time it downloads the PNGs via the bridge) and sets them as background images.

const iconOf = new Map(); // item ID -> FileDataID or null (no icon known)
let iconFallback = null; // question mark
const iconUrl = (fdid) => `url(wowicon://${fdid}.png)`;

function itemIcon(id, q, size = '') {
  if (!id) return `<span class="icon empty ${size}"></span>`;
  const known = iconOf.has(Number(id)) ? (iconOf.get(Number(id)) ?? iconFallback) : null;
  return `<span class="icon ${quality(q)} ${size}" data-icon="${Number(id)}"${known ? ` style="background-image:${iconUrl(known)}"` : ''}></span>`;
}

let hydrating = Promise.resolve();
function hydrateIcons() {
  const run = async () => {
    const els = [...content.querySelectorAll('[data-icon]')];
    const ids = [...new Set(els.map((el) => Number(el.dataset.icon)))].filter((id) => !iconOf.has(id));
    if (ids.length) {
      const r = await api.icons(ids);
      if (r && !r.error) {
        for (const id of ids) iconOf.set(id, r.icons?.[id] ?? null);
        if (r.fallback) iconFallback = r.fallback;
      }
    }
    for (const el of content.querySelectorAll('[data-icon]')) {
      const fdid = iconOf.has(Number(el.dataset.icon)) ? (iconOf.get(Number(el.dataset.icon)) ?? iconFallback) : null;
      if (fdid && !el.style.backgroundImage) el.style.backgroundImage = iconUrl(fdid);
    }
  };
  hydrating = hydrating.then(run, run);
  return hydrating;
}

// Pages that re-render themselves (options on the writs and upgrades pages) get their icons the same way.
let hydrateTimer;
new MutationObserver(() => {
  clearTimeout(hydrateTimer);
  hydrateTimer = setTimeout(hydrateIcons, 30);
}).observe(content, { childList: true, subtree: true });

// ---------- Data ----------

async function cached(key, fn) {
  if (!state.cache.has(key)) state.cache.set(key, fn());
  const result = await state.cache.get(key);
  if (result && result.error) state.cache.delete(key); // retry next time
  return result;
}

async function loadStatus() {
  const [status, addon, gphAddon, game] = await Promise.all([
    api.bridge('status'), api.addonInfo('wowmcp'), api.addonInfo('goldperhour'), api.wowRunning(),
  ]);
  state.status = status;
  state.addon = addon;
  state.gphAddon = gphAddon;
  state.game = game;
  state.lastLoad = Date.now();
  fillCharacterSelect();
  updateLiveState();
}

function fillCharacterSelect() {
  const select = $('#char-select');
  const chars = (state.status?.characters || []).filter((c) => !c.error);
  if (!state.char || !chars.some((c) => c.key === state.char)) state.char = chars[0]?.key || null;
  select.innerHTML = chars.map((c) => `<option value="${esc(c.key)}" ${c.key === state.char ? 'selected' : ''}>`
    + `${esc(c.name || c.key)} · ${esc(c.level ?? '?')} ${esc(c.class || '')}</option>`).join('');
  select.disabled = chars.length === 0;
}

function connectionHealth() {
  const s = state.status;
  const a = state.addon;
  if (!s || !a) return 'bad';
  if (!s.wowFound || !a.installed) return 'bad';
  if (!a.upToDate || !s.characters?.length) return 'warn';
  return 'ok';
}

function updateLiveState() {
  const health = connectionHealth();
  $('#nav-dot').className = `dot ${health}`;
  const time = state.lastLoad ? new Date(state.lastLoad).toLocaleTimeString('de-DE', { timeStyle: 'short' }) : '–';
  const game = state.game?.running ? 'WoW Forever läuft' : 'WoW Forever geschlossen';
  $('#live-state').innerHTML = `${esc(game)}<br>Live-Abgleich aktiv · ${esc(time)}`;
}

// ---------- Navigation ----------

async function showPage(page) {
  window.__idle = false;
  state.page = page;
  tips = [];
  tipBox.classList.remove('show');
  document.querySelectorAll('#nav button').forEach((b) => b.classList.toggle('active', b.dataset.page === page));
  $('#page-title').textContent = PAGES[page].title;
  try {
    await PAGES[page].render();
    await hydrateIcons();
  } catch (err) {
    content.innerHTML = errorBox(err.message || String(err));
  }
  window.__idle = true;
}
window.__showPage = showPage;

$('#nav').addEventListener('click', (e) => {
  const button = e.target.closest('button[data-page]');
  if (button) showPage(button.dataset.page);
});
$('#char-select').addEventListener('change', (e) => {
  state.char = e.target.value;
  showPage(state.page);
});
$('#refresh').addEventListener('click', async () => {
  state.cache.clear();
  content.innerHTML = loading('Lade Daten neu …');
  await loadStatus();
  showPage(state.page);
});

api.onDataChanged(async () => {
  // WoW wrote SavedVariables (/reload or logout): reload everything that depends on them.
  state.cache.clear();
  state.gph.notice = null;
  await loadStatus();
  showPage(state.page);
});

// ---------- Page: connection ----------

// Where an install takes its files from: the latest GitHub release, or a local checkout from the settings.
function addonSource(a) {
  const s = a.source;
  if (!s) return `<span class="badge bad">nicht verfügbar</span> ${esc(a.sourceError || '')}`;
  if (s.kind === 'local') return `${esc(s.version || '?')} <span class="badge">lokaler Ordner</span> <span class="muted">${esc(s.dir)}</span>`;
  const stale = s.offline
    ? ` <span class="badge warn" title="${esc(s.error || '')}">nicht geprüft, Stand ${when(s.checkedAt / 1000)}</span>` : '';
  return `${esc(s.version || '?')} <span class="muted">Release ${esc(s.tag)} von github.com/${esc(s.repo)}</span>${stale}`;
}

// One card per addon the app installs; `key` is the addon key in addon.js (wowmcp, goldperhour).
function addonCard(a, about = '') {
  const key = esc(a.key);
  const check = a.source?.kind === 'local' ? ''
    : `<button class="ghost" data-action="check-addon" data-addon="${key}">Nach Updates suchen</button>`;
  const hint = !a.lastInstall ? '' : a.lastInstall.firstInstall
    ? '<div class="notice small" style="margin:10px 0 0">Installiert. Im Spiel <code>/reload</code> eingeben. Taucht es danach nicht in der Addon-Liste auf, WoW einmal neu starten.</div>'
    : '<div class="notice small" style="margin:10px 0 0">Aktualisiert. Im Spiel <code>/reload</code> eingeben.</div>';
  if (!a.installed) {
    return `<div class="card"><h2>${esc(a.name)}-Addon</h2>
      <h3><span class="badge bad">nicht installiert</span></h3>
      ${about ? `<p class="small">${about}</p>` : ''}
      <dl class="kv small"><dt>Quelle</dt><dd>${addonSource(a)}</dd><dt>Ziel</dt><dd>${esc(a.installedDir)}</dd></dl>
      <div class="row" style="margin-top:10px">
        <button class="primary" data-action="install" data-addon="${key}" ${a.source ? '' : 'disabled'}>Addon installieren</button>
        ${check}
      </div></div>`;
  }
  const badge = !a.source ? '<span class="badge">installiert</span>'
    : a.ahead ? '<span class="badge ok">neuer als die Quelle</span>'
    : a.upToDate ? '<span class="badge ok">aktuell</span>' : '<span class="badge warn">Update verfügbar</span>';
  return `<div class="card"><h2>${esc(a.name)}-Addon</h2>
    <h3>${badge}</h3>
    <dl class="kv small">
      <dt>Installiert</dt><dd>${esc(a.installedVersion || '?')}</dd>
      <dt>Quelle</dt><dd>${addonSource(a)}</dd>
      ${a.changed.length && !a.upToDate ? `<dt>Geändert</dt><dd>${esc(a.changed.join(', '))}</dd>` : ''}
      <dt>Auctionator</dt><dd>${a.auctionatorInstalled ? 'installiert' : '<span class="badge bad">fehlt</span>'}</dd>
    </dl>${hint}
    <div class="row" style="margin-top:10px">
      ${a.upToDate ? '' : `<button class="primary" data-action="install" data-addon="${key}">Addon aktualisieren</button>`}
      ${check}
      <button class="ghost" data-action="open-addon" data-addon="${key}">Ordner öffnen</button>
    </div></div>`;
}

const GPH_ABOUT = 'Gold pro Stunde, getrennt nach Kräuterkunde und Loot. Die App zeigt Sitzungen, Verlauf und Einstellungen.';

function gameCard() {
  const s = state.status;
  const g = state.game || {};
  const running = g.running ? '<span class="badge ok">läuft</span>'
    : g.otherClient ? '<span class="badge warn">anderer WoW-Client offen</span>' : '<span class="badge">geschlossen</span>';
  return `<div class="card"><h2>WoW Forever</h2>
    <h3>${running}</h3>
    <dl class="kv small">
      <dt>Ordner</dt><dd>${s.wowFound ? esc(s.wowDir) : `<span class="badge bad">nicht gefunden</span> ${esc(s.wowDir)}`}</dd>
      <dt>Client</dt><dd>${esc(s.characters?.[0]?.client?.version || '?')} (${esc(s.characters?.[0]?.client?.build || '?')})</dd>
    </dl></div>`;
}

function auctionatorCard() {
  const au = state.status.auctionator;
  if (!au || au.error) return `<div class="card"><h2>Auktionshaus-Scan</h2>${errorBox(au?.error || 'Keine Auctionator-Daten')}</div>`;
  const days = au.scanDate ? Math.round((Date.now() - new Date(au.scanDate)) / 86400000) : null;
  const badge = days === null ? '' : days <= 1 ? '<span class="badge ok">frisch</span>' : `<span class="badge warn">${days} Tage alt</span>`;
  return `<div class="card"><h2>Auktionshaus-Scan</h2>
    <div class="big">${esc(au.scanDate || '–')} ${badge}</div>
    <dl class="kv small">
      <dt>Items</dt><dd>${au.items.toLocaleString('de-DE')}</dd>
      <dt>Gespeichert</dt><dd>${when(au.modified)}</dd>
    </dl>
    <p class="muted small">Preise sind der Mindest-Sofortkauf beim letzten Scan, keine Live-Angebote.</p></div>`;
}

function itemExportCard() {
  const ex = state.status.itemExport;
  const body = ex ? `<div class="big">${ex.count.toLocaleString('de-DE')} Items</div>
      <dl class="kv small">
        <dt>Stand</dt><dd>${when(ex.updated)} (${ago(ex.updated)})</dd>
        ${ex.missing ? `<dt>Nicht geladen</dt><dd>${ex.missing}</dd>` : ''}
        ${ex.lastRun ? `<dt>Letzter Lauf</dt><dd>${ex.lastRun.new} neu von ${ex.lastRun.total}</dd>` : ''}
        <dt>Automatisch</dt><dd>${ex.legacy ? 'alter 1.0-Export, wird beim nächsten /reload übernommen' : ex.auto === false ? 'aus' : 'nach dem Schließen des AH'}</dd>
      </dl>`
    : '<div class="big muted">noch keiner</div>';
  return `<div class="card"><h2>Item-Export</h2>${body}</div>`;
}

function requestCard() {
  const a = state.addon;
  const s = state.status;
  const pending = a.pendingRequest;
  const done = s.lastRequest;
  let stateHtml = '<p class="muted small">Kein offener Auftrag.</p>';
  if (pending && (!done || done.id !== pending.id)) {
    stateHtml = `<div class="notice"><b>Auftrag #${pending.id} wartet</b> (${pending.action === 'sync' ? 'Snapshot' : 'Item-Export'})
      <ol class="steps"><li>Im Spiel <code>/reload</code> eingeben, das Addon startet den Auftrag.</li>
      <li>Auf die Chat-Meldung von WoWMCP warten.</li><li>Nochmal <code>/reload</code>, dann aktualisiert sich die App von selbst.</li></ol></div>`;
  } else if (done) {
    const err = done.error ? ` <span class="badge bad">${esc(done.error)}</span>` : '';
    stateHtml = `<p class="small"><span class="badge ok">erledigt</span> Auftrag #${esc(done.id)} (${esc(done.action)}) ${when(done.done)}${err}
      ${done.new !== undefined ? ` · ${done.new} neue Items` : ''}</p>`;
  }
  const disabled = a.installed ? '' : 'disabled';
  return `<div class="card" style="grid-column: 1 / -1"><h2>Aufträge an das Addon</h2>
    <p class="small muted">Die App legt einen Auftrag im Addon-Ordner ab, das Spiel liest ihn beim nächsten <code>/reload</code>.
      Ergebnisse kommen über die SavedVariables zurück.</p>
    ${stateHtml}
    <div class="row">
      <button class="primary" data-action="request-items" ${disabled}>Neue AH-Items exportieren</button>
      <button class="ghost" data-action="request-items-full" ${disabled}>Alle Items neu lesen</button>
      <button class="ghost" data-action="request-sync" ${disabled}>Charakter-Snapshot</button>
    </div></div>`;
}

function charactersTable() {
  const chars = state.status.characters || [];
  if (!chars.length) {
    return `<div class="notice">Noch kein Charakter exportiert. Einloggen und <code>/wowmcp sync</code> eingeben.</div>`;
  }
  const rows = chars.map((c) => {
    if (c.error) return `<tr><td>${esc(c.key)}</td><td colspan="6">${errorBox(c.error)}</td></tr>`;
    const errors = Object.entries(c.errors || {});
    const old = c.addonVersion && state.addon.sourceVersion && c.addonVersion !== state.addon.sourceVersion;
    return `<tr>
      <td><b>${esc(c.name)}</b> <span class="muted">${esc(c.surname || '')}</span></td>
      <td>${esc(c.level)} ${esc(c.race || '')} ${esc(c.class || '')}</td>
      <td class="num">${money(c.money)}</td>
      <td>${esc(c.zone || '')}</td>
      <td>${ago(c.updated)}</td>
      <td>${c.addonVersion ? `<span class="badge ${old ? 'warn' : 'ok'}">${esc(c.addonVersion)}</span>` : '<span class="badge">1.0</span>'}</td>
      <td>${errors.length ? errors.map(([k, v]) => `<span class="badge bad" title="${esc(v)}">${esc(k)}</span>`).join(' ') : '<span class="muted">–</span>'}</td>
    </tr>`;
  }).join('');
  return `<div class="card"><table>
    <thead><tr><th>Charakter</th><th>Stufe/Klasse</th><th class="num">Gold</th><th>Zone</th><th>Snapshot</th><th>Addon</th><th>Fehler</th></tr></thead>
    <tbody>${rows}</tbody></table></div>`;
}

async function renderConnection() {
  if (!state.status) {
    content.innerHTML = loading('Verbinde mit WoW Forever …');
    await loadStatus();
  }
  if (state.status.error) {
    content.innerHTML = errorBox(state.status.error);
    return;
  }
  const a = state.addon;
  let hint = '';
  const outdated = [a, state.gphAddon].filter((x) => x?.installed && !x.upToDate && !x.lastInstall);
  if (outdated.length) {
    hint = `<div class="notice">Für ${esc(outdated.map((x) => x.name).join(' und '))} gibt es eine andere Version als die installierte. „Addon aktualisieren“ kopiert sie ins Spiel, danach im Spiel <code>/reload</code>.</div>`;
  }
  content.innerHTML = `${hint}
    <div class="grid">${gameCard()}${addonCard(a)}${addonCard(state.gphAddon, GPH_ABOUT)}${auctionatorCard()}${itemExportCard()}${requestCard()}</div>
    <div class="section"><h2>Charaktere</h2>${charactersTable()}</div>`;
}

content.addEventListener('click', async (e) => {
  const button = e.target.closest('button[data-action]');
  if (!button) return;
  const action = button.dataset.action;
  const gph = button.dataset.addon === 'goldperhour';
  if (action.startsWith('gph-')) return gphAction(action, button);
  button.disabled = true;
  if (action === 'install') {
    const result = await api.installAddon(button.dataset.addon || 'wowmcp');
    if (result.error) { alert(result.error); button.disabled = false; return; }
    if (gph) state.gphAddon = { ...result, lastInstall: result };
    else state.addon = { ...result, lastInstall: result };
  } else if (action === 'check-addon') {
    // Asks GitHub now; otherwise the app keeps the last answer for a quarter of an hour.
    const result = await api.addonInfo(button.dataset.addon || 'wowmcp', { force: true });
    if (gph) state.gphAddon = result;
    else state.addon = result;
  } else if (action === 'open-addon') {
    api.openPath((gph ? state.gphAddon : state.addon).installedDir);
    button.disabled = false;
  } else if (action.startsWith('request-')) {
    const result = await api.createRequest(action === 'request-sync' ? 'sync' : 'items', { full: action === 'request-items-full' });
    if (result.error) { alert(result.error); button.disabled = false; return; }
    state.addon = await api.addonInfo('wowmcp');
  }
  if (state.page === 'connection') renderConnection();
  if (state.page === 'goldperhour') renderGoldPerHour();
});

// ---------- Page: character ----------

async function renderCharacter() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = '<div class="notice">Kein Charakter exportiert.</div>'; return; }
  content.innerHTML = loading('Lade Charakter …');
  const d = await cached(`character:${state.char}`, () => api.bridge('character', { char: state.char }));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  const c = d.character;
  const st = c.stats || {};
  const xpPct = c.xpMax ? Math.round((c.xp / c.xpMax) * 100) : 0;
  const num = (v, digits = 0) => (v === undefined || v === null ? '–'
    : Number(v).toLocaleString('de-DE', { minimumFractionDigits: digits, maximumFractionDigits: digits }));
  const pct = (v) => (v === undefined || v === null ? '–' : `${num(v, 2)} %`);

  // Paper doll in the layout of the in-game character window: left and right columns, weapons below.
  const bySlot = Object.fromEntries(d.equipment.map((e) => [e.slot, e]));
  const slot = ([id, label], side) => {
    const e = bySlot[id];
    if (!e) {
      return `<div class="pd-slot ${side} empty">${itemIcon(null, null, 'lg')}
        <div class="pd-text"><div class="pd-name">${esc(label)}</div><div class="pd-meta">leer</div></div></div>`;
    }
    const dur = e.durability;
    const meta = [
      `${num(e.score, 1)} P.`,
      e.ilvl ? `iLvl ${esc(e.ilvl)}` : '',
      dur ? `<span class="${dur[0] / dur[1] < 0.3 ? 'dur-low' : ''}">${dur[0]}/${dur[1]}</span>` : '',
    ].filter(Boolean).join(' · ');
    return `<div class="pd-slot ${side}"${tipAttr(e.tip)}>${itemIcon(e.id, e.quality, 'lg')}
      <div class="pd-text"><div class="pd-name ${quality(e.quality)}">${esc(e.name)}</div>
        <div class="pd-meta"><span class="pd-label">${esc(label)}</span> · ${meta}</div></div></div>`;
  };
  const column = (slots, side) => slots.map((s) => slot(s, side)).join('');

  const statBox = (title, rows) => `<div class="pd-stats"><h4>${esc(title)}</h4>
    ${rows.map(([k, v]) => `<div class="pd-stat"><span>${esc(k)}</span><b>${v}</b></div>`).join('')}</div>`;
  const res = st.resistances || {};
  const resist = [['arcane', 'Arkan'], ['fire', 'Feuer'], ['nature', 'Natur'], ['frost', 'Frost'], ['shadow', 'Schatten']]
    .map(([k, label]) => `<span class="res res-${k}" title="${label}widerstand">${num(res[k] ?? 0)}<small>${label}</small></span>`).join('');
  const durs = d.equipment.filter((e) => e.durability).map((e) => e.durability[0] / e.durability[1]);
  const ilvls = d.equipment.map((e) => e.ilvl).filter(Boolean);
  const center = `<div class="pd-center">
      <div class="pd-res">${resist}</div>
      <div class="pd-grid">
        ${statBox('Attribute', [['Stärke', num(st.strength)], ['Beweglichkeit', num(st.agility)], ['Ausdauer', num(st.stamina)],
          ['Intelligenz', num(st.intellect)], ['Willenskraft', num(st.spirit)]])}
        ${statBox('Zauber', [['Schattenschaden', num(st.spellDamage?.shadow)], ['Heilung', num(st.healing)],
          ['Zauberkrit', pct(st.spellCrit?.shadow)], ['Zaubertreffer', st.spellHit !== undefined ? `${num(st.spellHit)} %` : '–']])}
        ${statBox('Verteidigung', [['Leben', num(st.healthMax)], [st.powerType === 'MANA' || !st.powerType ? 'Mana' : 'Energie', num(st.powerMax)],
          ['Rüstung', num(st.armor)], ['Ausweichen', pct(st.dodge)]])}
        ${statBox('Ausrüstung', [['Ausrüstungswert', num(d.setValue, 1)], ['Ø iLvl', ilvls.length ? num(ilvls.reduce((a, b) => a + b, 0) / ilvls.length, 1) : '–'],
          ['Haltbarkeit min.', durs.length ? `<span class="${Math.min(...durs) < 0.3 ? 'dur-low' : ''}">${Math.round(Math.min(...durs) * 100)} %</span>` : '–'],
          ['Skala', esc(d.profileLabel)]])}
      </div></div>`;

  const profs = d.professions.map((p) => `<div style="margin-bottom:8px">
      <div class="row small"><span>${esc(p.name)}</span><span class="spacer"></span><span class="muted">${p.rank}/${p.max}</span></div>
      <div class="bar"><span style="width:${Math.round((p.rank / p.max) * 100)}%"></span></div></div>`).join('');
  const talents = d.talents.map((t) => Object.entries(t.tabs || {}).map(([tab, pts]) => `${esc(tab)} ${pts}`).join(' / ')).join('<br>');
  content.innerHTML = `
    <div class="paperdoll">
      <div class="pd-head">
        <div><div class="pd-title">${esc(c.name)} <span>${esc(c.surname || '')}</span></div>
          <div class="pd-sub">Stufe ${esc(c.level)} ${esc(c.race)} ${esc(c.class)}</div></div>
        <span class="spacer"></span>
        <div class="pd-money">${money(c.money)}<div class="small muted">${esc(c.zone || '')} · Snapshot ${ago(d.updated)}</div></div>
      </div>
      <div class="pd-body">
        <div class="pd-col">${column(PAPERDOLL.left, 'left')}</div>
        ${center}
        <div class="pd-col">${column(PAPERDOLL.right, 'right')}</div>
        <div class="pd-weapons">${column(PAPERDOLL.bottom, 'bottom')}</div>
      </div>
      <div class="pd-xp"><div class="bar"><span style="width:${xpPct}%"></span></div>
        <div class="pd-xp-text">Erfahrung ${(c.xp || 0).toLocaleString('de-DE')} / ${(c.xpMax || 0).toLocaleString('de-DE')} (${xpPct} %)${c.rested ? ` · erholt ${c.rested.toLocaleString('de-DE')}` : ''}</div></div>
    </div>
    <p class="small muted">P. = Punkte: was der Ausrüstung ohne das Item fehlt, nach der Skala „${esc(d.profileLabel)}“ der AH-Upgrades
      (${esc(d.weights)}). Zusammen ${d.setValue.toLocaleString('de-DE')}. Maus über ein Item zeigt den Tooltip.</p>
    <div class="grid section"><div class="card"><h2>Berufe</h2>${profs || '<span class="muted">–</span>'}</div>
      <div class="card"><h2>Talente</h2>${talents || '<span class="muted">–</span>'}</div></div>`;
}

// Slot IDs and order of the in-game character window (Classic layout).
const PAPERDOLL = {
  left: [[1, 'Kopf'], [2, 'Hals'], [3, 'Schultern'], [15, 'Rücken'], [5, 'Brust'], [4, 'Hemd'], [19, 'Wappenrock'], [9, 'Handgelenke']],
  right: [[10, 'Hände'], [6, 'Taille'], [7, 'Beine'], [8, 'Füße'], [11, 'Finger'], [12, 'Finger'], [13, 'Schmuck'], [14, 'Schmuck']],
  bottom: [[16, 'Waffenhand'], [17, 'Schildhand'], [18, 'Distanz']],
};

// ---------- Page: writs ----------

async function renderWrits() {
  if (!state.status) await loadStatus();
  const o = state.writs;
  content.innerHTML = `<div class="controls">
      <label>Vorschläge pro Stufe<input type="number" id="w-top" min="1" max="10" value="${o.top}"></label>
      <label class="check"><input type="checkbox" id="w-own" ${o.besitzZumAhPreis ? 'checked' : ''}> eigene Items zum AH-Preis rechnen</label>
    </div><div id="w-body">${loading('Berechne Verfügungen …')}</div>`;
  $('#w-top').addEventListener('change', (e) => { o.top = Number(e.target.value) || 3; renderWrits(); });
  $('#w-own').addEventListener('change', (e) => { o.besitzZumAhPreis = e.target.checked; renderWrits(); });
  const key = `writs:${state.char}:${o.top}:${o.besitzZumAhPreis}`;
  const d = await cached(key, () => api.bridge('writs', { char: state.char, top: o.top, besitzZumAhPreis: o.besitzZumAhPreis }));
  const body = $('#w-body');
  if (!body) return;
  if (d.error) { body.innerHTML = errorBox(d.error); return; }
  const rep = d.reputation;
  const repHtml = rep ? `<div class="card" style="margin-bottom:16px"><div class="row">
      <div><h3>${esc(rep.name)}</h3><div class="muted">${esc(rep.standing)} · ${rep.value} / ${rep.max} · noch ${rep.max - rep.value} bis zur nächsten Stufe</div></div>
      <span class="spacer"></span><div class="muted small">AH-Scan ${esc(d.scanDate)}</div></div>
      <div class="bar" style="margin-top:10px"><span style="width:${Math.round((rep.value / rep.max) * 100)}%"></span></div></div>` : '';
  const tiers = d.tiers.map((t) => {
    const rows = t.best.map((b, i) => `<tr class="${i === 0 ? 'best' : ''}">
        <td><span class="item">${itemIcon(b.writId, 1)}<span>${i === 0 ? '★ ' : ''}${esc(b.name)}${b.qty > 1 ? ` <span class="muted">(${b.qty}x)</span>` : ''}</span></span></td>
        <td class="muted">${esc(b.prof)}</td>
        <td class="num">${money(b.wcost)}</td><td class="num">${money(b.icost)}</td>
        <td class="num">${money(b.cost)}</td>
        <td class="num" title="${esc(b.payoutSource)}"><span class="good">−${b.payoutMeasured ? '' : '≈'}${money(b.payout)}</span></td>
        <td class="num"><b>${netMoney(b.net)}</b></td>
        <td class="num">${perUnit(b.per)}</td>
        <td class="num muted">${b.writs}</td>
        <td class="small">${esc(b.note)}</td></tr>`).join('');
    const sealed = t.sealed ? `<p class="small notice">Du hast ${t.sealed}x „Versiegelte Verfügung“ dieser Stufe. Erst öffnen, vielleicht ist sie billiger.</p>` : '';
    return `<div class="card section"><div class="row"><h3>${esc(t.name)}</h3></div>
      ${rows ? `<table><thead><tr><th>Verfügung</th><th>Beruf</th><th class="num">Verfügung</th><th class="num">Item</th>
        <th class="num">Summe</th><th class="num">Münzbeutel</th><th class="num">Netto</th><th class="num">pro Ruf</th>
        <th class="num">im AH</th><th>Hinweis</th></tr></thead><tbody>${rows}</tbody></table>`
        : '<p class="muted">Keine Verfügung mit Preisdaten.</p>'}${sealed}</div>`;
  }).join('');
  const sources = [...new Set(d.tiers.flatMap((t) => t.best.map((b) => b.payoutSource)))];
  const estimated = d.tiers.some((t) => t.best.some((b) => !b.payoutMeasured));
  body.innerHTML = `${repHtml}
    <div class="card"><div class="row"><div><h2>Pro Tag</h2><div class="big">${netMoney(d.dailyNet)}</div>
        <div class="muted small">netto nach Münzbeuteln · ${money(d.dailyTotal)} Einkauf</div></div>
      <span class="spacer"></span><div class="muted">für ${d.dailyRep} Ruf (je Stufe die beste Verfügung)</div></div></div>
    ${tiers}
    <p class="small muted">Items im Besitz zählen als 0. Die Quest selbst zahlt kein Gold, sie gibt einen Münzbeutel; Netto = Einkauf − Inhalt des Beutels,
      sortiert nach Netto pro Ruf. Münzbeutel: ${sources.map(esc).join(' · ')}.${estimated ? ' Werte mit ≈ sind geschätzt; ab Addon 1.5 misst das Addon den Inhalt jedes geöffneten Beutels.' : ''}
      Verfügungsliste: wow.gg (Nov 2026).</p>
    ${cratesHtml(d)}`;
}

// Net cost: negative means the coin pouch pays more than the writ and item cost.
function netMoney(copper) {
  if (copper === null || copper === undefined) return money(copper);
  const c = Math.round(copper);
  return c < 0 ? `<span class="good">${money(-c)} Gewinn</span>` : money(c);
}

// Copper per unit (reputation or favor) as silver with two decimals.
function perUnit(copper) {
  if (copper === null || copper === undefined) return '<span class="muted">–</span>';
  return `${(copper / 100).toLocaleString('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} s`;
}

function cratesHtml(d) {
  const c = d.crates;
  if (!c) return '';
  const bundle = (o) => o ? `<span class="item">${itemIcon(o.id, 1, 'sm')}<span>${o.qty}x ${esc(o.name)}</span></span>` : '<span class="muted">–</span>';
  const alternatives = (r) => r.options.filter((o) => o !== r.best && r.best && o.id !== r.best.id).map((o) =>
    `${o.qty}x ${esc(o.name)} ${o.cost === null ? '<span class="muted">kein Preis</span>' : money(o.cost)}${o.enough ? '' : ' <span class="warn-text">(zu wenig im AH)</span>'}`).join(' · ');
  const best = c.best.map((r, i) => `<tr class="${i === 0 ? 'best' : ''}">
      <td><span class="item">${itemIcon(r.crateId, 1)}<span>${i === 0 ? '★ ' : ''}${esc(r.kind)} <span class="muted">(${esc(r.tierName)})</span></span></span></td>
      <td>${bundle(r.best)}</td><td class="num"><b>${money(Math.round(r.cost))}</b></td>
      <td class="num">${r.favor.toLocaleString('de-DE')}</td><td class="num">${perUnit(r.per)}</td></tr>`).join('');
  const tiers = c.tiers.map((t) => {
    const rows = t.rows.map((r, i) => `<tr class="${i === 0 && r.complete ? 'best' : ''}">
        <td><span class="item">${itemIcon(r.crateId, 1)}<span>${i === 0 && r.complete ? '★ ' : ''}${esc(r.kind)}</span></span></td>
        <td class="num">${r.crateCost === null ? '<span class="muted">nicht im AH</span>' : money(r.crateCost)}</td>
        <td>${bundle(r.best)}${alternatives(r) ? `<div class="small muted">oder ${alternatives(r)}</div>` : ''}</td>
        <td class="num">${r.best ? money(r.best.cost) : '<span class="muted">–</span>'}</td>
        <td class="num"><b>${r.complete ? money(Math.round(r.cost)) : '<span class="muted">–</span>'}</b></td>
        <td class="num">${perUnit(r.per)}</td><td class="num muted">${perUnit(r.perGreen)}</td>
        <td class="small">${esc(r.note)}</td></tr>`).join('');
    return `<div class="card section"><div class="row"><h3>${esc(t.name)}</h3>
        <span class="badge ${t.favorGuess ? 'warn' : ''}" title="${esc(t.favorSource)}">${t.favor.toLocaleString('de-DE')} Gunst · grün ${t.favorGreen.toLocaleString('de-DE')}${t.favorGuess ? ' (geschätzt)' : ''}</span>
        ${t.tooLow ? `<span class="badge bad">erst ab Stufe ${t.level}</span>` : `<span class="muted small">ab Stufe ${t.level}</span>`}</div>
      <table><thead><tr><th>Kiste</th><th class="num">leere Kiste</th><th>günstigstes Bündel</th><th class="num">Bündel</th>
        <th class="num">Summe</th><th class="num">pro Gunst</th><th class="num">pro Gunst (grün)</th><th>Hinweis</th></tr></thead>
        <tbody>${rows}</tbody></table></div>`;
  }).join('');
  return `<h2 class="section" id="crates">Handelskisten</h2>
    <div class="card"><div class="row"><div><h2>Günstigste Händlergunst</h2>
        <div class="muted small">eine leere Kiste + ein Bündel, versiegelt beim Handelsvertreter abgeben</div></div>
      <span class="spacer"></span>${d.favor !== null && d.favor !== undefined ? `<div><div class="muted small">Deine Händlergunst</div><div class="big">${d.favor.toLocaleString('de-DE')}</div></div>` : ''}</div>
      ${best ? `<table><thead><tr><th>Kiste</th><th>Bündel</th><th class="num">Summe</th><th class="num">Gunst</th><th class="num">pro Gunst</th></tr></thead><tbody>${best}</tbody></table>`
        : '<p class="muted">Keine Kiste mit vollständigen Preisen.</p>'}</div>
    ${tiers}
    <p class="small muted">Kisten zahlen kein Gold und keinen Ruf, nur Händlergunst. Netto = leere Kiste (AH, im Besitz 0) + günstigstes Bündel, das das AH liefern kann
      (eigene Items 0). „Pro Gunst“ rechnet mit der gewöhnlichen (weißen) versiegelten Kiste, die grüne bringt doppelt so viel; wovon die Qualität abhängt, ist nicht bekannt.
      Gunst pro Kiste laut urgametips.com/wow.gg (Nov 2026), Fachmann geschätzt; ab Addon 1.5 misst das Addon jede Abgabe. Bündel-Mengen aus den Spieldaten geprüft.
      Elitekisten sind nicht enthalten (Inhalt unbekannt).</p>`;
}

// ---------- Page: upgrades ----------

async function renderUpgrades() {
  if (!state.status) await loadStatus();
  const o = state.upgrades;
  const option = (value, label, current) => `<option value="${value}"${value === current ? ' selected' : ''}>${label}</option>`;
  content.innerHTML = `<div class="controls">
      <label>Budget (Gold, leer = eigenes Gold)<input type="number" id="u-max" min="0" step="0.5" value="${esc(o.maxPreis)}" placeholder="eigenes Gold"></label>
      <label>Skala<select id="u-profil">${option('', 'automatisch', o.profil)}${option('leveln', 'Leveln', o.profil)}${option('dungeon', 'Dungeon/Raid', o.profil)}</select></label>
      <label>Sortierung<select id="u-sort">${option('gewinn', 'größter Gewinn', o.sort)}${option('preis', 'Punkte pro Gold', o.sort)}</select></label>
      <label>Vorschau (Stufen voraus)<input type="number" id="u-pre" min="0" max="10" value="${o.vorschau}"></label>
      <label>Vorschläge pro Platz<input type="number" id="u-top" min="1" max="10" value="${o.top}"></label>
    </div><div id="u-body">${loading('Vergleiche AH-Items mit deiner Ausrüstung …')}</div>`;
  const bind = (id, field, parse) => $(id).addEventListener('change', (e) => { o[field] = parse(e.target.value); renderUpgrades(); });
  bind('#u-max', 'maxPreis', (v) => v);
  bind('#u-profil', 'profil', (v) => v);
  bind('#u-sort', 'sort', (v) => v);
  bind('#u-pre', 'vorschau', (v) => Number(v) || 0);
  bind('#u-top', 'top', (v) => Number(v) || 3);
  const key = `upgrades:${state.char}:${o.maxPreis}:${o.vorschau}:${o.top}:${o.profil}:${o.sort}`;
  const d = await cached(key, () => api.bridge('upgrades', {
    char: state.char, maxPreis: o.maxPreis ? Number(o.maxPreis) : 0, vorschau: o.vorschau, top: o.top,
    profil: o.profil, sort: o.sort,
  }));
  const body = $('#u-body');
  if (!body) return;
  if (d.error) {
    body.innerHTML = `${errorBox(d.error)}<div class="notice">Im Auktionshaus mit Auctionator scannen und das AH schließen:
      Das Addon liest neue Items dann selbst. Oder auf der Seite „Verbindung“ einen Item-Export anfordern.</div>`;
    return;
  }
  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => c.gain)));
  const signed = (n) => `${n < 0 ? '−' : '+'}${Math.abs(n).toFixed(1)}`;
  const pctOf = (c) => (c.gainPct === null ? 'neu' : `${c.gainPct < 0 ? '−' : '+'}${Math.abs(c.gainPct)} %`);
  const perGold = (c) => c.perGold.toLocaleString('de-DE', { maximumFractionDigits: 1 });
  const itemCell = (c, compare) => `<span class="item">${itemIcon(c.id, c.quality)}<span class="${quality(c.quality)}"${tipAttr(c.tip, compare)}>${esc(c.name)}</span></span>
          ${c.isNew ? '<span class="badge new">NEU in Forever</span>' : ''}
          ${c.soon ? `<span class="badge warn">ab Stufe ${c.reqLevel}</span>` : ''}
          ${c.twoHand ? '<span class="badge">Zweihand, ersetzt auch die Schildhand</span>' : ''}
          <div class="small muted">${esc(c.stats)}</div>
          ${c.effects.map((t) => `<div class="effects">${esc(t)}</div>`).join('')}`;
  const slots = d.slots.map((s) => {
    const cur = s.current;
    const rows = s.candidates.map((c) => `<tr>
        <td><div class="gain"><b>${signed(c.gain)}</b><div class="bar"><span style="width:${Math.round((c.gain / maxGain) * 100)}%"></span></div></div></td>
        <td class="num muted">${pctOf(c)}</td>
        <td class="num">${money(c.price)}</td>
        <td class="num muted">${perGold(c)}</td>
        <td>${itemCell(c, cur?.tip)}</td></tr>`).join('');
    return `<div class="card section slot-card">
      <div><div class="slot-name">${esc(s.slotName)}</div>
        <div class="small muted" style="margin-top:4px">jetzt:</div>
        <div class="small"><span class="item">${itemIcon(cur?.id, cur?.quality)}<span class="${quality(cur?.quality)}"${tipAttr(cur?.tip)}>${esc(cur?.name || '–')}</span></span></div>
        <div class="small muted">${esc(cur?.stats || '')}</div></div>
      <table><thead><tr><th>Gewinn</th><th class="num">zum Item</th><th class="num">Preis</th><th class="num">Punkte/Gold</th><th>Item</th></tr></thead><tbody>${rows}</tbody></table></div>`;
  }).join('');
  const pk = d.package;
  const packageHtml = pk.items.length ? `<div class="section"><h2>Einkaufsliste: bestes Paket für ${money(d.budget)}</h2><div class="card">
      <div class="row" style="gap:36px;align-items:flex-start;margin-bottom:10px">
        <div><h2>Gewinn</h2><div class="big good">${signed(pk.gain)}${pk.gainPct === null ? '' : ` <span class="small muted">(+${pk.gainPct} % Ausrüstungswert)</span>`}</div></div>
        <div><h2>Kosten</h2><div class="big">${money(pk.cost)}</div></div>
        <div><h2>Käufe</h2><div class="big">${pk.items.length}</div></div><span class="spacer"></span></div>
      <table><thead><tr><th>Platz</th><th class="num">Gewinn</th><th class="num">Preis</th><th class="num">Punkte/Gold</th><th>Item</th>
        <th>ersetzt</th><th class="num">bis hier</th></tr></thead><tbody>
      ${pk.items.map((c) => `<tr><td class="muted">${esc(c.slotName)}</td>
        <td class="num"><b class="${c.gain < 0 ? '' : 'good'}">${signed(c.gain)}</b></td>
        <td class="num">${money(c.price)}</td><td class="num muted">${perGold(c)}</td>
        <td>${itemCell(c, c.compareTip)}</td><td class="small muted">${esc(c.replaces.join(' + ') || '–')}</td>
        <td class="num small">${money(c.cumCost)}<div class="muted">${signed(c.cumGain)}</div></td></tr>`).join('')}</tbody></table>
      <p class="small muted">Beste Kombination über alle Plätze, die ins Budget passt (Ringe als Paar, Waffenhand + Schildhand gegen Zweihand,
        ohne Items über deiner Stufe). Sortiert nach Punkten pro Gold: Von oben nach unten kaufen und aufhören, wo es dir zu teuer wird.</p>
      </div></div>` : '';
  const scale = `<details class="scale"><summary>Skala „${esc(d.profileLabel)}“: 1 Punkt = 1 Schatten-Zaubermacht. ${esc(d.weights)}</summary>
      <table><tbody>${d.scale.map((r) => `<tr><td>${esc(r.label)}</td><td class="num">${r.weight.toLocaleString('de-DE')}</td>
        <td class="small muted">${esc(r.note)}</td></tr>`).join('')}</tbody></table></details>`;
  const review = d.review.length ? `<div class="section"><h2>Mit Spezialeffekt – selbst ansehen</h2><div class="card"><table>
      <thead><tr><th>Platz</th><th class="num">Preis</th><th>Item</th><th>Effekt</th></tr></thead><tbody>
      ${d.review.map((r) => `<tr><td class="muted">${esc(r.slot)}</td><td class="num">${money(r.price)}</td>
        <td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}"${tipAttr(r.tip)}>${esc(r.name)}</span></span> ${r.isNew ? '<span class="badge new">NEU</span>' : ''}</td>
        <td class="small">${esc(r.effects.join(' / '))}</td></tr>`).join('')}</tbody></table></div></div>` : '';
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>Budget</h2><div class="big">${money(d.budget)}</div></div>
      <div><h2>Stufe</h2><div class="big">${d.level}</div></div>
      <div><h2>Ausrüstungswert</h2><div class="big">${d.setValue.toLocaleString('de-DE')}</div></div>
      <div><h2>AH-Scan</h2><div class="big">${esc(d.scanDate)}</div></div>
      <div><h2>Item-Export</h2><div class="big">${d.exportCount.toLocaleString('de-DE')}</div><div class="small muted">${ago(d.exportUpdated)}</div></div>
      <span class="spacer"></span></div>
      ${scale}
      <p class="small muted">Gewinn = Änderung des Ausrüstungswerts beim Tausch, „zum Item“ = im Verhältnis zum ersetzten Item.
        Preise = Mindest-Sofortkauf beim Scan.
        Items mit Zufallswerten („des Schattenzorns“) kann Auctionator nicht unterscheiden, sie fehlen hier.</p></div>
    ${packageHtml}
    ${slots ? `<div class="section"><h2>Alle Plätze</h2></div>${slots}` : '<div class="notice section">Keine Upgrades in diesem Budget.</div>'}${review}`;
}

// ---------- Page: recipes ----------

const DIFFICULTY = [['orange', 'orange: sicher ein Skillpunkt'], ['yellow', 'gelb: meistens ein Skillpunkt'],
  ['green', 'grün: selten ein Skillpunkt'], ['grey', 'grau: kein Skillpunkt']];

function signedMoney(copper) {
  if (copper === null || copper === undefined) return '<span class="muted">–</span>';
  return copper < 0 ? `<span class="loss">−${money(-copper)}</span>` : `<span class="good">+${money(copper)}</span>`;
}

function recipeName(r) {
  const diff = DIFFICULTY[r.difficulty];
  const dot = diff ? `<span class="skill-dot ${diff[0]}" title="${esc(diff[1])}${r.yellow ? ` · gelb ab ${r.yellow}, grau ab ${r.grey}` : ''}"></span>` : '';
  const qty = r.qty && r.qty !== 1 ? ` <span class="muted">×${Number(r.qty).toLocaleString('de-DE', { maximumFractionDigits: 1 })}</span>` : '';
  const reagents = r.reagents.map((g) => `<span class="reagent" title="${esc(g.name)}: ${g.unit === null ? 'kein Preis' : `${(g.unit / 100).toLocaleString('de-DE', { maximumFractionDigits: 2 })} s pro Stück (${g.source})`}${g.owned ? ` · ${g.owned} im Besitz` : ''}">`
    + `${itemIcon(g.id, g.quality, 'sm')}${g.count}× ${esc(g.name)}${g.source && g.source !== 'AH' ? ' <span class="muted">(Händler)</span>' : ''}</span>`).join('');
  const badges = [
    r.isNew ? '<span class="badge new">NEU in Forever</span>' : '',
    r.cooldown ? `<span class="badge">Abklingzeit ${esc(r.cooldown)}</span>` : '',
    r.craftable ? `<span class="badge ok" title="aus Taschen und Bank">${r.craftable}× herstellbar</span>` : '',
  ].join(' ');
  return `<div class="recipe">${dot}<span class="item">${itemIcon(r.output, r.quality)}<span class="${quality(r.quality)}">${esc(r.name)}</span></span>${qty} ${badges}</div>
    <div class="reagents">${reagents}</div>
    ${r.notes.length ? `<div class="small warn-text">${esc(r.notes.join(' · '))}</div>` : ''}`;
}

const TREND = { fallend: ['down', '↘ fällt'], steigend: ['up', '↗ steigt'], stabil: ['flat', '→ stabil'] };

function goldText(copper) {
  return `${copper < 0 ? '−' : '+'}${(Math.abs(copper) / 10000).toLocaleString('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} g`;
}

// Gewinn pro Herstellung an den letzten vollen Scan-Tagen als kleine Linie; die gestrichelte Linie ist null.
function profitTrend(r) {
  const h = r.history || [];
  if (h.length < 2) return '<span class="muted" title="Für einen Verlauf braucht es Preise von mindestens zwei vollen Scan-Tagen.">–</span>';
  const W = 116, H = 30, pad = 4;
  const t = h.map((p) => Date.parse(p.date));
  const values = h.map((p) => p.profit);
  const lo = Math.min(0, ...values), hi = Math.max(0, ...values);
  const x = (i) => pad + ((t[i] - t[0]) / (t[h.length - 1] - t[0])) * (W - 2 * pad);
  const y = (v) => pad + ((hi - v) / (hi - lo || 1)) * (H - 2 * pad);
  const points = h.map((p, i) => `${x(i).toFixed(1)},${y(p.profit).toFixed(1)}`).join(' ');
  const [cls, label] = TREND[r.trend] || ['flat', ''];
  const day = (iso) => `${iso.slice(8)}.${iso.slice(5, 7)}.`;
  const tip = h.map((p) => `${day(p.date)}  ${goldText(p.profit)}  ·  Preis ${(p.price / 10000).toLocaleString('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} g  ·  ${p.listed} im AH`).join('\n');
  return `<div class="trend ${cls}" title="${esc(tip)}">
      <svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" role="img" aria-label="Gewinnverlauf">
        <line class="zero" x1="0" x2="${W}" y1="${y(0).toFixed(1)}" y2="${y(0).toFixed(1)}"/>
        <polyline points="${points}"/>
        ${h.map((p, i) => `<circle cx="${x(i).toFixed(1)}" cy="${y(p.profit).toFixed(1)}" r="${i === h.length - 1 ? 2.6 : 1.4}"/>`).join('')}
      </svg>
      <div class="small">${label}<span class="muted"> · ${h.length} Scans seit ${day(h[0].date)}</span></div></div>`;
}

function marketCells(r) {
  return `<td class="num">${money(r.cost)}</td>
    <td class="num">${money(r.sell)}${r.sellSource === 'Händler' ? '<div class="small muted">Händler</div>' : ''}</td>
    <td class="num"><b>${signedMoney(r.profit)}</b>${r.margin !== null ? `<div class="small muted">${r.margin > 0 ? '+' : ''}${r.margin} %</div>` : ''}</td>
    <td>${profitTrend(r)}</td>
    <td class="num ${r.listed < 3 ? 'warn-text' : 'muted'}">${r.listed}</td>`;
}

function acquireCell(r) {
  const a = r.acquire;
  const parts = a.options.map((o) => {
    if (o.kind === 'ah') return `<div><span class="item">${itemIcon(o.id, o.quality, 'sm')}AH ${money(o.price)}</span> <span class="muted small">(${o.listed} im Angebot${o.age ? `, ${o.age} Tage alt` : ''})</span></div>`;
    if (o.kind === 'vendor') return `<div><span class="item">${itemIcon(o.id, o.quality, 'sm')}Händler ${esc(o.npc || '')} ${money(o.price)}</span>${o.limited ? ' <span class="badge warn">begrenzt</span>' : ''}</div>`;
    if (o.kind === 'trainer') return '<div>Lehrer</div>';
    return `<div><span class="item">${itemIcon(o.id, o.quality, 'sm')}<span class="${quality(o.quality)}">${esc(o.name)}</span></span> <span class="muted small">nicht im AH</span></div>`;
  }).join('');
  const text = a.sourceText ? `<div class="small muted">${esc(a.sourceText.replace(/\|c\w{8}|\|r|\|n/g, ' '))}</div>` : '';
  return parts + text;
}

function learnCell(r, rank) {
  const a = r.acquire;
  if (a.learnable === true) return `<span class="badge ok">jetzt</span>${a.reqSkill ? `<div class="small muted">ab ${a.reqSkill}</div>` : ''}`;
  if (a.reqSkill) return `<span class="badge warn">ab ${a.reqSkill}</span><div class="small muted">noch ${a.reqSkill - rank}</div>`;
  if (a.trainer && r.yellow) return `<span class="badge">spätestens ${r.yellow}</span>`;
  return '<span class="muted">?</span>';
}

async function renderRecipes() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = '<div class="notice">Kein Charakter exportiert.</div>'; return; }
  const o = state.recipes;
  content.innerHTML = loading('Rechne Rezepte gegen den AH-Scan …');
  const d = await cached(`recipes:${state.char}`, () => api.bridge('recipes', { char: state.char }));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  if (!d.professions.length) {
    content.innerHTML = '<div class="notice">Dieser Charakter hat keinen Herstellungsberuf (Sammelberufe haben keine Rezepte).</div>';
    return;
  }
  if (!d.professions.some((p) => p.skillLine === o.skillLine)) o.skillLine = d.professions[0].skillLine;
  const p = d.professions.find((x) => x.skillLine === o.skillLine);
  const option = (value, label, current) => `<option value="${value}"${String(value) === String(current) ? ' selected' : ''}>${label}</option>`;

  const keep = (r) => (!o.onlyProfit || (r.profit ?? -1) > 0) && r.listed >= o.minListed;
  const learned = p.learned.filter(keep);
  const missing = p.missing.filter((r) => keep(r) && (!o.onlyLearnable || r.acquire.learnable === true));
  const scanned = !!p.scanned;

  const header = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(p.name)}</h2><div class="big">${p.rank} <span class="muted small">/ ${p.max}</span></div>
        <div class="bar" style="width:160px;margin-top:6px"><span style="width:${Math.round((p.rank / p.max) * 100)}%"></span></div></div>
      <div><h2>Gelernt</h2><div class="big">${scanned ? p.learnedCount : '?'}</div><div class="small muted">${scanned ? `${p.profitableCount} mit Gewinn` : 'Berufsfenster noch nicht gelesen'}</div></div>
      <div><h2>Bestes Rezept</h2><div class="big">${scanned ? signedMoney(p.bestLearned) : '–'}</div><div class="small muted">pro Herstellung, ab 3 im AH</div></div>
      <div><h2>Lohnender zu holen</h2><div class="big">${scanned ? p.missing.filter((r) => r.better).length : '–'}</div><div class="small muted">mehr Gewinn als dein bestes</div></div>
      <div><h2>AH-Scan</h2><div class="big">${esc(d.scanDate || '–')}</div><div class="small muted">${scanned ? `Rezepte gelesen ${ago(p.scanned)}` : ''}</div></div>
      <span class="spacer"></span></div></div>`;

  const notice = scanned ? '' : `<div class="notice"><b>Welche Rezepte du kennst, weiß die App noch nicht.</b>
      ${d.addonVersion && d.addonVersion < '1.4' ? `Das Addon im Spiel ist ${esc(d.addonVersion)}. Auf der Seite „Verbindung“ zuerst „Addon aktualisieren“ und im Spiel <code>/reload</code>.` : ''}
      <ol class="steps"><li>Im Spiel das ${esc(p.name)}-Fenster einmal öffnen (Zauberbuch oder Taste K).</li>
      <li><code>/reload</code> eingeben. Die App trennt dann gelernte und fehlende Rezepte.</li></ol>
      Bis dahin stehen unten alle Rezepte des Berufs.</div>`;

  const learnedTable = scanned ? `<div class="section"><h2>Deine Rezepte (${learned.length} von ${p.learnedCount})</h2><div class="card">
      ${learned.length ? `<table><thead><tr><th>Rezept und Reagenzien</th><th class="num">Reagenzien</th><th class="num">Verkauf (−5 %)</th><th class="num">Gewinn</th><th>Verlauf</th><th class="num">im AH</th></tr></thead>
      <tbody>${learned.map((r) => `<tr><td>${recipeName(r)}</td>${marketCells(r)}</tr>`).join('')}</tbody></table>`
        : '<p class="muted">Kein gelerntes Rezept passt zum Filter.</p>'}</div></div>` : '';

  const missingTitle = scanned ? 'Noch nicht gelernt' : 'Alle Rezepte';
  const missingTable = `<div class="section"><h2>${missingTitle} (${missing.length} von ${p.missingCount})</h2><div class="card">
      ${missing.length ? `<table><thead><tr><th>Rezept und Reagenzien</th><th class="num">Reagenzien</th><th class="num">Verkauf (−5 %)</th><th class="num">Gewinn</th><th>Verlauf</th><th class="num">im AH</th>
        <th>Lernen</th><th>Woher</th><th class="num">bezahlt nach</th></tr></thead>
      <tbody>${missing.map((r) => `<tr class="${r.better ? 'best' : ''}"><td>${r.better ? '<span class="badge new">lohnt mehr</span> ' : ''}${recipeName(r)}</td>${marketCells(r)}
        <td>${learnCell(r, p.rank)}</td><td class="small">${acquireCell(r)}</td>
        <td class="num small">${r.payback ? `${Math.ceil(r.payback)}× herstellen` : '<span class="muted">–</span>'}</td></tr>`).join('')}</tbody></table>`
        : '<p class="muted">Kein Rezept passt zum Filter.</p>'}</div></div>`;

  content.innerHTML = `<div class="controls">
      <label>Beruf<select id="r-prof">${d.professions.map((x) => option(x.skillLine, `${esc(x.name)} ${x.rank}/${x.max}`, o.skillLine)).join('')}</select></label>
      <label>Mindestens im AH<input type="number" id="r-listed" min="0" max="1000" value="${o.minListed}"></label>
      <label class="check"><input type="checkbox" id="r-profit" ${o.onlyProfit ? 'checked' : ''}> nur mit Gewinn</label>
      <label class="check"><input type="checkbox" id="r-learnable" ${o.onlyLearnable ? 'checked' : ''}> nur jetzt lernbar</label>
    </div>
    ${notice}${header}${learnedTable}${missingTable}
    <p class="small muted">Gewinn pro Herstellung = Mindest-Sofortkauf des Ergebnisses beim Scan × Menge, minus 5 % AH-Gebühr, minus Reagenzien
      zum günstigeren Preis aus AH oder Händler (eigene Reagenzien zählen zum Marktpreis). „im AH“ = Angebote des Ergebnisses beim letzten Scan:
      Bei wenigen Angeboten ist der Preis unsicher. „Verlauf“ = derselbe Gewinn an den letzten vollen Scan-Tagen (Preis des Tages: Mitte aus
      höchstem und niedrigstem Mindestpreis, die gestrichelte Linie ist null). „fällt“ oder „steigt“ heißt: Der Gewinn hat sich über die letzten
      drei Scan-Tage um mehr als 20 % verändert. Details zeigt der Tooltip. Der Punkt vor dem Namen ist die Skill-Farbe. „bezahlt nach“ = Rezeptpreis geteilt durch den Gewinn.
      Händlerpreise liest das Addon, wenn du mit einem Händler sprichst (bisher ${d.vendorPrices}).</p>`;
  $('#r-prof').addEventListener('change', (e) => { o.skillLine = Number(e.target.value); renderRecipes(); });
  $('#r-listed').addEventListener('change', (e) => { o.minListed = Math.max(0, Number(e.target.value) || 0); renderRecipes(); });
  $('#r-profit').addEventListener('change', (e) => { o.onlyProfit = e.target.checked; renderRecipes(); });
  $('#r-learnable').addEventListener('change', (e) => { o.onlyLearnable = e.target.checked; renderRecipes(); });
}

// ---------- Page: GoldPerHour ----------
// GoldPerHourDB is account-wide, so this page ignores the character selection. WoW writes it on /reload or
// logout; the app changes the addon only through AppRequest.lua (start/stop/reset and settings).

const GPH_CATS = [['herb', 'Kräuter'], ['loot', 'Loot']];

function duration(sec) {
  const s = Math.max(0, Math.round(sec || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h} h ${String(m).padStart(2, '0')} min` : `${m} min`;
}

// Gold per hour rounded like the addon: copper is noise once gold is involved.
function rate(copper) {
  if (copper === null || copper === undefined) return '<span class="muted" title="Erst ab einer Minute Sitzungszeit">–</span>';
  const c = Math.round(copper);
  return money(c >= 10000 ? c - (c % 100) : c);
}

// Cumulative gold of one session over session time: herbs, loot and the sum.
function gphChart(samples, { width = 640, height = 170, small = false } = {}) {
  if (!samples || samples.length < 2) {
    return '<p class="muted small">Für eine Kurve braucht es mindestens zwei Messpunkte (alle 5 Minuten Sitzungszeit, dazu bei Start und Stop).</p>';
  }
  const pad = small ? 3 : 2;
  const T = samples[samples.length - 1].t || 1;
  const hi = Math.max(1, ...samples.map((s) => s.herb + s.loot));
  const x = (t) => pad + (t / T) * (width - 2 * pad);
  const y = (v) => pad + ((hi - v) / hi) * (height - 2 * pad);
  const line = (f) => samples.map((s) => `${x(s.t).toFixed(1)},${y(f(s)).toFixed(1)}`).join(' ');
  const g = (c) => `${(c / 10000).toLocaleString('de-DE', { maximumFractionDigits: c >= 100000 ? 0 : 1 })} g`;
  // Labels stay HTML: the SVG stretches to the card width and would distort text.
  const axis = small ? '' : `<line class="axis" x1="0" x2="${width}" y1="${y(0)}" y2="${y(0)}"/>
    <line class="grid" x1="0" x2="${width}" y1="${y(hi)}" y2="${y(hi)}"/>`;
  const tip = samples.map((s) => `${duration(s.t)}: ${g(s.herb + s.loot)} (Kräuter ${g(s.herb)}, Loot ${g(s.loot)})`).join('\n');
  const svg = `<svg class="gph-chart${small ? ' small' : ''}" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="Gold über die Sitzungszeit">
      <title>${esc(tip)}</title>${axis}
      <polyline class="herb" points="${line((s) => s.herb)}"/>
      <polyline class="loot" points="${line((s) => s.loot)}"/>
      <polyline class="total" points="${line((s) => s.herb + s.loot)}"/>
    </svg>`;
  if (small) return svg;
  return `<div class="chart-axis small muted"><span>${g(hi)}</span></div>${svg}
    <div class="chart-axis small muted"><span>0</span><span>${duration(T)}</span></div>
    <div class="legend small"><span class="key total"></span>Gesamt <span class="key herb"></span>Kräuter <span class="key loot"></span>Loot</div>`;
}

function gphItemsTable(cat, label, limit = 0) {
  const items = limit ? cat.items.slice(0, limit) : cat.items;
  if (!items.length && !cat.coin) return `<p class="muted small">${esc(label)}: noch nichts.</p>`;
  const share = (v) => (cat.gold ? `${Math.round((v / cat.gold) * 100)} %` : '');
  const rows = items.map((e) => `<tr>
      <td><span class="item">${itemIcon(e.id, e.quality, 'sm')}<span class="${quality(e.quality)}">${esc(e.name)}</span></span></td>
      <td class="num">${e.count.toLocaleString('de-DE')}</td><td class="num">${money(e.value)}</td><td class="num muted">${share(e.value)}</td></tr>`).join('');
  const more = cat.items.length > items.length ? `<tr><td colspan="4" class="muted small">+${cat.items.length - items.length} weitere</td></tr>` : '';
  const coin = cat.coin ? `<tr><td class="muted">Münzen</td><td></td><td class="num">${money(cat.coin)}</td><td class="num muted">${share(cat.coin)}</td></tr>` : '';
  return `<table class="compact"><thead><tr><th>${esc(label)}</th><th class="num">Anzahl</th><th class="num">Wert</th><th class="num">Anteil</th></tr></thead>
    <tbody>${rows}${more}${coin}</tbody></table>`;
}

function gphRequestState(d) {
  const pending = state.gphAddon?.pendingRequest;
  const done = d.lastRequest;
  const what = (r) => [r.action && { start: 'Start', stop: 'Stop', reset: 'Zurücksetzen' }[r.action], r.settings && 'Einstellungen']
    .filter(Boolean).join(' + ') || 'Auftrag';
  if (pending && (!done || done.id !== pending.id)) {
    return `<div class="notice small" style="margin:10px 0 0"><b>${esc(what(pending))} wartet</b> (Auftrag #${pending.id}).
      Im Spiel <code>/reload</code> eingeben, das Addon führt ihn einmal aus. Die App zeigt das Ergebnis nach dem nächsten Speichern (wieder <code>/reload</code> oder Logout).
      <div class="row" style="margin-top:8px"><button class="ghost" data-action="gph-clear">Auftrag zurückziehen</button></div></div>`;
  }
  if (done) {
    const err = done.error ? ` <span class="badge bad" title="${esc(done.error)}">Fehler</span>` : '';
    return `<p class="small" style="margin:10px 0 0"><span class="badge ok">erledigt</span> ${esc(what(done))} ${when(done.done)}${err}</p>`;
  }
  return '';
}

function gphSettingsCard(d) {
  const s = d.settings;
  const disabled = state.gphAddon?.installed ? '' : 'disabled';
  const extra = s.herbSpells.join(', ');
  return `<div class="card" id="gph-settings"><h2>Einstellungen im Addon</h2>
    <div class="controls" style="margin-bottom:12px">
      <label class="check"><input type="checkbox" id="gph-shown" ${s.shown ? 'checked' : ''}> Fenster anzeigen</label>
      <label class="check"><input type="checkbox" id="gph-resume" ${s.resumeAfterReload ? 'checked' : ''}> läuft nach /reload weiter</label>
      <label class="check"><input type="checkbox" id="gph-auto" ${s.autoStart ? 'checked' : ''}> startet beim ersten Loot</label>
    </div>
    <div class="controls">
      <label>Skalierung<input type="number" id="gph-scale" min="0.5" max="2" step="0.05" value="${esc(s.scale)}"></label>
      <label>Items im Tooltip<input type="number" id="gph-tooltip" min="1" max="30" value="${esc(s.tooltipItems)}"></label>
      <label>Preise<select id="gph-price">
        <option value="auctionator" ${s.priceSource !== 'vendor' ? 'selected' : ''}>Auctionator, sonst Händler</option>
        <option value="vendor" ${s.priceSource === 'vendor' ? 'selected' : ''}>nur Händlerpreis</option></select></label>
      <label>Weitere Kräuter-Zauber (IDs)<input type="text" id="gph-herbs" value="${esc(extra)}" placeholder="z. B. 2368, 3570"></label>
    </div>
    <p class="small muted">Kräutersammeln (${esc(d.herbGathering)}) und Zauber mit demselben Namen zählen immer. Die Preisquelle gilt für neu
      aufgehobene Items, Bisheriges behält seinen Wert.</p>
    <div class="row"><button class="primary" data-action="gph-save" ${disabled}>An das Addon senden</button>
      ${d.legacy && d.found ? '<span class="small muted">Version 0.2 kennt diese Einstellungen noch nicht: zuerst aktualisieren.</span>' : ''}</div></div>`;
}

function gphHistoryTable(d) {
  if (!d.history.length) {
    return '<p class="muted small">Noch keine abgeschlossene Sitzung. Beim Zurücksetzen (im Spiel oder hier) landet die Sitzung im Verlauf, höchstens 100.</p>';
  }
  const best = d.summary.best;
  const rows = d.history.map((h) => {
    const id = h.started || h.archived;
    const open = state.gph.open === id;
    const zones = h.zones.slice(0, 2).map((z) => esc(z.zone)).join(', ') + (h.zones.length > 2 ? ` <span class="muted">+${h.zones.length - 2}</span>` : '');
    const row = `<tr class="clickable${open ? ' open' : ''}" data-gph-open="${esc(id)}">
      <td class="nowrap">${when(h.started || h.archived)}</td><td class="nowrap">${duration(h.elapsed)}</td>
      <td>${esc(h.chars.join(', ')) || '<span class="muted">–</span>'}</td><td>${zones || '<span class="muted">–</span>'}</td>
      <td class="num">${money(h.categories.herb.gold)}</td><td class="num">${money(h.categories.loot.gold)}</td>
      <td class="num"><b>${money(h.total)}</b></td>
      <td class="num">${rate(h.perHour)}${id === best ? ' <span class="badge new">beste</span>' : ''}</td>
      <td>${gphChart(h.samples, { width: 110, height: 26, small: true })}</td></tr>`;
    if (!open) return row;
    return `${row}<tr class="detail"><td colspan="9"><div class="gph-detail">
      <div>${gphItemsTable(h.categories.herb, 'Kräuter', 8)}</div><div>${gphItemsTable(h.categories.loot, 'Loot', 8)}</div>
      <div>${gphChart(h.samples)}</div></div></td></tr>`;
  }).join('');
  return `<table><thead><tr><th>Beginn</th><th>Dauer</th><th>Charakter</th><th>Zonen</th><th class="num">Kräuter</th><th class="num">Loot</th>
    <th class="num">Gesamt</th><th class="num">pro Stunde</th><th>Verlauf</th></tr></thead><tbody>${rows}</tbody></table>`;
}

async function renderGoldPerHour() {
  if (!state.status) await loadStatus();
  const a = state.gphAddon;
  // Opening a history row re-renders from the cache; no spinner then, so the page keeps its scroll position.
  if (!state.cache.has('goldperhour')) content.innerHTML = loading('Lade GoldPerHour …');
  const d = await cached('goldperhour', () => api.bridge('goldperhour'));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  const top = [];
  if (!a.installed || !a.upToDate || a.lastInstall) top.push(`<div class="grid" style="margin-bottom:16px">${addonCard(a, GPH_ABOUT)}</div>`);
  if (state.gph.notice) top.push(`<div class="notice">${state.gph.notice}</div>`);
  if (!d.found) {
    content.innerHTML = `${top.join('')}<div class="notice">Noch keine GoldPerHour-Daten. ${a.installed
      ? 'Im Spiel <code>/reload</code> eingeben, dann <code>/gph</code> öffnet das Fenster. WoW speichert die Werte bei jedem /reload und Logout.'
      : 'Erst das Addon installieren.'}</div>${gphSettingsCard(d)}`;
    return;
  }
  const c = d.current;
  const saved = d.updated || d.modified;
  const runBadge = c.runningAtSave ? '<span class="badge ok">lief beim Speichern</span>'
    : c.elapsed ? '<span class="badge">pausiert</span>' : '<span class="badge">nicht gestartet</span>';
  const disabled = a.installed ? '' : 'disabled';
  const catCard = ([key, label]) => {
    const cat = c.categories[key];
    return `<div class="card"><h2>${label}</h2><div class="big">${money(cat.gold)}</div>
      <dl class="kv small"><dt>pro Stunde</dt><dd>${rate(cat.perHour)}</dd>
        <dt>Items</dt><dd>${cat.itemCount.toLocaleString('de-DE')} (${cat.items.length} Sorten)</dd>
        ${cat.coin ? `<dt>Münzen</dt><dd>${money(cat.coin)}</dd>` : ''}</dl></div>`;
  };
  const sum = d.summary;
  content.innerHTML = `${top.join('')}
    <p class="small muted" style="margin-top:0">Stand beim letzten Speichern ${when(saved)} (${ago(saved)}). WoW schreibt die Werte nur bei <code>/reload</code> oder Logout,
      laufende Sitzungen zählen im Spiel weiter.</p>
    <div class="grid">
      <div class="card"><h2>Aktuelle Sitzung</h2><div class="big">${money(c.total)}</div>
        <h3 style="margin-top:4px">${rate(c.perHour)} <span class="muted small">pro Stunde</span></h3>
        <dl class="kv small"><dt>Dauer</dt><dd>${duration(c.elapsed)} ${runBadge}</dd>
          ${c.started ? `<dt>Begonnen</dt><dd>${when(c.started)}</dd>` : ''}
          ${c.chars.length ? `<dt>Charakter</dt><dd>${esc(c.chars.join(', '))}</dd>` : ''}</dl></div>
      ${GPH_CATS.map(catCard).join('')}
      <div class="card"><h2>Steuerung</h2>
        <div class="row">
          <button class="primary" data-action="gph-start" ${disabled}>Start</button>
          <button class="ghost" data-action="gph-stop" ${disabled}>Stop</button>
          <button class="ghost" data-action="gph-reset" ${disabled}>Zurücksetzen</button>
        </div>
        <p class="small muted" style="margin:8px 0 0">Wirkt beim nächsten <code>/reload</code>. Im Spiel geht es sofort: <code>/gph start</code>, <code>/gph stop</code>, Reset-Knopf im Fenster.</p>
        ${gphRequestState(d)}</div>
    </div>
    <div class="section"><h2>Gold über die Sitzung</h2><div class="card">${gphChart(c.samples)}</div></div>
    <div class="section gph-two">
      <div class="card"><h2>Items der Sitzung</h2>${gphItemsTable(c.categories.herb, 'Kräuter')}<div style="height:12px"></div>${gphItemsTable(c.categories.loot, 'Loot')}</div>
      <div class="card"><h2>Zonen</h2>${c.zones.length ? `<table class="compact"><thead><tr><th>Zone</th><th class="num">Gold</th><th class="num">Anteil</th></tr></thead><tbody>
        ${c.zones.map((z) => `<tr><td>${esc(z.zone)}</td><td class="num">${money(z.value)}</td><td class="num muted">${c.total ? Math.round((z.value / c.total) * 100) : 0} %</td></tr>`).join('')}</tbody></table>`
        : `<p class="muted small">${d.legacy ? 'Zonen zeichnet das Addon ab Version 0.3 auf.' : 'Noch nichts aufgehoben.'}</p>`}</div>
    </div>
    <div class="section">${gphSettingsCard(d)}</div>
    <div class="section"><h2>Verlauf</h2>
      ${sum.sessions ? `<p class="small muted" style="margin-top:0">${sum.sessions} Sitzungen, zusammen ${duration(sum.elapsed)} und ${money(sum.total)}, im Schnitt ${rate(sum.perHour)} pro Stunde.
        Zeile anklicken für Items und Kurve.</p>` : ''}
      <div class="card">${gphHistoryTable(d)}</div></div>`;
}

function gphFormSettings() {
  const v = (id) => $(`#${id}`);
  return {
    shown: v('gph-shown').checked,
    resumeAfterReload: v('gph-resume').checked,
    autoStart: v('gph-auto').checked,
    scale: Number(v('gph-scale').value),
    tooltipItems: Number(v('gph-tooltip').value),
    priceSource: v('gph-price').value,
    herbSpells: v('gph-herbs').value.split(/[\s,;]+/).filter(Boolean).map(Number).filter((n) => Number.isInteger(n) && n > 0),
  };
}

async function gphAction(action, button) {
  if (action === 'gph-reset' && !confirm('GoldPerHour-Sitzung beim nächsten /reload zurücksetzen? Sie landet dann im Verlauf.')) return;
  button.disabled = true;
  let result;
  if (action === 'gph-clear') result = await api.gphClearRequest();
  else if (action === 'gph-save') result = await api.gphRequest({ settings: gphFormSettings() });
  else result = await api.gphRequest({ action: action.slice(4) });
  if (result.error) { alert(result.error); button.disabled = false; return; }
  state.gph.notice = action === 'gph-clear' ? 'Auftrag zurückgezogen.'
    : 'Gesendet. Im Spiel <code>/reload</code> eingeben, damit das Addon den Auftrag ausführt.';
  state.gphAddon = await api.addonInfo('goldperhour');
  renderGoldPerHour();
}

content.addEventListener('click', (e) => {
  const row = e.target.closest('[data-gph-open]');
  if (!row) return;
  const id = Number(row.dataset.gphOpen);
  state.gph.open = state.gph.open === id ? null : id;
  renderGoldPerHour();
});

// ---------- Start ----------

(async () => {
  await loadStatus();
  showPage('connection');
  // Game process and snapshot ages change without file writes; refresh the light status every 30 s.
  setInterval(async () => {
    state.game = await api.wowRunning();
    updateLiveState();
  }, 30000);
})();
