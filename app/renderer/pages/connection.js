// Page "Connection": is the game running, the two addons the app installs, the Auctionator scan, the item
// export, the lists the app sends into the game, the character snapshots and requests to the WoW Tools Forever addon.
import { $, ago, api, content, daysOld, errorBox, esc, help, hintAttr, loadStatus, loading, money, num, percent, rerender, selectChar, state, th, when } from '../core.js';
import { t } from '../i18n.js';

// Where an install takes its files from: the latest GitHub release, or a local checkout from the settings.
function addonSource(a) {
  const s = a.source;
  if (!s) return `<span class="badge bad">${esc(t('not available'))}</span> ${esc(a.sourceError || '')}`;
  if (s.kind === 'local') return `${esc(s.version || '?')} <span class="badge">${esc(t('local folder'))}</span> <span class="muted">${esc(s.dir)}</span>`;
  const stale = s.offline
    ? ` <span class="badge warn"${hintAttr(s.error)}>${esc(t('not checked, as of {time}', { time: when(s.checkedAt / 1000) }))}</span>` : '';
  return `${esc(s.version || '?')} <span class="muted">${esc(t('Release {tag} from github.com/{repo}', { tag: s.tag, repo: s.repo }))}</span>${stale}`;
}

// One card per addon the app installs; `key` is the addon key in addon.js (wowtoolsforever, goldperhour).
export function addonCard(a, about = '') {
  const key = esc(a.key);
  const title = esc(t('{name} addon', { name: a.name }));
  const check = a.source?.kind === 'local' ? ''
    : `<button class="ghost" data-action="check-addon" data-addon="${key}">${esc(t('Check for updates'))}</button>`;
  const hint = !a.lastInstall ? '' : `<div class="notice small" style="margin:10px 0 0">${a.lastInstall.firstInstall
    ? t('Installed. Type <code>/reload</code> in the game. If it does not show up in the addon list afterwards, restart WoW once.')
    : t('Updated. Type <code>/reload</code> in the game.')}</div>`;
  if (!a.installed) {
    return `<div class="card" id="card-${key}"><h2>${title}</h2>
      <h3><span class="badge bad">${esc(t('not installed'))}</span></h3>
      ${about ? `<p class="small">${esc(about)}</p>` : ''}
      <dl class="kv small"><dt>${esc(t('Source'))}</dt><dd>${addonSource(a)}</dd><dt>${esc(t('Target'))}</dt><dd>${esc(a.installedDir)}</dd></dl>
      <div class="card-actions">
        <button class="primary" data-action="install" data-addon="${key}" ${a.source ? '' : 'disabled'}>${esc(t('Install addon'))}</button>
        ${check}
      </div></div>`;
  }
  const badge = !a.source ? `<span class="badge">${esc(t('installed'))}</span>`
    : a.ahead ? `<span class="badge ok">${esc(t('newer than the source'))}</span>`
    : a.upToDate ? `<span class="badge ok">${esc(t('up to date'))}</span>` : `<span class="badge warn">${esc(t('update available'))}</span>`;
  return `<div class="card" id="card-${key}"><h2>${title}</h2>
    <h3>${badge}</h3>
    <dl class="kv small">
      <dt>${esc(t('Installed'))}</dt><dd>${esc(a.installedVersion || '?')}</dd>
      <dt>${esc(t('Source'))}</dt><dd>${addonSource(a)}</dd>
      ${a.changed.length && !a.upToDate ? `<dt>${esc(t('Changed'))}</dt><dd>${esc(a.changed.join(', '))}</dd>` : ''}
      <dt>Auctionator</dt><dd>${a.auctionatorInstalled ? esc(t('installed')) : `<span class="badge bad">${esc(t('missing'))}</span>`}</dd>
    </dl>${hint}
    <div class="card-actions">
      ${a.upToDate ? '' : `<button class="primary" data-action="install" data-addon="${key}">${esc(t('Update addon'))}</button>`}
      ${check}
      <button class="ghost" data-action="open-addon" data-addon="${key}">${esc(t('Open folder'))}</button>
    </div></div>`;
}

export const gphAbout = () => t('Gold per hour, split into herbalism and loot. The app shows sessions, history and settings.');

