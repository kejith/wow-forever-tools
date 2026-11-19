// Page "Connection": is the game running, the two addons the app installs, the Auctionator scan, the item
// export, the lists the app sends into the game, the character snapshots and requests to the WoWMCP addon.
import { ago, api, content, daysOld, errorBox, esc, help, hintAttr, loadStatus, loading, money, num, rerender, state, th, when } from '../core.js';
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

// One card per addon the app installs; `key` is the addon key in addon.js (wowmcp, goldperhour).
export function addonCard(a, about = '') {
  const key = esc(a.key);
  const title = esc(t('{name} addon', { name: a.name }));
  const check = a.source?.kind === 'local' ? ''
    : `<button class="ghost" data-action="check-addon" data-addon="${key}">${esc(t('Check for updates'))}</button>`;
  const hint = !a.lastInstall ? '' : `<div class="notice small" style="margin:10px 0 0">${a.lastInstall.firstInstall
    ? t('Installed. Type <code>/reload</code> in the game. If it does not show up in the addon list afterwards, restart WoW once.')
    : t('Updated. Type <code>/reload</code> in the game.')}</div>`;
  if (!a.installed) {
    return `<div class="card"><h2>${title}</h2>
      <h3><span class="badge bad">${esc(t('not installed'))}</span></h3>
      ${about ? `<p class="small">${esc(about)}</p>` : ''}
      <dl class="kv small"><dt>${esc(t('Source'))}</dt><dd>${addonSource(a)}</dd><dt>${esc(t('Target'))}</dt><dd>${esc(a.installedDir)}</dd></dl>
      <div class="row" style="margin-top:10px">
        <button class="primary" data-action="install" data-addon="${key}" ${a.source ? '' : 'disabled'}>${esc(t('Install addon'))}</button>
        ${check}
      </div></div>`;
  }
  const badge = !a.source ? `<span class="badge">${esc(t('installed'))}</span>`
    : a.ahead ? `<span class="badge ok">${esc(t('newer than the source'))}</span>`
    : a.upToDate ? `<span class="badge ok">${esc(t('up to date'))}</span>` : `<span class="badge warn">${esc(t('update available'))}</span>`;
  return `<div class="card"><h2>${title}</h2>
    <h3>${badge}</h3>
    <dl class="kv small">
      <dt>${esc(t('Installed'))}</dt><dd>${esc(a.installedVersion || '?')}</dd>
      <dt>${esc(t('Source'))}</dt><dd>${addonSource(a)}</dd>
      ${a.changed.length && !a.upToDate ? `<dt>${esc(t('Changed'))}</dt><dd>${esc(a.changed.join(', '))}</dd>` : ''}
      <dt>Auctionator</dt><dd>${a.auctionatorInstalled ? esc(t('installed')) : `<span class="badge bad">${esc(t('missing'))}</span>`}</dd>
    </dl>${hint}
    <div class="row" style="margin-top:10px">
      ${a.upToDate ? '' : `<button class="primary" data-action="install" data-addon="${key}">${esc(t('Update addon'))}</button>`}
      ${check}
      <button class="ghost" data-action="open-addon" data-addon="${key}">${esc(t('Open folder'))}</button>
    </div></div>`;
}

export const gphAbout = () => t('Gold per hour, split into herbalism and loot. The app shows sessions, history and settings.');

function gameCard() {
  const s = state.status;
  const g = state.game || {};
  const running = g.running ? `<span class="badge ok">${esc(t('running'))}</span>`
    : g.otherClient ? `<span class="badge warn">${esc(t('another WoW client is open'))}</span>` : `<span class="badge">${esc(t('closed'))}</span>`;
  const client = s.characters?.[0]?.client;
  return `<div class="card"><h2>WoW Forever</h2>
    <h3>${running}</h3>
    <dl class="kv small">
      <dt>${esc(t('Folder'))}</dt><dd>${s.wowFound ? esc(s.wowDir) : `<span class="badge bad">${esc(t('not found'))}</span> ${esc(s.wowDir)}`}</dd>
      <dt>${esc(t('Client'))}</dt><dd>${esc(client?.version || '?')} (${esc(client?.build || '?')})${s.clientLocale ? ` · ${esc(s.clientLocale)}` : ''}</dd>
    </dl></div>`;
}

function auctionatorCard() {
  const au = state.status.auctionator;
  const title = `<h2>${esc(t('Auction house scan'))} ${help(t('Prices are the lowest buyout at the last scan, not live offers. For fresh prices run a full Auctionator scan in the game, then type <code>/reload</code>.'))}</h2>`;
  if (!au || au.error) return `<div class="card">${title}${errorBox(au?.error || t('No Auctionator data'))}</div>`;
  const days = au.scanDate ? Math.round((Date.now() - new Date(au.scanDate)) / 86400000) : null;
  const badge = days === null ? '' : days <= 1 ? `<span class="badge ok">${esc(t('fresh'))}</span>` : `<span class="badge warn">${esc(daysOld(days))}</span>`;
  return `<div class="card">${title}
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
  return `<div class="card"><h2>${esc(t('Item export'))} ${help(esc(t('Stats and tooltips of the gear in the auction house, read by the WoWMCP addon in the game. The page “AH upgrades” compares these items with your gear.')))}</h2>${body}</div>`;
}

function requestCard() {
  const a = state.addon;
  const pending = a.pendingRequest;
  const done = state.status.lastRequest;
  let stateHtml = `<p class="muted small">${esc(t('No open request.'))}</p>`;
  if (pending && (!done || done.id !== pending.id)) {
    stateHtml = `<div class="notice"><b>${esc(t('Request #{id} is waiting', { id: pending.id }))}</b> (${esc(pending.action === 'sync' ? t('snapshot') : t('item export'))})
      <ol class="steps"><li>${t('Type <code>/reload</code> in the game; the addon starts the request.')}</li>
      <li>${esc(t('Wait for the chat message from WoWMCP.'))}</li>
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
  if (!g.addon?.installed) return ['bad', t('addon not installed'), esc(t('Install the WoWMCP addon above.'))];
  if (!g.addon.ready) {
    return ['warn', t('needs WoWMCP 1.7'), t('WoWMCP {version} cannot show lists. Update the addon above, then type <code>/reload</code> in the game.',
      { version: esc(g.addon.version || '?') })];
  }
  if (!g.file) return ['', t('not sent yet'), ''];
  if (g.file.hash !== g.hash) return ['warn', t('changed since sent'), esc(t('The game still gets the lists as of {time}.', { time: when(g.file.generated) }))];
  // The game read the file if the addon says so, or if it saved (so it reloaded) after the file was written.
  const loaded = g.loaded?.hash === g.file.hash || (g.saved || 0) > (g.file.modified || 0);
  if (loaded) return ['ok', t('in the game'), t('<code>/wowmcp show</code> opens the window; it also opens with the auction house.')];
  if (state.game?.running) return ['warn', t('waiting for /reload'), t('Type <code>/reload</code> in the game to load the lists.')];
  return ['ok', t('ready for the next login'), ''];
}

