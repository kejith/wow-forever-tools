// WoWMCP desktop app: renderer. Plain DOM, no framework; all data comes from window.wow (preload.js).
const api = window.wow;

const PAGES = {
  connection: { title: 'Verbindung', render: renderConnection },
  character: { title: 'Charakter', render: renderCharacter },
  writs: { title: 'Handwerksverfügungen', render: renderWrits },
  upgrades: { title: 'AH-Upgrades', render: renderUpgrades },
};

const state = {
  page: 'connection',
  char: null,
  status: null,
  addon: null,
  game: null,
  cache: new Map(),
  lastLoad: null,
  writs: { top: 3, besitzZumAhPreis: false },
  upgrades: { maxPreis: '', vorschau: 4, top: 3 },
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

// ---------- Data ----------

async function cached(key, fn) {
  if (!state.cache.has(key)) state.cache.set(key, fn());
  const result = await state.cache.get(key);
  if (result && result.error) state.cache.delete(key); // retry next time
  return result;
}

async function loadStatus() {
  const [status, addon, game] = await Promise.all([
    api.bridge('status'), api.addonInfo(), api.wowRunning(),
  ]);
  state.status = status;
  state.addon = addon;
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
  document.querySelectorAll('#nav button').forEach((b) => b.classList.toggle('active', b.dataset.page === page));
  $('#page-title').textContent = PAGES[page].title;
  try {
    await PAGES[page].render();
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
  await loadStatus();
  showPage(state.page);
});

// ---------- Page: connection ----------

function addonCard() {
  const a = state.addon;
  if (!a.installed) {
    return `<div class="card"><h2>WoWMCP-Addon</h2>
      <h3><span class="badge bad">nicht installiert</span></h3>
      <p class="muted small">Ziel: ${esc(a.installedDir)}</p>
      <button class="primary" data-action="install">Addon installieren</button></div>`;
  }
  const badge = a.upToDate ? '<span class="badge ok">aktuell</span>' : '<span class="badge warn">Update verfügbar</span>';
  return `<div class="card"><h2>WoWMCP-Addon</h2>
    <h3>${badge}</h3>
    <dl class="kv small">
      <dt>Installiert</dt><dd>${esc(a.installedVersion || '?')}</dd>
      <dt>Quelle</dt><dd>${esc(a.sourceVersion || '?')}</dd>
      ${a.changed.length && !a.upToDate ? `<dt>Geändert</dt><dd>${esc(a.changed.join(', '))}</dd>` : ''}
      <dt>Auctionator</dt><dd>${a.auctionatorInstalled ? 'installiert' : '<span class="badge bad">fehlt</span>'}</dd>
    </dl>
    <div class="row" style="margin-top:10px">
      ${a.upToDate ? '' : '<button class="primary" data-action="install">Addon aktualisieren</button>'}
      <button class="ghost" data-action="open-addon">Ordner öffnen</button>
    </div></div>`;
}

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
        <dt>Automatisch</dt><dd>${ex.legacy ? 'erst ab Addon 1.1' : ex.auto === false ? 'aus' : 'nach dem Schließen des AH'}</dd>
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
  if (a.installed && !a.upToDate) {
    hint = `<div class="notice">Die Addon-Quelle ist neuer als die Installation. „Addon aktualisieren“ kopiert sie ins Spiel.
      ${a.restartNeeded ? ' Danach WoW einmal neu starten, weil neue Dateien dazukommen.' : ' Danach reicht ein <code>/reload</code>.'}</div>`;
  }
  if (a.lastInstall?.restartNeeded) {
    hint = '<div class="notice"><b>Spiel neu starten:</b> Das Update bringt neue Dateien mit, die WoW erst nach einem Neustart lädt (ein <code>/reload</code> reicht dafür nicht).</div>';
  } else if (a.lastInstall) {
    hint = '<div class="notice">Addon aktualisiert. Im Spiel <code>/reload</code> eingeben.</div>';
  }
  content.innerHTML = `${hint}
    <div class="grid">${gameCard()}${addonCard()}${auctionatorCard()}${itemExportCard()}${requestCard()}</div>
    <div class="section"><h2>Charaktere</h2>${charactersTable()}</div>`;
}