// ---------- The app itself ----------
// state.update is the updater of the main process (update.js): the newest release of the app, and how far
// its installer is (found, downloading, ready). Every change arrives through api.onUpdate.

const megabytes = (bytes) => num(bytes / (1024 * 1024), 0);
const loaded = (p) => (p?.total ? Math.min(100, Math.floor((p.received / p.total) * 100)) : 0);
const loadedText = (p) => t('{done} of {total} MB', { done: megabytes(p?.received || 0), total: megabytes(p?.total || 0) });

const UPDATE_BADGE = {
  idle: () => ['', t('not checked yet')],
  checking: () => ['', t('checking …')],
  current: () => ['ok', t('up to date')],
  available: () => ['warn', t('update available')],
  downloading: () => ['warn', t('downloading …')],
  ready: () => ['ok', t('ready to install')],
  installing: () => ['warn', t('installing …')],
};

export function appCard() {
  const u = state.update;
  const title = `<h2>WoW Tools Forever ${help(esc(t('The app asks GitHub for its newest release when it starts and every six hours. A newer version is downloaded in the background, compared with the checksum of the release and installed when you close the app, or right away with “Restart and update”. Settings and downloaded data stay. Without the switch the app only tells you that there is a new version.')))}</h2>`;
  if (!u) return `<div class="card" id="app-update">${title}${loading(t('Looking for updates …'))}</div>`;
  const [cls, label] = (UPDATE_BADGE[u.phase] || UPDATE_BADGE.idle)();
  const newer = ['available', 'downloading', 'ready', 'installing'].includes(u.phase);
  const busy = ['checking', 'downloading', 'installing'].includes(u.phase);
  let note = '';
  if (u.reason === 'source') note = t('Started from source: update it with <code>git pull</code>. Only the installed app updates itself.');
  else if (u.reason === 'platform') note = esc(t('The app updates itself only when it is installed on Windows. Take a new version from the release page.'));
  else if (u.phase === 'ready') {
    note = esc(u.automatic
      ? t('Version {version} is downloaded and goes in when you close the app. “Restart and update” does it now.', { version: u.latest.version })
      : t('Version {version} is downloaded. “Restart and update” installs it.', { version: u.latest.version }));
  } else if (u.phase === 'installing') note = esc(t('The installer is running. The app closes and starts again by itself.'));
  const progress = u.phase !== 'downloading' ? '' : `<div class="bar" style="margin:10px 0 4px"><span style="width:${loaded(u.progress)}%"></span></div>
    <div class="small muted u-progress">${esc(loadedText(u.progress))}</div>`;
  const buttons = [
    u.phase === 'ready' && u.canInstall ? `<button class="primary" data-action="update-install">${esc(t('Restart and update'))}</button>` : '',
    u.phase === 'available' && u.canInstall ? `<button class="primary" data-action="update-download">${esc(t('Download update'))}</button>` : '',
    `<button class="ghost" data-action="update-check" ${busy ? 'disabled' : ''}>${esc(t('Check for updates'))}</button>`,
    newer && u.latest ? `<button class="ghost" data-action="update-notes"${hintAttr(t('Opens the page of the release in the browser.'))}>${esc(t('What is new'))}</button>` : '',
  ].join('');
  return `<div class="card" id="app-update" data-phase="${esc(u.phase)}">${title}
    <h3><span class="badge ${cls}">${esc(label)}</span></h3>
    <dl class="kv small">
      <dt>${esc(t('Installed'))}</dt><dd>${esc(u.current)}${u.updated ? ` <span class="badge ok">${esc(t('just updated'))}</span>` : ''}</dd>
      <dt>${esc(t('Newest release'))}</dt><dd>${u.latest ? `${esc(u.latest.version)} <span class="muted">${esc(when(Date.parse(u.latest.publishedAt) / 1000))}</span>` : '–'}</dd>
      <dt>${esc(t('Checked'))}</dt><dd>${u.checkedAt ? `${when(u.checkedAt / 1000)} (${esc(ago(u.checkedAt / 1000))})` : '–'}</dd>
    </dl>
    ${progress}
    ${note ? `<div class="notice small" style="margin:10px 0 0">${note}</div>` : ''}
    ${u.error ? `<div class="notice small" style="margin:10px 0 0"><span class="badge warn">${esc(t('not possible right now'))}</span> ${esc(u.error)}</div>` : ''}
    <div class="card-actions">${buttons}</div>
    ${u.canInstall ? `<label class="check" style="margin-top:10px"><input type="checkbox" id="u-auto" ${u.automatic ? 'checked' : ''}> ${esc(t('download and install updates automatically'))}</label>` : ''}
  </div>`;
}

