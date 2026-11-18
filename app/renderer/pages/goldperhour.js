// Page "Gold per hour": the sessions of the GoldPerHour addon. GoldPerHourDB is account-wide, so this page
// ignores the character selection. WoW writes it on /reload or logout; the app changes the addon only
// through AppRequest.lua (start/stop/reset and settings).
import { $, DASH, ago, api, cached, content, errorBox, esc, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, state, upTo, when } from '../core.js';
import { mark, t } from '../i18n.js';
import { addonCard, gphAbout } from './connection.js';

const GPH_CATS = [['herb', mark('Herbs')], ['loot', mark('Loot')]];
const ACTIONS = { start: mark('Start'), stop: mark('Stop'), reset: mark('Reset') };

const th = (...cells) => cells.map(([label, cls]) => `<th${cls ? ` class="${cls}"` : ''}>${esc(label)}</th>`).join('');

function duration(sec) {
  const s = Math.max(0, Math.round(sec || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h} h ${String(m).padStart(2, '0')} min` : `${m} min`;
}

// Gold per hour rounded like the addon: copper is noise once gold is involved.
function rate(copper) {
  if (copper === null || copper === undefined) return `<span class="muted" title="${esc(t('Only from one minute of session time on'))}">–</span>`;
  const c = Math.round(copper);
  return money(c >= 10000 ? c - (c % 100) : c);
}

// Cumulative gold of one session over session time: herbs, loot and the sum.
function gphChart(samples, { width = 640, height = 170, small = false } = {}) {
  if (!samples || samples.length < 2) {
    return `<p class="muted small">${esc(t('A curve needs at least two samples (every 5 minutes of session time, plus at start and stop).'))}</p>`;
  }
  const pad = small ? 3 : 2;
  const T = samples[samples.length - 1].t || 1;
  const hi = Math.max(1, ...samples.map((s) => s.herb + s.loot));
  const x = (time) => pad + (time / T) * (width - 2 * pad);
  const y = (v) => pad + ((hi - v) / hi) * (height - 2 * pad);
  const line = (f) => samples.map((s) => `${x(s.t).toFixed(1)},${y(f(s)).toFixed(1)}`).join(' ');
  const g = (c) => `${upTo(c / 10000, c >= 100000 ? 0 : 1)} g`;
  // Labels stay HTML: the SVG stretches to the card width and would distort text.
  const axis = small ? '' : `<line class="axis" x1="0" x2="${width}" y1="${y(0)}" y2="${y(0)}"/>
    <line class="grid" x1="0" x2="${width}" y1="${y(hi)}" y2="${y(hi)}"/>`;
  const tip = samples.map((s) => t('{time}: {total} (herbs {herbs}, loot {loot})',
    { time: duration(s.t), total: g(s.herb + s.loot), herbs: g(s.herb), loot: g(s.loot) })).join('\n');
  const svg = `<svg class="gph-chart${small ? ' small' : ''}" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="${esc(t('Gold over the session time'))}">
      <title>${esc(tip)}</title>${axis}
      <polyline class="herb" points="${line((s) => s.herb)}"/>
      <polyline class="loot" points="${line((s) => s.loot)}"/>
      <polyline class="total" points="${line((s) => s.herb + s.loot)}"/>
    </svg>`;
  if (small) return svg;
  return `<div class="chart-axis small muted"><span>${g(hi)}</span></div>${svg}
    <div class="chart-axis small muted"><span>0</span><span>${duration(T)}</span></div>
    <div class="legend small"><span class="key total"></span>${esc(t('Total'))} <span class="key herb"></span>${esc(t('Herbs'))} <span class="key loot"></span>${esc(t('Loot'))}</div>`;
}

function gphItemsTable(cat, label, limit = 0) {
  const items = limit ? cat.items.slice(0, limit) : cat.items;
  if (!items.length && !cat.coin) return `<p class="muted small">${esc(t('{label}: nothing yet.', { label }))}</p>`;
  const share = (v) => (cat.gold ? percent(Math.round((v / cat.gold) * 100)) : '');
  const rows = items.map((e) => `<tr>
      <td><span class="item">${itemIcon(e.id, e.quality, 'sm')}<span class="${quality(e.quality)}">${esc(e.name)}</span></span></td>
      <td class="num">${num(e.count)}</td><td class="num">${money(e.value)}</td><td class="num muted">${share(e.value)}</td></tr>`).join('');
  const more = cat.items.length > items.length ? `<tr><td colspan="4" class="muted small">${esc(t('+{n} more', { n: cat.items.length - items.length }))}</td></tr>` : '';
  const coin = cat.coin ? `<tr><td class="muted">${esc(t('Coins'))}</td><td></td><td class="num">${money(cat.coin)}</td><td class="num muted">${share(cat.coin)}</td></tr>` : '';
  return `<table class="compact"><thead><tr>${th([label], [t('Count'), 'num'], [t('Value'), 'num'], [t('Share'), 'num'])}</tr></thead>
    <tbody>${rows}${more}${coin}</tbody></table>`;
}

function gphRequestState(d) {
  const pending = state.gphAddon?.pendingRequest;
  const done = d.lastRequest;
  const what = (r) => [ACTIONS[r.action] && t(ACTIONS[r.action]), r.settings && t('Settings')].filter(Boolean).join(' + ') || t('Request');
  if (pending && (!done || done.id !== pending.id)) {
    return `<div class="notice small" style="margin:10px 0 0">${t('<b>{what} is waiting</b> (request #{id}). Type <code>/reload</code> in the game; the addon runs it once. The app shows the result after the next save (<code>/reload</code> again or logout).',
      { what: esc(what(pending)), id: esc(pending.id) })}
      <div class="row" style="margin-top:8px"><button class="ghost" data-action="gph-clear">${esc(t('Withdraw request'))}</button></div></div>`;
  }
  if (done) {
    const err = done.error ? ` <span class="badge bad" title="${esc(done.error)}">${esc(t('error'))}</span>` : '';
    return `<p class="small" style="margin:10px 0 0"><span class="badge ok">${esc(t('done'))}</span> ${esc(what(done))} ${when(done.done)}${err}</p>`;
  }
  return '';
}

function gphSettingsCard(d) {
  const s = d.settings;
  const disabled = state.gphAddon?.installed ? '' : 'disabled';
  const check = (id, on, label) => `<label class="check"><input type="checkbox" id="${id}" ${on ? 'checked' : ''}> ${esc(label)}</label>`;
  return `<div class="card" id="gph-settings"><h2>${esc(t('Settings in the addon'))}</h2>
    <div class="controls" style="margin-bottom:12px">
      ${check('gph-shown', s.shown, t('Show the window'))}
      ${check('gph-resume', s.resumeAfterReload, t('keeps running after /reload'))}
      ${check('gph-auto', s.autoStart, t('starts with the first loot'))}
    </div>
    <div class="controls">
      <label>${esc(t('Window scale'))}<input type="number" id="gph-scale" min="0.5" max="2" step="0.05" value="${esc(s.scale)}"></label>
      <label>${esc(t('Items in the tooltip'))}<input type="number" id="gph-tooltip" min="1" max="30" value="${esc(s.tooltipItems)}"></label>
      <label>${esc(t('Prices'))}<select id="gph-price">
        ${selectOption('auctionator', t('Auctionator, otherwise vendor'), s.priceSource === 'vendor' ? 'vendor' : 'auctionator')}
        ${selectOption('vendor', t('vendor price only'), s.priceSource === 'vendor' ? 'vendor' : 'auctionator')}</select></label>
      <label>${esc(t('More herb spells (IDs)'))}<input type="text" id="gph-herbs" value="${esc(s.herbSpells.join(', '))}" placeholder="${esc(t('e.g. 2368, 3570'))}"></label>
    </div>
    <p class="small muted">${esc(t('Herb Gathering ({id}) and spells with the same name always count. The price source applies to newly looted items; what was looted before keeps its value.', { id: d.herbGathering }))}</p>
    <div class="row"><button class="primary" data-action="gph-save" ${disabled}>${esc(t('Send to the addon'))}</button>
      ${d.legacy && d.found ? `<span class="small muted">${esc(t('Version 0.2 does not know these settings yet: update first.'))}</span>` : ''}</div></div>`;
}

function gphHistoryTable(d) {
  if (!d.history.length) {
    return `<p class="muted small">${esc(t('No finished session yet. Resetting (in the game or here) moves the session into the history, at most 100.'))}</p>`;
  }
  const best = d.summary.best;
  const [herbs, loot] = GPH_CATS.map(([, label]) => t(label));
  const rows = d.history.map((h) => {
    const id = h.started || h.archived;
    const open = state.gph.open === id;
    const zones = h.zones.slice(0, 2).map((z) => esc(z.zone)).join(', ') + (h.zones.length > 2 ? ` <span class="muted">+${h.zones.length - 2}</span>` : '');
    const row = `<tr class="clickable${open ? ' open' : ''}" data-gph-open="${esc(id)}">
      <td class="nowrap">${when(h.started || h.archived)}</td><td class="nowrap">${duration(h.elapsed)}</td>
      <td>${esc(h.chars.join(', ')) || DASH}</td><td>${zones || DASH}</td>
      <td class="num">${money(h.categories.herb.gold)}</td><td class="num">${money(h.categories.loot.gold)}</td>
      <td class="num"><b>${money(h.total)}</b></td>
      <td class="num">${rate(h.perHour)}${id === best ? ` <span class="badge new">${esc(t('best'))}</span>` : ''}</td>
      <td>${gphChart(h.samples, { width: 110, height: 26, small: true })}</td></tr>`;
    if (!open) return row;
    return `${row}<tr class="detail"><td colspan="9"><div class="gph-detail">
      <div>${gphItemsTable(h.categories.herb, herbs, 8)}</div><div>${gphItemsTable(h.categories.loot, loot, 8)}</div>
      <div>${gphChart(h.samples)}</div></div></td></tr>`;
  }).join('');
  return `<table><thead><tr>${th([t('Started')], [t('Duration')], [t('Character')], [t('Zones')], [herbs, 'num'], [loot, 'num'],
    [t('Total'), 'num'], [t('per hour'), 'num'], [t('Curve')])}</tr></thead><tbody>${rows}</tbody></table>`;
}

export async function renderGoldPerHour() {
  if (!state.status) await loadStatus();
  const a = state.gphAddon;
  // Opening a history row re-renders from the cache; no spinner then, so the page keeps its scroll position.
  if (!state.cache.has('goldperhour')) content.innerHTML = loading(t('Loading GoldPerHour …'));
  const d = await cached('goldperhour', () => api.bridge('goldperhour'));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  const top = [];
  if (!a.installed || !a.upToDate || a.lastInstall) top.push(`<div class="grid" style="margin-bottom:16px">${addonCard(a, gphAbout())}</div>`);
  if (state.gph.notice) top.push(`<div class="notice">${state.gph.notice}</div>`);
  if (!d.found) {
    content.innerHTML = `${top.join('')}<div class="notice">${esc(t('No GoldPerHour data yet.'))} ${a.installed
      ? t('Type <code>/reload</code> in the game, then <code>/gph</code> opens the window. WoW saves the values at every /reload and logout.')
      : esc(t('Install the addon first.'))}</div>${gphSettingsCard(d)}`;
    return;
  }
  const c = d.current;
  const saved = d.updated || d.modified;
  const runBadge = c.runningAtSave ? `<span class="badge ok">${esc(t('was running when saved'))}</span>`
    : c.elapsed ? `<span class="badge">${esc(t('paused'))}</span>` : `<span class="badge">${esc(t('not started'))}</span>`;
  const disabled = a.installed ? '' : 'disabled';
  const [herbs, loot] = GPH_CATS.map(([, label]) => t(label));
  const catCard = ([key, label]) => {
    const cat = c.categories[key];
    return `<div class="card"><h2>${esc(t(label))}</h2><div class="big">${money(cat.gold)}</div>
      <dl class="kv small"><dt>${esc(t('per hour'))}</dt><dd>${rate(cat.perHour)}</dd>
        <dt>${esc(t('Items'))}</dt><dd>${esc(t('{n} ({kinds} kinds)', { n: num(cat.itemCount), kinds: cat.items.length }))}</dd>
        ${cat.coin ? `<dt>${esc(t('Coins'))}</dt><dd>${money(cat.coin)}</dd>` : ''}</dl></div>`;
  };
  const sum = d.summary;
  const zoneRows = c.zones.map((z) => `<tr><td>${esc(z.zone)}</td><td class="num">${money(z.value)}</td>
    <td class="num muted">${percent(c.total ? Math.round((z.value / c.total) * 100) : 0)}</td></tr>`).join('');
  content.innerHTML = `${top.join('')}
    <p class="small muted" style="margin-top:0">${t('As of the last save {when} ({ago}). WoW writes the values only on <code>/reload</code> or logout; running sessions keep counting in the game.',
      { when: when(saved), ago: esc(ago(saved)) })}</p>
    <div class="grid">
      <div class="card"><h2>${esc(t('Current session'))}</h2><div class="big">${money(c.total)}</div>
        <h3 style="margin-top:4px">${rate(c.perHour)} <span class="muted small">${esc(t('per hour'))}</span></h3>
        <dl class="kv small"><dt>${esc(t('Duration'))}</dt><dd>${duration(c.elapsed)} ${runBadge}</dd>
          ${c.started ? `<dt>${esc(t('Started at'))}</dt><dd>${when(c.started)}</dd>` : ''}
          ${c.chars.length ? `<dt>${esc(t('Character'))}</dt><dd>${esc(c.chars.join(', '))}</dd>` : ''}</dl></div>
      ${GPH_CATS.map(catCard).join('')}
      <div class="card"><h2>${esc(t('Controls'))}</h2>
        <div class="row">
          <button class="primary" data-action="gph-start" ${disabled}>${esc(t(ACTIONS.start))}</button>
          <button class="ghost" data-action="gph-stop" ${disabled}>${esc(t(ACTIONS.stop))}</button>
          <button class="ghost" data-action="gph-reset" ${disabled}>${esc(t(ACTIONS.reset))}</button>
        </div>
        <p class="small muted" style="margin:8px 0 0">${t('Takes effect at the next <code>/reload</code>. In the game it is immediate: <code>/gph start</code>, <code>/gph stop</code>, the reset button in the window.')}</p>
        ${gphRequestState(d)}</div>
    </div>
    <div class="section"><h2>${esc(t('Gold over the session'))}</h2><div class="card">${gphChart(c.samples)}</div></div>
    <div class="section gph-two">
      <div class="card"><h2>${esc(t('Items of the session'))}</h2>${gphItemsTable(c.categories.herb, herbs)}<div style="height:12px"></div>${gphItemsTable(c.categories.loot, loot)}</div>
      <div class="card"><h2>${esc(t('Zones'))}</h2>${c.zones.length ? `<table class="compact"><thead><tr>${th([t('Zone')], [t('Gold'), 'num'], [t('Share'), 'num'])}</tr></thead><tbody>
        ${zoneRows}</tbody></table>`
        : `<p class="muted small">${esc(d.legacy ? t('The addon records zones from version 0.3 on.') : t('Nothing looted yet.'))}</p>`}</div>
    </div>
    <div class="section">${gphSettingsCard(d)}</div>
    <div class="section"><h2>${esc(t('History'))}</h2>
      ${sum.sessions ? `<p class="small muted" style="margin-top:0">${t('{n} sessions, {duration} and {money} in total, {rate} per hour on average. Click a row for items and the curve.',
        { n: sum.sessions, duration: duration(sum.elapsed), money: money(sum.total), rate: rate(sum.perHour) })}</p>` : ''}
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
  if (action === 'gph-reset' && !confirm(t('Reset the GoldPerHour session at the next /reload? It then moves into the history.'))) return;
  button.disabled = true;
  let result;
  if (action === 'gph-clear') result = await api.gphClearRequest();
  else if (action === 'gph-save') result = await api.gphRequest({ settings: gphFormSettings() });
  else result = await api.gphRequest({ action: action.slice(4) });
  if (result.error) { alert(result.error); button.disabled = false; return; }
  state.gph.notice = action === 'gph-clear' ? esc(t('Request withdrawn.'))
    : t('Sent. Type <code>/reload</code> in the game so the addon runs the request.');
  state.gphAddon = await api.addonInfo('goldperhour');
  renderGoldPerHour();
}

content.addEventListener('click', (e) => {
  const button = e.target.closest('button[data-action^="gph-"]');
  if (button) { gphAction(button.dataset.action, button); return; }
  const row = e.target.closest('[data-gph-open]');
  if (!row) return;
  const id = Number(row.dataset.gphOpen);
  state.gph.open = state.gph.open === id ? null : id;
  renderGoldPerHour();
});