content.addEventListener('click', async (e) => {
  const button = e.target.closest('button[data-action]');
  if (!button) return;
  const action = button.dataset.action;
  button.disabled = true;
  if (action === 'install') {
    const result = await api.installAddon();
    if (result.error) { alert(result.error); button.disabled = false; return; }
    state.addon = { ...result, lastInstall: result };
  } else if (action === 'open-addon') {
    api.openPath(state.addon.installedDir);
    button.disabled = false;
  } else if (action.startsWith('request-')) {
    const result = await api.createRequest(action === 'request-sync' ? 'sync' : 'items', { full: action === 'request-items-full' });
    if (result.error) { alert(result.error); button.disabled = false; return; }
    state.addon = await api.addonInfo();
  }
  if (state.page === 'connection') renderConnection();
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
  const shadow = st.spellDamage?.shadow;
  const crit = st.spellCrit?.shadow;
  const statCards = [
    ['Schattenschaden', shadow ?? '–'], ['Zauberkrit', crit !== undefined ? `${crit.toFixed(2)} %` : '–'],
    ['Zaubertreffer', st.spellHit !== undefined ? `${st.spellHit} %` : '–'], ['Leben', st.healthMax ?? '–'],
    ['Mana', st.powerMax ?? '–'], ['Intelligenz', st.intellect ?? '–'], ['Ausdauer', st.stamina ?? '–'],
    ['Willenskraft', st.spirit ?? '–'],
  ].map(([k, v]) => `<div class="card"><h2>${esc(k)}</h2><div class="big">${esc(v)}</div></div>`).join('');
  const eq = d.equipment.map((e) => `<tr>
      <td class="muted">${esc(e.slotName)}</td>
      <td class="${quality(e.quality)}">${esc(e.name)}</td>
      <td class="num">${esc(e.ilvl ?? '')}</td>
      <td class="small">${esc(e.stats)}</td>
      <td class="num">${e.score}</td>
      <td class="num small">${e.durability ? `${e.durability[0]}/${e.durability[1]}` : ''}</td></tr>`).join('');
  const profs = d.professions.map((p) => `<div style="margin-bottom:8px">
      <div class="row small"><span>${esc(p.name)}</span><span class="spacer"></span><span class="muted">${p.rank}/${p.max}</span></div>
      <div class="bar"><span style="width:${Math.round((p.rank / p.max) * 100)}%"></span></div></div>`).join('');
  const talents = d.talents.map((t) => Object.entries(t.tabs || {}).map(([tab, pts]) => `${esc(tab)} ${pts}`).join(' / ')).join('<br>');
  content.innerHTML = `
    <div class="card" style="margin-bottom:16px"><div class="row">
      <div><h3 style="font-size:20px;margin:0">${esc(c.name)} <span class="muted">${esc(c.surname || '')}</span></h3>
        <div class="muted">Stufe ${esc(c.level)} ${esc(c.race)} ${esc(c.class)} · ${esc(c.zone || '')} · Snapshot ${ago(d.updated)}</div></div>
      <span class="spacer"></span><div class="big">${money(c.money)}</div></div>
      <div style="margin-top:12px" class="small muted">Erfahrung ${xpPct} % (${(c.xp || 0).toLocaleString('de-DE')} / ${(c.xpMax || 0).toLocaleString('de-DE')})</div>
      <div class="bar"><span style="width:${xpPct}%"></span></div></div>
    <div class="grid" style="grid-template-columns: repeat(auto-fill, minmax(175px, 1fr))">${statCards}</div>
    <div class="section"><h2>Ausrüstung</h2><div class="card"><table>
      <thead><tr><th>Platz</th><th>Item</th><th class="num">iLvl</th><th>Werte</th><th class="num">Punkte</th><th class="num">Haltbarkeit</th></tr></thead>
      <tbody>${eq}</tbody></table>
      <p class="small muted">Punkte nach den Gewichten der AH-Upgrades (Schatten/Zaubermacht 1, Ausdauer 0,5, Int 0,35, Willenskraft 0,25).</p></div></div>
    <div class="grid section"><div class="card"><h2>Berufe</h2>${profs || '<span class="muted">–</span>'}</div>
      <div class="card"><h2>Talente</h2>${talents || '<span class="muted">–</span>'}</div></div>`;
}

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
        <td>${i === 0 ? '★ ' : ''}${esc(b.name)}${b.qty > 1 ? ` <span class="muted">(${b.qty}x)</span>` : ''}</td>
        <td class="muted">${esc(b.prof)}</td>
        <td class="num">${money(b.wcost)}</td><td class="num">${money(b.icost)}</td>
        <td class="num"><b>${money(b.cost)}</b></td>
        <td class="num">${(b.per / 100).toFixed(2)} s</td>
        <td class="num muted">${b.writs}</td>
        <td class="small">${esc(b.note)}</td></tr>`).join('');
    const sealed = t.sealed ? `<p class="small notice">Du hast ${t.sealed}x „Versiegelte Verfügung“ dieser Stufe. Erst öffnen, vielleicht ist sie billiger.</p>` : '';
    return `<div class="card section"><div class="row"><h3>${esc(t.name)}</h3></div>
      ${rows ? `<table><thead><tr><th>Verfügung</th><th>Beruf</th><th class="num">Verfügung</th><th class="num">Item</th>
        <th class="num">Summe</th><th class="num">pro Ruf</th><th class="num">im AH</th><th>Hinweis</th></tr></thead><tbody>${rows}</tbody></table>`
        : '<p class="muted">Keine Verfügung mit Preisdaten.</p>'}${sealed}</div>`;
  }).join('');
  body.innerHTML = `${repHtml}
    <div class="card"><div class="row"><div><h2>Pro Tag</h2><div class="big">${money(d.dailyTotal)}</div></div>
      <span class="spacer"></span><div class="muted">für ${d.dailyRep} Ruf (je Stufe die beste Verfügung)</div></div></div>
    ${tiers}
    <p class="small muted">Items im Besitz zählen als 0. Verfügungsliste: wow.gg (Nov 2026).</p>`;
}

// ---------- Page: upgrades ----------

async function renderUpgrades() {
  if (!state.status) await loadStatus();
  const o = state.upgrades;
  content.innerHTML = `<div class="controls">
      <label>Max. Preis pro Item (Gold, leer = eigenes Gold)<input type="number" id="u-max" min="0" step="0.5" value="${esc(o.maxPreis)}" placeholder="eigenes Gold"></label>
      <label>Vorschau (Stufen voraus)<input type="number" id="u-pre" min="0" max="10" value="${o.vorschau}"></label>
      <label>Vorschläge pro Platz<input type="number" id="u-top" min="1" max="10" value="${o.top}"></label>
    </div><div id="u-body">${loading('Vergleiche AH-Items mit deiner Ausrüstung …')}</div>`;
  const bind = (id, field, parse) => $(id).addEventListener('change', (e) => { o[field] = parse(e.target.value); renderUpgrades(); });
  bind('#u-max', 'maxPreis', (v) => v);
  bind('#u-pre', 'vorschau', (v) => Number(v) || 0);
  bind('#u-top', 'top', (v) => Number(v) || 3);
  const key = `upgrades:${state.char}:${o.maxPreis}:${o.vorschau}:${o.top}`;
  const d = await cached(key, () => api.bridge('upgrades', {
    char: state.char, maxPreis: o.maxPreis ? Number(o.maxPreis) : 0, vorschau: o.vorschau, top: o.top,
  }));
  const body = $('#u-body');
  if (!body) return;
  if (d.error) {
    body.innerHTML = `${errorBox(d.error)}<div class="notice">Im Auktionshaus mit Auctionator scannen und das AH schließen:
      Das Addon liest neue Items dann selbst. Oder auf der Seite „Verbindung“ einen Item-Export anfordern.</div>`;
    return;
  }
  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => c.gain)));
  const slots = d.slots.map((s) => {
    const cur = s.current;
    const rows = s.candidates.map((c) => `<tr>
        <td><div class="gain"><b>+${c.gain.toFixed(1)}</b><div class="bar"><span style="width:${Math.round((c.gain / maxGain) * 100)}%"></span></div></div></td>
        <td class="num">${money(c.price)}</td>
        <td><span class="${quality(c.quality)}">${esc(c.name)}</span>
          ${c.isNew ? '<span class="badge new">NEU in Forever</span>' : ''}
          ${c.soon ? `<span class="badge warn">ab Stufe ${c.reqLevel}</span>` : ''}
          <div class="small muted">${esc(c.stats)}</div>
          ${c.effects.map((t) => `<div class="effects">${esc(t)}</div>`).join('')}</td></tr>`).join('');
    return `<div class="card section slot-card">
      <div><div class="slot-name">${esc(s.slotName)}</div>
        <div class="small muted" style="margin-top:4px">jetzt:</div>
        <div class="small ${quality(cur?.quality)}">${esc(cur?.name || '–')}</div>
        <div class="small muted">${esc(cur?.stats || '')}</div></div>
      <table><thead><tr><th>Gewinn</th><th class="num">Preis</th><th>Item</th></tr></thead><tbody>${rows}</tbody></table></div>`;
  }).join('');
  const review = d.review.length ? `<div class="section"><h2>Mit Spezialeffekt – selbst ansehen</h2><div class="card"><table>
      <thead><tr><th>Platz</th><th class="num">Preis</th><th>Item</th><th>Effekt</th></tr></thead><tbody>
      ${d.review.map((r) => `<tr><td class="muted">${esc(r.slot)}</td><td class="num">${money(r.price)}</td>
        <td class="${quality(r.quality)}">${esc(r.name)} ${r.isNew ? '<span class="badge new">NEU</span>' : ''}</td>
        <td class="small">${esc(r.effects.join(' / '))}</td></tr>`).join('')}</tbody></table></div></div>` : '';
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>Budget</h2><div class="big">${money(d.budget)}</div></div>
      <div><h2>Stufe</h2><div class="big">${d.level}</div></div>
      <div><h2>AH-Scan</h2><div class="big">${esc(d.scanDate)}</div></div>
      <div><h2>Item-Export</h2><div class="big">${d.exportCount.toLocaleString('de-DE')}</div><div class="small muted">${ago(d.exportUpdated)}</div></div>
      <span class="spacer"></span></div>
      <p class="small muted">Gewichte: ${esc(d.weights)}. Preise = Mindest-Sofortkauf beim Scan.
        Items mit Zufallswerten („des Schattenzorns“) kann Auctionator nicht unterscheiden, sie fehlen hier.</p></div>
    ${slots || '<div class="notice section">Keine Upgrades in diesem Budget.</div>'}${review}`;
}

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