// Brings the card and the note in the sidebar up to date. While a download runs only its progress moves,
// so the card under the mouse is not rebuilt four times a second.
export function showUpdate() {
  const u = state.update;
  const pill = $('#update-pill');
  const version = u?.latest?.version;
  const text = !u || !version ? '' : u.phase === 'available' ? t('Update {version} available', { version })
    : u.phase === 'downloading' ? t('Loading update {version} … {percent}', { version, percent: percent(loaded(u.progress)) })
    : u.phase === 'ready' ? (u.canInstall ? t('Restart to update to {version}', { version }) : t('Update {version} available', { version }))
    : u.phase === 'installing' ? t('Installing update {version} …', { version }) : '';
  pill.hidden = !text;
  pill.textContent = text;
  pill.classList.toggle('ready', u?.phase === 'ready');
  const card = $('#app-update');
  if (!card || state.page !== 'connection') return;
  if (u?.phase === 'downloading' && card.dataset.phase === 'downloading') {
    card.querySelector('.bar > span').style.width = `${loaded(u.progress)}%`;
    card.querySelector('.u-progress').textContent = loadedText(u.progress);
  } else {
    card.outerHTML = appCard();
  }
}

// The buttons of the card. Each step answers through api.onUpdate, which redraws the card.
function updateAction(action) {
  if (action === 'update-check') api.checkUpdate();
  else if (action === 'update-download') api.downloadUpdate();
  else if (action === 'update-install') api.installUpdate();
  else if (action === 'update-notes') api.openReleaseNotes();
}

function gameCard() {
  const s = state.status;
  const g = state.game || {};
  const running = g.running ? `<span class="badge ok">${esc(t('running'))}</span>`
    : g.otherClient ? `<span class="badge warn">${esc(t('another WoW client is open'))}</span>` : `<span class="badge">${esc(t('closed'))}</span>`;
  const client = s.characters?.[0]?.client;
  return `<div class="card" id="card-game"><h2>WoW Forever</h2>
    <h3>${running}</h3>
    <dl class="kv small">
      <dt>${esc(t('Folder'))}</dt><dd>${s.wowFound ? esc(s.wowDir) : `<span class="badge bad">${esc(t('not found'))}</span> ${esc(s.wowDir)}`}</dd>
      <dt>${esc(t('Client'))}</dt><dd>${esc(client?.version || '?')} (${esc(client?.build || '?')})${s.clientLocale ? ` · ${esc(s.clientLocale)}` : ''}</dd>
    </dl></div>`;
}

function auctionatorCard() {
  const au = state.status.auctionator;
  const title = `<h2>${esc(t('Auction house scan'))} ${help(t('Prices are the lowest buyout at the last scan, not live offers. For fresh prices run a full Auctionator scan in the game, then type <code>/reload</code>.'))}</h2>`;
  if (!au || au.error) return `<div class="card" id="card-scan">${title}${errorBox(au?.error || t('No Auctionator data'))}</div>`;
  const days = au.scanDate ? Math.round((Date.now() - new Date(au.scanDate)) / 86400000) : null;
  const badge = days === null ? '' : days <= 1 ? `<span class="badge ok">${esc(t('fresh'))}</span>` : `<span class="badge warn">${esc(daysOld(days))}</span>`;
  return `<div class="card" id="card-scan">${title}
    <div class="big">${esc(au.scanDate || '–')} ${badge}</div>
    <dl class="kv small">
      <dt>${esc(t('Items'))}</dt><dd>${num(au.items)}</dd>
      <dt>${esc(t('Saved'))}</dt><dd>${when(au.modified)}</dd>
    </dl></div>`;
}