function listsTable(g) {
  const rows = (g.chars || []).map((c) => {
    const lists = c.lists.map((l) => `${esc(l.title)} <span class="muted">${num(l.rows)}</span>`).join(' · ');
    const errors = Object.entries(c.errors || {});
    return `<tr>
      <td style="white-space:nowrap"><b>${esc(c.name)}</b> <span class="muted">${esc(c.level ?? '')}</span></td>
      <td>${lists || '<span class="muted">–</span>'}</td>
      <td class="num">${num(c.lists.reduce((n, l) => n + l.shop, 0))}</td>
      <td class="num">${num(c.tips)}</td>
      <td class="num">${num(c.vendor)}</td>
      <td>${errors.length ? errors.map(([k, v]) => `<span class="badge bad"${hintAttr(v)}>${esc(k)}</span>`).join(' ') : '<span class="muted">–</span>'}</td>
    </tr>`;
  }).join('');
  if (!rows) return `<p class="muted small">${esc(t('No character exported yet.'))}</p>`;
  const head = th([t('Character')],
    [t('Lists'), '', esc(t('The tabs of the window in the game, each with its number of rows.'))],
    [t('To buy'), 'num', esc(t('Rows the app suggests buying. They are also in the Auctionator shopping lists “WoWMCP: …”, one per list of the character that is logged in.'))],
    [t('Tooltips'), 'num', esc(t('Items that get a line from the app in their tooltip in the game, for example the gain of an upgrade or the profit of a recipe.'))],
    [t('Vendor hints'), 'num', esc(t('Recipes worth buying that a vendor sells. The addon names them in the chat when you open such a vendor.'))],
    [t('Errors')]);
  return `<table style="margin-top:10px"><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table>`;
}

function listsCard() {
  const g = state.ingame;
  const title = `<h2>${esc(t('Lists in the game'))} ${help(t('The WoWMCP addon shows what this app found in the game: a window with AH upgrades, writs, trade crates, recipes and consumables. A click on a row searches the auction house for the item, a shift-click puts its link into the chat or the search box. The app writes the lists into the addon folder and the game reads them on <code>/reload</code>. After a new AH scan that takes two: the first <code>/reload</code> saves the scan for the app, the second loads the new lists.'))}</h2>`;
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
    return `<div class="notice">${t('No character exported yet. Log in and type <code>/wowmcp sync</code>.')}</div>`;
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
      <td>${esc(ago(c.updated))}</td>
      <td>${c.addonVersion ? `<span class="badge ${old ? 'warn' : 'ok'}">${esc(c.addonVersion)}</span>` : '<span class="badge">1.0</span>'}</td>
      <td>${errors.length ? errors.map(([k, v]) => `<span class="badge bad"${hintAttr(v)}>${esc(k)}</span>`).join(' ') : '<span class="muted">–</span>'}</td>
    </tr>`;
  }).join('');
  const head = th([t('Character')], [t('Level/class')], [t('Gold'), 'num'], [t('Zone')],
    ['Snapshot', '', t('Age of the character data. WoW writes it only on <code>/reload</code> or logout.')], ['Addon'], [t('Errors')]);
  return `<div class="card"><table><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table></div>`;
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
  content.innerHTML = `${hint}
    <div class="grid">${gameCard()}${addonCard(a)}${addonCard(state.gphAddon, gphAbout())}${auctionatorCard()}${itemExportCard()}${listsCard()}${requestCard()}</div>
    <div class="section"><h2>${esc(t('Characters'))}</h2>${charactersTable()}</div>`;
}

// Buttons of the addon cards (also shown on the GoldPerHour page) and of the request card.
content.addEventListener('click', async (e) => {
  const button = e.target.closest('button[data-action]');
  if (!button) return;
  const action = button.dataset.action;
  if (action.startsWith('gph-')) return; // pages/goldperhour.js
  const key = button.dataset.addon === 'goldperhour' ? 'goldperhour' : 'wowmcp';
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
    state.addon = await api.addonInfo('wowmcp');
  }
  if (state.page === 'connection' || state.page === 'goldperhour') rerender();
});

// The switch of the card "Lists in the game"; the main process syncs again and reports through onGameData.
content.addEventListener('change', async (e) => {
  if (e.target.id !== 'g-auto') return;
  if (state.ingame) state.ingame.automatic = e.target.checked;
  await api.setSettings({ sendToGame: e.target.checked });
});