function itemExportCard() {
  const ex = state.status.itemExport;
  const auto = !ex ? '' : ex.legacy ? t('old 1.0 export, taken over at the next /reload')
    : ex.auto === false ? t('off') : t('after closing the auction house');
  const body = ex ? `<div class="big">${esc(t('{n} items', { n: num(ex.count) }))}</div>
      <dl class="kv small">
        <dt>${esc(t('As of'))}</dt><dd>${when(ex.updated)} (${esc(ago(ex.updated))})</dd>
        ${ex.missing ? `<dt>${esc(t('Not loaded'))}</dt><dd>${esc(ex.missing)}</dd>` : ''}
        ${ex.lastRun ? `<dt>${esc(t('Last run'))}</dt><dd>${esc(t('{new} new of {total}', { new: ex.lastRun.new, total: ex.lastRun.total }))}</dd>` : ''}
        <dt>${esc(t('Automatic'))}</dt><dd>${esc(auto)}</dd>
      </dl>`
    : `<div class="big muted">${esc(t('none yet'))}</div>`;
  return `<div class="card" id="card-export"><h2>${esc(t('Item export'))} ${help(esc(t('Stats and tooltips of the gear in the auction house, read by the WoW Tools Forever addon in the game. The page “AH upgrades” compares these items with your gear.')))}</h2>${body}</div>`;
}

function requestCard() {
  const a = state.addon;
  const pending = a.pendingRequest;
  const done = state.status.lastRequest;
  let stateHtml = `<p class="muted small">${esc(t('No open request.'))}</p>`;
  if (pending && (!done || done.id !== pending.id)) {
    stateHtml = `<div class="notice"><b>${esc(t('Request #{id} is waiting', { id: pending.id }))}</b> (${esc(pending.action === 'sync' ? t('snapshot') : t('item export'))})
      <ol class="steps"><li>${t('Type <code>/reload</code> in the game; the addon starts the request.')}</li>
      <li>${esc(t('Wait for the chat message from WoW Tools Forever.'))}</li>
      <li>${t('Type <code>/reload</code> again, then the app updates itself.')}</li></ol></div>`;
  } else if (done) {
    const err = done.error ? ` <span class="badge bad">${esc(done.error)}</span>` : '';
    stateHtml = `<p class="small"><span class="badge ok">${esc(t('done'))}</span> ${esc(t('Request #{id} ({action})', { id: done.id, action: done.action }))} ${when(done.done)}${err}
      ${done.new !== undefined ? ` · ${esc(t('{n} new items', { n: done.new }))}` : ''}</p>`;
  }
  const disabled = a.installed ? '' : 'disabled';
  return `<div class="card" style="grid-column: 1 / -1"><h2>${esc(t('Requests to the addon'))}
      ${help(t('The app puts a request into the addon folder; the game reads it at the next <code>/reload</code>. Results come back through the SavedVariables.'))}</h2>
    ${stateHtml}
    <div class="row">
      <button class="primary" data-action="request-items" ${disabled}${hintAttr(t('Reads the items of the last AH scans the export does not know yet.'))}>${esc(t('Export new AH items'))}</button>
      <button class="ghost" data-action="request-items-full" ${disabled}${hintAttr(t('Reads every item again, for example after an addon update. Takes longer.'))}>${esc(t('Read all items again'))}</button>
      <button class="ghost" data-action="request-sync" ${disabled}${hintAttr(t('Exports stats, gear, bags, quests and professions of the character that is logged in.'))}>${esc(t('Character snapshot'))}</button>
    </div></div>`;
}

// ---------- Lists in the game ----------
// state.ingame is the main process' last sync (scripts/ingame.py): what the lists hold per character, the
// file in the addon folder and what the game has loaded of it.

// Where the lists are: [badge class, badge text, hint (HTML)].
function listsState(g) {
  if (!g.addon?.installed) return ['bad', t('addon not installed'), esc(t('Install the WoW Tools Forever addon above.'))];
  if (!g.addon.ready) {
    return ['warn', t('needs WoW Tools Forever 1.7'), t('WoW Tools Forever {version} cannot show lists. Update the addon above, then type <code>/reload</code> in the game.',
      { version: esc(g.addon.version || '?') })];
  }
  if (!g.file) return ['', t('not sent yet'), ''];
  if (g.file.hash !== g.hash) return ['warn', t('changed since sent'), esc(t('The game still gets the lists as of {time}.', { time: when(g.file.generated) }))];
  // The game read the file if the addon says so, or if it saved (so it reloaded) after the file was written.
  const loaded = g.loaded?.hash === g.file.hash || (g.saved || 0) > (g.file.modified || 0);
  if (loaded) return ['ok', t('in the game'), t('<code>/wtf show</code> opens the window; it also opens with the auction house.')];
  if (state.game?.running) return ['warn', t('waiting for /reload'), t('Type <code>/reload</code> in the game to load the lists.')];
  return ['ok', t('ready for the next login'), ''];
}

function listsTable(g) {
  const rows = (g.chars || []).map((c) => {
    const lists = c.lists.length
      ? `<div class="pill-list">${c.lists.map((l) => `<span class="pill-tag">${esc(l.title)} <b>${num(l.rows)}</b></span>`).join('')}</div>`
      : '<span class="muted">–</span>';
    const errors = Object.entries(c.errors || {});
    return `<tr>
      <td style="white-space:nowrap"><b>${esc(c.name)}</b> <span class="muted">${esc(c.level ?? '')}</span></td>
      <td>${lists}</td>
      <td class="num">${num(c.lists.reduce((n, l) => n + l.shop, 0))}</td>
      <td class="num">${num(c.tips)}</td>
      <td class="num">${num(c.vendor)}</td>
      <td>${errors.length ? errors.map(([k, v]) => `<span class="badge bad"${hintAttr(v)}>${esc(k)}</span>`).join(' ') : '<span class="muted">–</span>'}</td>
    </tr>`;
  }).join('');
  if (!rows) return `<p class="muted small">${esc(t('No character exported yet.'))}</p>`;
  const head = th([t('Character')],
    [t('Lists'), '', esc(t('The tabs of the window in the game, each with its number of rows.'))],
    [t('To buy'), 'num', esc(t('Rows the app suggests buying. They are also in the Auctionator shopping lists “WoW Tools Forever: …”, one per list of the character that is logged in.'))],
    [t('Tooltips'), 'num', esc(t('Items that get a line from the app in their tooltip in the game, for example the gain of an upgrade or the profit of a recipe.'))],
    [t('Vendor hints'), 'num', esc(t('Recipes worth buying that a vendor sells. The addon names them in the chat when you open such a vendor.'))],
    [t('Errors')]);
  return `<table style="margin-top:10px"><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table>`;
}

function listsCard() {
  const g = state.ingame;
  const title = `<h2>${esc(t('Lists in the game'))} ${help(t('The WoW Tools Forever addon shows what this app found in the game: a window with AH upgrades, writs, trade crates, recipes and consumables. A click on a row searches the auction house for the item, a shift-click puts its link into the chat or the search box. The app writes the lists into the addon folder and the game reads them on <code>/reload</code>. After a new AH scan that takes two: the first <code>/reload</code> saves the scan for the app, the second loads the new lists.'))}</h2>`;
  const wide = 'class="card" id="lists" style="grid-column: 1 / -1"';
  if (!g) return `<div ${wide}>${title}${loading(t('Computing the lists …'))}</div>`;
  if (g.error) return `<div ${wide}>${title}${errorBox(g.error)}</div>`;
  const [cls, label, hint] = listsState(g);
  const failed = g.result && !g.result.written && !g.result.unchanged && g.addon?.ready ? errorBox(g.result.reason || '') : '';
  const unsent = !g.file || g.file.hash !== g.hash;
  return `<div ${wide}>${title}
    <h3><span class="badge ${cls}">${esc(label)}</span>${hint ? ` <span class="small muted" style="font-weight:400">${hint}</span>` : ''}</h3>
    ${failed}
    <dl class="kv small">
      <dt>${esc(t('Sent'))}</dt><dd>${g.file ? `${when(g.file.generated)} (${esc(ago(g.file.generated))})` : '–'}</dd>
      <dt>${esc(t('AH scan'))}</dt><dd>${esc(g.scanDate || '–')}</dd>
    </dl>
    ${listsTable(g)}
    <div class="row" style="margin-top:10px">
      <button class="${unsent ? 'primary' : 'ghost'}" data-action="send-lists" ${g.addon?.ready ? '' : 'disabled'}${hintAttr(t('Computes the lists again and writes them into the addon folder.'))}>${esc(t('Send now'))}</button>
      <label class="check"><input type="checkbox" id="g-auto" ${g.automatic ? 'checked' : ''}> ${esc(t('send automatically when the game has saved'))}</label>
    </div></div>`;
}

function charactersTable() {
  const chars = state.status.characters || [];
  if (!chars.length) {
    return `<div class="notice">${t('No character exported yet. Log in and type <code>/wtf sync</code>.')}</div>`;
  }
  const rows = chars.map((c) => {
    if (c.error) return `<tr><td>${esc(c.key)}</td><td colspan="6">${errorBox(c.error)}</td></tr>`;
    const errors = Object.entries(c.errors || {});
    const old = c.addonVersion && state.addon.sourceVersion && c.addonVersion !== state.addon.sourceVersion;
    const active = c.key === state.char;
    return `<tr class="char-row${active ? ' active' : ''}" data-char-select="${esc(c.key)}"${hintAttr(active ? t('Currently selected character') : t('Click to select this character'))}>
      <td><b>${esc(c.name)}</b> <span class="muted">${esc(c.surname || '')}</span></td>
      <td>${esc(c.level)} ${esc(c.race || '')} ${esc(c.class || '')}</td>
      <td class="num">${money(c.money)}</td>
      <td>${esc(c.zone || '')}</td>
      <td>${esc(ago(c.updated))}</td>
      <td>${c.addonVersion ? `<span class="badge ${old ? 'warn' : 'ok'}">${esc(c.addonVersion)}</span>` : '<span class="badge">1.0</span>'}</td>
      <td>${errors.length ? errors.map(([k, v]) => `<span class="badge bad"${hintAttr(v)}>${esc(k)}</span>`).join(' ') : '<span class="muted">–</span>'}</td>
    </tr>`;
  }).join('');
  const head = th([t('Character')], [t('Level/class')], [t('Gold'), 'num'], [t('Zone')],
    ['Snapshot', '', t('Age of the character data. WoW writes it only on <code>/reload</code> or logout.')], ['Addon'], [t('Errors')]);
  return `<div class="card" id="card-chars"><table><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table></div>`;
}

// The ready check at the top: every part the app needs at a glance, clickable to jump to its card.
function readyCheck() {
  const s = state.status;
  const g = state.game || {};
  const addon = (a) => (!a ? ['bad', t('not available')] : !a.installed ? ['bad', t('not installed')]
    : !a.source || a.upToDate || a.ahead ? ['ok', t('up to date')] : ['warn', t('update available')]);
  const au = s.auctionator;
  const days = au?.scanDate ? Math.round((Date.now() - new Date(au.scanDate)) / 86400000) : null;
  const scan = !au || au.error ? ['bad', t('No Auctionator data')] : days === null ? ['warn', '–'] : days <= 1 ? ['ok', t('fresh')] : ['warn', daysOld(days)];
  const ex = s.itemExport;
  const chars = (s.characters || []).filter((c) => !c.error);
  const lists = state.ingame && !state.ingame.error ? listsState(state.ingame) : ['', state.ingame?.error ? t('not available') : t('Computing the lists …')];
  const checks = [
    [g.running ? 'ok' : 'warn', 'WoW Forever', g.running ? t('running') : g.otherClient ? t('another WoW client is open') : t('closed'), '#card-game'],
    [addon(state.addon)[0], t('{name} addon', { name: 'WoW Tools Forever' }), addon(state.addon)[1], '#card-wowtoolsforever'],
    [addon(state.gphAddon)[0], t('{name} addon', { name: 'GoldPerHour' }), addon(state.gphAddon)[1], '#card-goldperhour'],
    [scan[0], t('Auction house scan'), scan[1], '#card-scan'],
    [ex ? 'ok' : 'warn', t('Item export'), ex ? t('{n} items', { n: num(ex.count) }) : t('none yet'), '#card-export'],
    [lists[0], t('Lists in the game'), lists[1], '#lists'],
    [chars.length ? 'ok' : 'bad', t('Characters'), chars.length ? chars.map((c) => c.name).join(', ') : t('No character exported yet.'), '#card-chars'],
  ];
  const mark = { ok: '✓', warn: '!', bad: '✕', '': '…' };
  return `<div class="ready">${checks.map(([cls, title, sub, target]) => `<button type="button" class="ready-item ${cls}" data-ready-jump="${target}">
      <span class="ready-mark">${mark[cls] ?? '…'}</span>
      <span class="ready-text"><b>${esc(title)}</b><span>${esc(sub)}</span></span></button>`).join('')}</div>`;
}

export async function renderConnection() {
  if (!state.status) {
    content.innerHTML = loading(t('Connecting to WoW Forever …'));
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
    hint = `<div class="notice">${t('{names}: the source has a version other than the installed one. “Update addon” copies it into the game; type <code>/reload</code> in the game afterwards.',
      { names: esc(outdated.map((x) => x.name).join(', ')) })}</div>`;
  }
  content.innerHTML = `${readyCheck()}${hint}
    <div class="conn-strip">${gameCard()}${auctionatorCard()}${itemExportCard()}</div>
    <div class="conn-addons">${addonCard(a)}${addonCard(state.gphAddon, gphAbout())}${appCard()}</div>
    <div class="grid">${listsCard()}${requestCard()}</div>
    <div class="section"><h2>${esc(t('Characters'))}</h2>${charactersTable()}</div>`;
}

// Buttons of the addon cards (also shown on the GoldPerHour page) and of the request card.
content.addEventListener('click', async (e) => {
  const jump = e.target.closest('[data-ready-jump]');
  if (jump) {
    const el = $(jump.dataset.readyJump);
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' });
      el.classList.remove('card-flash');
      void el.offsetWidth;
      el.classList.add('card-flash');
    }
    return;
  }
  const charRow = e.target.closest('[data-char-select]');
  if (charRow && state.page === 'connection') {
    selectChar(charRow.dataset.charSelect);
    return;
  }
  const button = e.target.closest('button[data-action]');
  if (!button) return;
  const action = button.dataset.action;
  if (action.startsWith('gph-')) return; // pages/goldperhour.js
  if (action.startsWith('update-')) { updateAction(action); return; }
  const key = button.dataset.addon === 'goldperhour' ? 'goldperhour' : 'wowtoolsforever';
  const slot = key === 'goldperhour' ? 'gphAddon' : 'addon';
  button.disabled = true;
  if (action === 'install') {
    const result = await api.installAddon(key);
    if (result.error) { alert(result.error); button.disabled = false; return; }
    state[slot] = { ...result, lastInstall: result };
  } else if (action === 'check-addon') {
    // Asks GitHub now; otherwise the app keeps the last answer for a quarter of an hour.
    state[slot] = await api.addonInfo(key, { force: true });
  } else if (action === 'open-addon') {
    api.openPath(state[slot].installedDir);
    button.disabled = false;
  } else if (action === 'send-lists') {
    state.ingame = await api.sendGameData();
  } else if (action.startsWith('request-')) {
    const result = await api.createRequest(action === 'request-sync' ? 'sync' : 'items', { full: action === 'request-items-full' });
    if (result.error) { alert(result.error); button.disabled = false; return; }
    state.addon = await api.addonInfo('wowtoolsforever');
  }
  if (state.page === 'connection' || state.page === 'goldperhour') rerender();
});

// The switch of the card "Lists in the game"; the main process syncs again and reports through onGameData.
content.addEventListener('change', async (e) => {
  // The switch of the app's card: the main process answers through onUpdate.
  if (e.target.id === 'u-auto') { await api.setSettings({ autoUpdate: e.target.checked }); return; }
  if (e.target.id !== 'g-auto') return;
  if (state.ingame) state.ingame.automatic = e.target.checked;
  await api.setSettings({ sendToGame: e.target.checked });
});
