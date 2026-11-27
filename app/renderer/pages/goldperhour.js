// Page "Gold per hour": live sessions, account & character wealth history, always-on loot/zone/run
// analytics, auction house sales, cashflow balance and addon settings. GoldPerHourDB is account-wide.
import { $, DASH, ago, api, cached, content, errorBox, esc, help, hintAttr, hydrateIcons, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, setTabs, state, th, upTo, when } from '../core.js';
import { mark, t } from '../i18n.js';
import { addonCard, gphAbout } from './connection.js';

const GPH_CATS = [
  ['herb', mark('Herbs')],
  ['mining', mark('Mining')],
  ['skinning', mark('Skinning')],
  ['fishing', mark('Fishing')],
  ['loot', mark('Loot')],
  ['dungeon', mark('Dungeon')],
];
const CAT_LABEL = Object.fromEntries(GPH_CATS);
const ACTIONS = { start: mark('Start'), stop: mark('Stop'), reset: mark('Reset') };

const PERIODS = [
  ['today', mark('Today')],
  ['days7', mark('7 days')],
  ['days30', mark('30 days')],
  ['allTime', mark('All time')],
];

const WEALTH_RANGES = [
  [1, mark('24h')],
  [7, mark('7 days')],
  [30, mark('30 days')],
  [0, mark('all')],
];

const INCOME_LABELS = {
  ah: mark('Auction House sales'),
  dungeon: mark('Dungeon gold'),
  loot: mark('Mob loot'),
  quest: mark('Quests'),
  vendor: mark('Vendor sales'),
  trade: mark('Trade'),
  mail: mark('Mail'),
  other: mark('Other'),
};

const EXPENSE_LABELS = {
  repair: mark('Repairs'),
  ahBuy: mark('AH purchases'),
  ahDeposit: mark('AH deposits'),
  vendor: mark('Vendor purchases'),
  taxi: mark('Flight paths'),
  trainer: mark('Trainer'),
  mail: mark('Mail / postage'),
  trade: mark('Trade'),
  other: mark('Other'),
};

const QUALITY_OPTIONS = [
  [0, mark('Poor')],
  [1, mark('Common')],
  [2, mark('Uncommon')],
  [3, mark('Rare')],
  [4, mark('Epic')],
  [5, mark('Legendary')],
];

const CLASS_COLOR = {
  WARRIOR: '#c79c6e', PALADIN: '#f58cba', HUNTER: '#abd473', ROGUE: '#fff569',
  PRIEST: '#ffffff', DEATHKNIGHT: '#c41f3b', SHAMAN: '#0070de', MAGE: '#69ccf0',
  WARLOCK: '#9482c9', MONK: '#00ff96', DRUID: '#ff7d0a', DEMONHUNTER: '#a330c9',
  EVOKER: '#33937f',
};

function duration(sec) {
  const s = Math.max(0, Math.round(sec || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h} h ${String(m).padStart(2, '0')} min` : `${m} min`;
}

function rate(copper) {
  if (copper === null || copper === undefined) return `<span class="muted"${hintAttr(t('Only from one minute of session time on'))}>–</span>`;
  const c = Math.round(copper);
  return money(c >= 10000 ? c - (c % 100) : c);
}

function signedMoney(copper) {
  if (copper === null || copper === undefined) return DASH;
  if (copper === 0) return `<span class="muted">${money(0)}</span>`;
  const cls = copper > 0 ? 'good' : 'loss';
  const sign = copper > 0 ? '+' : '−';
  return `<span class="${cls}">${sign}${money(Math.abs(copper))}</span>`;
}

function fmtGoldAxis(c) {
  return `${upTo(c / 10000, Math.abs(c) >= 100000 ? 0 : 1)} g`;
}

function shareBar(pct, cls = '') {
  const w = Math.max(0, Math.min(100, pct || 0));
  return `<span class="gph-share"><span class="gph-share-bar ${cls}"><span style="width:${w.toFixed(1)}%"></span></span><span class="muted small">${percent(Math.round(w))}</span></span>`;
}

function periodButtons(group, active) {
  return `<div class="gph-pills">${PERIODS.map(([k, lbl]) => `<button type="button" class="pill${k === active ? ' active' : ''}" data-gph-period="${group}:${k}">${esc(t(lbl))}</button>`).join('')}</div>`;
}

// ---------- Session chart & item tables ----------

function gphChart(samples, { width = 640, height = 170, small = false } = {}) {
  if (!samples || samples.length < 2) {
    return `<p class="muted small">${esc(t('A curve needs at least two samples (every 5 minutes of session time, plus at start and stop).'))}</p>`;
  }
  const pad = small ? 3 : 2;
  const T = samples[samples.length - 1].t || 1;
  const totalOf = (s) => (s.herb || 0) + (s.loot || 0) + (s.gather || 0) + (s.dungeon || 0);
  const hi = Math.max(1, ...samples.map(totalOf));
  const x = (time) => pad + (time / T) * (width - 2 * pad);
  const y = (v) => pad + ((hi - v) / hi) * (height - 2 * pad);
  const line = (f) => samples.map((s) => `${x(s.t).toFixed(1)},${y(f(s)).toFixed(1)}`).join(' ');
  const axis = small ? '' : `<line class="axis" x1="0" x2="${width}" y1="${y(0)}" y2="${y(0)}"/>
    <line class="grid" x1="0" x2="${width}" y1="${y(hi)}" y2="${y(hi)}"/>`;
  const hasGather = samples.some((s) => (s.gather || 0) > 0);
  const hasDungeon = samples.some((s) => (s.dungeon || 0) > 0);
  const tip = samples.map((s) => t('{time}: {total} (herbs {herbs}, loot {loot})', {
    time: duration(s.t), total: fmtGoldAxis(totalOf(s)), herbs: fmtGoldAxis(s.herb || 0), loot: fmtGoldAxis(s.loot || 0),
  })).join('\n');
  const svg = `<svg class="gph-chart${small ? ' small' : ''}" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="${esc(t('Gold over the session time'))}">
      <title>${esc(tip)}</title>${axis}
      <polyline class="herb" points="${line((s) => s.herb || 0)}"/>
      ${hasGather ? `<polyline class="gather" points="${line((s) => s.gather || 0)}"/>` : ''}
      <polyline class="loot" points="${line((s) => s.loot || 0)}"/>
      ${hasDungeon ? `<polyline class="dungeon" points="${line((s) => s.dungeon || 0)}"/>` : ''}
      <polyline class="total" points="${line(totalOf)}"/>
    </svg>`;
  if (small) return svg;
  return `<div class="chart-axis small muted"><span>${fmtGoldAxis(hi)}</span></div>${svg}
    <div class="chart-axis small muted"><span>0</span><span>${duration(T)}</span></div>
    <div class="legend small"><span class="key total"></span>${esc(t('Total'))}
      <span class="key herb"></span>${esc(t('Herbs'))}
      ${hasGather ? `<span class="key gather"></span>${esc(t('Gathering'))}` : ''}
      <span class="key loot"></span>${esc(t('Loot'))}
      ${hasDungeon ? `<span class="key dungeon"></span>${esc(t('Dungeon'))}` : ''}</div>`;
}

function gphItemsTable(cat, label, limit = 0, key = '') {
  const all = !!key && !!state.gph.more[key];
  const items = limit && !all ? cat.items.slice(0, limit) : cat.items;
  if (!items.length && !cat.coin) return `<p class="muted small">${esc(t('{label}: nothing yet.', { label }))}</p>`;
  const share = (v) => (cat.gold ? percent(Math.round((v / cat.gold) * 100)) : '');
  const rows = items.map((e) => {
    const srcTag = e.source === 'vendor' ? ` <span class="badge small">${esc(t('vendor'))}</span>` : '';
    return `<tr>
      <td><span class="item">${itemIcon(e.id, e.quality, 'sm')}<span class="${quality(e.quality)}">${esc(e.name)}</span>${srcTag}</span></td>
      <td class="num">${num(e.count)}</td><td class="num">${money(e.value)}</td><td class="num muted">${share(e.value)}</td></tr>`;
  }).join('');
  const rest = cat.items.slice(items.length);
  const toggle = (text) => (key ? `<button type="button" class="link" data-gph-more="${key}">${esc(text)}</button>` : esc(text));
  const more = rest.length ? `<tr><td class="muted small">${toggle(t('+{n} more', { n: rest.length }))}</td><td></td>
      <td class="num muted">${money(rest.reduce((sum, e) => sum + e.value, 0))}</td><td class="num muted">${share(rest.reduce((sum, e) => sum + e.value, 0))}</td></tr>`
    : all && limit && cat.items.length > limit ? `<tr><td colspan="4" class="small">${toggle(t('Show fewer'))}</td></tr>` : '';
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
    const err = done.error ? ` <span class="badge bad"${hintAttr(done.error)}>${esc(t('error'))}</span>` : '';
    return `<p class="small" style="margin:10px 0 0"><span class="badge ok">${esc(t('done'))}</span> ${esc(what(done))} ${when(done.done)}${err}</p>`;
  }
  return '';
}

// ---------- Tab 1: Live Session & Session History ----------

function gphHistoryTable(d) {
  if (!d.history.length) {
    return `<p class="muted small">${esc(t('No finished session yet. Resetting (in the game or here) moves the session into the history, at most 100.'))}</p>`;
  }
  const best = d.summary.best;
  const rows = d.history.map((h) => {
    const id = h.started || h.archived;
    const open = state.gph.open === id;
    const zones = h.zones.slice(0, 2).map((z) => esc(z.zone)).join(', ') + (h.zones.length > 2 ? ` <span class="muted">+${h.zones.length - 2}</span>` : '');
    const gatherGold = (h.categories.herb?.gold || 0) + (h.categories.mining?.gold || 0)
      + (h.categories.skinning?.gold || 0) + (h.categories.fishing?.gold || 0);
    const lootGold = (h.categories.loot?.gold || 0) + (h.categories.dungeon?.gold || 0);
    const row = `<tr class="clickable${open ? ' open' : ''}" data-gph-open="${esc(id)}">
      <td class="nowrap">${when(h.started || h.archived)}</td><td class="nowrap">${duration(h.elapsed)}</td>
      <td>${esc(h.chars.join(', ')) || DASH}</td><td>${zones || DASH}</td>
      <td class="num">${money(gatherGold)}</td><td class="num">${money(lootGold)}</td>
      <td class="num"><b>${money(h.total)}</b></td>
      <td class="num">${rate(h.perHour)}${id === best ? ` <span class="badge new">${esc(t('best'))}</span>` : ''}</td>
      <td>${gphChart(h.samples, { width: 110, height: 26, small: true })}</td></tr>`;
    if (!open) return row;
    const activeCats = GPH_CATS.filter(([k]) => (h.categories[k]?.gold || 0) > 0 || (h.categories[k]?.items?.length || 0) > 0);
    const showCats = activeCats.length ? activeCats : [['herb', CAT_LABEL.herb], ['loot', CAT_LABEL.loot]];
    return `${row}<tr class="detail"><td colspan="9"><div class="gph-detail">
      <div>${showCats.map(([k, lbl]) => gphItemsTable(h.categories[k], t(lbl), 8)).join('<div style="height:10px"></div>')}</div>
      <div>${gphChart(h.samples)}</div></div></td></tr>`;
  }).join('');
  return `<table><thead><tr>${th([t('Started')], [t('Duration')], [t('Character')], [t('Zones')], [t('Gathering'), 'num'], [t('Loot'), 'num'],
    [t('Total'), 'num'], [t('per hour'), 'num'], [t('Curve')])}</tr></thead><tbody>${rows}</tbody></table>`;
}

function renderSessionTab(d, a) {
  const c = d.current;
  const runBadge = c.runningAtSave ? `<span class="badge ok">${esc(t('was running when saved'))}</span>`
    : c.elapsed ? `<span class="badge">${esc(t('paused'))}</span>` : `<span class="badge">${esc(t('not started'))}</span>`;
  const disabled = a.installed ? '' : 'disabled';
  const activeCats = GPH_CATS.filter(([k]) => (c.categories[k]?.gold || 0) > 0 || (c.categories[k]?.items?.length || 0) > 0);
  const shownCats = activeCats.length ? activeCats : [['herb', CAT_LABEL.herb], ['loot', CAT_LABEL.loot]];

  const w = d.wealth || {};
  const today = d.periods?.today || {};
  const ah7 = d.auctionHouse?.summary?.days7 || {};
  const kpiStrip = `<div class="gph-kpi-strip">
    <div class="card gph-kpi"><span class="muted small">${esc(t('Account wealth'))}</span><div class="big">${money(w.total || 0)}</div>
      <div class="small muted">${esc(t('24h'))}: ${signedMoney(w.deltas?.day)}</div></div>
    <div class="card gph-kpi"><span class="muted small">${esc(t('Today (loot / net)'))}</span><div class="big">${money(today.lootTotal || 0)}</div>
      <div class="small muted">${esc(t('Net cashflow'))}: ${signedMoney(today.net || 0)}</div></div>
    <div class="card gph-kpi"><span class="muted small">${esc(t('Auction House (7 days)'))}</span><div class="big">${money(ah7.net || 0)}</div>
      <div class="small muted">${esc(t('{n} sales', { n: num(ah7.sales || 0) }))}</div></div>
  </div>`;

  const catCard = ([key, label]) => {
    const cat = c.categories[key] || { gold: 0, coin: 0, items: [], itemCount: 0 };
    return `<div class="card"><h2>${esc(t(label))}</h2><div class="big">${money(cat.gold)}</div>
      <dl class="kv small"><dt>${esc(t('per hour'))}</dt><dd>${rate(cat.perHour)}</dd>
        <dt>${esc(t('Items'))}</dt><dd>${esc(t('{n} ({kinds} kinds)', { n: num(cat.itemCount), kinds: cat.items.length }))}</dd>
        ${cat.coin ? `<dt>${esc(t('Coins'))}</dt><dd>${money(cat.coin)}</dd>` : ''}</dl></div>`;
  };
  const sum = d.summary;
  const zoneRows = c.zones.map((z) => `<tr><td>${esc(z.zone)}</td><td class="num">${money(z.value)}</td>
    <td class="num muted">${percent(c.total ? Math.round((z.value / c.total) * 100) : 0)}</td></tr>`).join('');
  const sessionHelp = help(t('Takes effect at the next <code>/reload</code>. In the game it is immediate: <code>/gph start</code>, <code>/gph stop</code>, the reset button in the window.'));
  const sessionBtns = `<div class="row" style="flex-wrap:nowrap">
      <button class="primary" data-action="gph-start" ${disabled}>${esc(t(ACTIONS.start))}</button>
      <button class="ghost" data-action="gph-stop" ${disabled}>${esc(t(ACTIONS.stop))}</button>
      <button class="ghost" data-action="gph-reset" ${disabled}>${esc(t(ACTIONS.reset))}</button>
    </div>`;
  const emptySession = !c.elapsed && !c.total;
  const sessionBody = emptySession
    ? `<div class="card"><h2>${esc(t('Current session'))} ${sessionHelp}</h2>
        <div class="gph-empty-bar">
          <div><div class="row">${runBadge}<span class="muted small">${esc(t('No active session yet. Start one here or in the game with /gph start.'))}</span></div></div>
          <span class="spacer"></span>${sessionBtns}
        </div>${gphRequestState(d)}</div>`
    : `<div class="grid">
      <div class="card"><h2>${esc(t('Current session'))} ${sessionHelp}</h2><div class="big">${money(c.total)}</div>
        <h3 style="margin-top:4px">${rate(c.perHour)} <span class="muted small">${esc(t('per hour'))}</span></h3>
        <dl class="kv small"><dt>${esc(t('Duration'))}</dt><dd>${duration(c.elapsed)} ${runBadge}</dd>
          ${c.vendorTotal ? `<dt>${esc(t('Vendor value'))}</dt><dd>${money(c.vendorTotal)}</dd>` : ''}
          ${c.started ? `<dt>${esc(t('Started at'))}</dt><dd>${when(c.started)}</dd>` : ''}
          ${c.chars.length ? `<dt>${esc(t('Character'))}</dt><dd>${esc(c.chars.join(', '))}</dd>` : ''}</dl>
        <div style="margin-top:12px">${sessionBtns}</div>
        ${gphRequestState(d)}</div>
      ${shownCats.map(catCard).join('')}
    </div>
    <div class="section"><h2>${esc(t('Gold over the session'))}</h2><div class="card">${gphChart(c.samples)}</div></div>
    <div class="section gph-two">
      <div class="card"><h2>${esc(t('Items of the session'))}</h2>${shownCats.map(([k, lbl]) => gphItemsTable(c.categories[k], t(lbl), 10, k)).join('<div style="height:12px"></div>')}</div>
      <div class="card"><h2>${esc(t('Zones'))}</h2>${c.zones.length ? `<table class="compact"><thead><tr>${th([t('Zone')], [t('Gold'), 'num'], [t('Share'), 'num'])}</tr></thead><tbody>
        ${zoneRows}</tbody></table>`
        : `<p class="muted small">${esc(d.legacy ? t('The addon records zones from version 0.3 on.') : t('Nothing looted yet.'))}</p>`}</div>
    </div>`;

  return `${kpiStrip}${sessionBody}
    <div class="section"><h2>${esc(t('History'))}</h2>
      ${sum.sessions ? `<p class="small muted" style="margin-top:0">${t(sum.sessions === 1 ? mark('1 session, {duration} and {money} in total, {rate} per hour on average.')
        : mark('{n} sessions, {duration} and {money} in total, {rate} per hour on average.'),
      { n: sum.sessions, duration: duration(sum.elapsed), money: money(sum.total), rate: rate(sum.perHour) })}</p>` : ''}
      <div class="card">${gphHistoryTable(d)}</div></div>`;
}

// ---------- Tab 2: Wealth History ----------

function wealthChart(points, valKey, { width = 760, height = 210 } = {}) {
  if (!points || points.length < 2) {
    return `<p class="muted small">${esc(t('At least two wealth snapshots are needed to draw a curve. Play or reload to record more points.'))}</p>`;
  }
  const pad = 4;
  const tMin = points[0].t;
  const tMax = Math.max(tMin + 1, points[points.length - 1].t);
  const vals = points.map((p) => p[valKey] || 0);
  let lo = Math.min(...vals);
  let hi = Math.max(...vals);
  const spread = hi - lo;
  const margin = spread > 0 ? spread * 0.1 : Math.max(10000, hi * 0.05);
  lo = Math.max(0, lo - margin);
  hi += margin;
  const x = (ts) => pad + ((ts - tMin) / (tMax - tMin)) * (width - 2 * pad);
  const y = (v) => pad + ((hi - v) / (hi - lo)) * (height - 2 * pad);
  const pts = points.map((p) => `${x(p.t).toFixed(1)},${y(p[valKey] || 0).toFixed(1)}`).join(' ');
  const area = `${x(points[0].t).toFixed(1)},${(height - pad).toFixed(1)} ${pts} ${x(points[points.length - 1].t).toFixed(1)},${(height - pad).toFixed(1)}`;
  const mid = (lo + hi) / 2;
  const tip = points.slice(-40).map((p) => `${when(p.t)}: ${fmtGoldAxis(p[valKey] || 0)}`).join('\n');
  return `<div class="chart-axis small muted"><span>${fmtGoldAxis(hi)}</span><span>${fmtGoldAxis(mid)}</span></div>
    <svg class="gph-chart gph-wealth-chart" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="${esc(t('Wealth over time'))}">
      <title>${esc(tip)}</title>
      <line class="grid" x1="0" x2="${width}" y1="${y(hi).toFixed(1)}" y2="${y(hi).toFixed(1)}"/>
      <line class="grid" x1="0" x2="${width}" y1="${y(mid).toFixed(1)}" y2="${y(mid).toFixed(1)}"/>
      <line class="axis" x1="0" x2="${width}" y1="${y(lo).toFixed(1)}" y2="${y(lo).toFixed(1)}"/>
      <polygon class="wealth-area" points="${area}"/>
      <polyline class="total" points="${pts}"/>
    </svg>
    <div class="chart-axis small muted"><span>${when(tMin)}</span><span>${fmtGoldAxis(lo)}</span><span>${when(tMax)}</span></div>`;
}

function renderWealthTab(d) {
  const w = d.wealth || { total: 0, charsTotal: 0, accountBank: 0, transitTotal: 0, chars: [], transit: [], accountHistory: [], deltas: {} };
  const chars = w.chars || [];
  const selectedChar = chars.find((c) => c.key === state.gph.wealthChar);
  const isAccount = !selectedChar;
  const rawSeries = isAccount ? (w.accountHistory || []) : (selectedChar.history || []);
  const valKey = isAccount ? 'total' : 'money';

  let series = rawSeries;
  const rangeDays = state.gph.wealthRange;
  if (rangeDays > 0 && rawSeries.length > 0) {
    const refTs = rawSeries[rawSeries.length - 1].t;
    const cutoff = refTs - rangeDays * 86400;
    const filtered = rawSeries.filter((p) => p.t >= cutoff);
    if (filtered.length >= 2) series = filtered;
  }

  const deltas = isAccount ? (w.deltas || {}) : (selectedChar.deltas || {});
  const headingVal = isAccount ? w.total : selectedChar.money;

  const charOptions = `<option value="account"${isAccount ? ' selected' : ''}>${esc(t('Whole account'))}</option>`
    + chars.map((c) => selectOption(c.key, `${c.name}${c.realm ? ` - ${c.realm}` : ''}`, state.gph.wealthChar)).join('');
  const rangeBtns = WEALTH_RANGES.map(([days, lbl]) => `<button type="button" class="pill${days === rangeDays ? ' active' : ''}" data-gph-wrange="${days}">${esc(t(lbl))}</button>`).join('');

  const charRows = chars.map((c) => {
    const col = CLASS_COLOR[c.class] || 'inherit';
    return `<tr class="clickable" data-gph-wchar="${esc(c.key)}">
      <td><b style="color:${col}">${esc(c.name)}</b>${c.realm ? ` <span class="muted small">${esc(c.realm)}</span>` : ''}</td>
      <td class="num">${money(c.money)}</td>
      <td>${shareBar(c.share)}</td>
      <td class="num">${signedMoney(c.deltas?.day)}</td>
      <td class="num">${signedMoney(c.deltas?.week)}</td>
      <td class="muted small nowrap">${c.updated ? ago(c.updated) : DASH}</td>
    </tr>`;
  }).join('');

  const transitRows = (w.transit || []).map((tr) => `<tr>
    <td>${esc(tr.from)}</td><td>${esc(tr.to)}</td><td class="num">${money(tr.amount)}</td>
    <td class="muted small nowrap">${tr.sentAt ? when(tr.sentAt) : DASH}</td></tr>`).join('');

  return `<div class="gph-kpi-strip">
      <div class="card gph-kpi"><span class="muted small">${esc(isAccount ? t('Account wealth') : selectedChar.name)}</span>
        <div class="big">${money(headingVal || 0)}</div>
        <div class="small muted">${w.accountBank ? `${esc(t('Account bank'))}: ${money(w.accountBank)}` : esc(t('{n} characters', { n: chars.length }))}
          ${w.transitTotal ? ` · ${esc(t('In transit'))}: ${money(w.transitTotal)}` : ''}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Change (24h)'))}</span><div class="big">${signedMoney(deltas.day)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Change (7 days)'))}</span><div class="big">${signedMoney(deltas.week)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Change (30 days)'))}</span><div class="big">${signedMoney(deltas.month)}</div></div>
    </div>
    <div class="card">
      <div class="gph-bar-head">
        <h2>${esc(t('Wealth history'))}</h2>
        <div class="gph-controls">
          <select id="gph-wealth-char">${charOptions}</select>
          <div class="gph-pills">${rangeBtns}</div>
        </div>
      </div>
      ${wealthChart(series, valKey)}
    </div>
    <div class="section"><h2>${esc(t('Characters on this account'))}</h2>
      <div class="card">${chars.length ? `<table><thead><tr>${th([t('Character')], [t('Gold'), 'num'], [t('Share')], [t('24h'), 'num'], [t('7 days'), 'num'], [t('Updated')])}</tr></thead>
        <tbody>${charRows}</tbody></table>`
        : `<p class="muted small">${esc(t('No character wealth recorded yet. Log in on your characters with GoldPerHour 0.5 or newer.'))}</p>`}</div></div>
    ${(w.transit || []).length ? `<div class="section"><h2>${esc(t('Gold in transit (alt mail)'))}</h2>
      <div class="card"><table class="compact"><thead><tr>${th([t('From')], [t('To')], [t('Amount'), 'num'], [t('Sent')])}</tr></thead>
        <tbody>${transitRows}</tbody></table></div></div>` : ''}`;
}

// ---------- Tab 3: Historical Loot, Zones, Dungeon Runs & Top Drops ----------

function renderHistoryTab(d) {
  const periodKey = state.gph.range || 'days30';
  const p = d.periods?.[periodKey] || { lootTotal: 0, vendorTotal: 0, items: [], zones: [], lootByCat: {} };
  const runs = d.runs || [];
  const runStats = d.runStats || [];
  const drops = d.drops || [];

  const zoneRows = (p.zones || []).slice(0, 20).map((z) => `<tr>
    <td>${esc(z.zone)}</td>
    <td class="num">${money(z.value)}</td>
    <td class="num muted">${z.coin ? money(z.coin) : DASH}</td>
    <td class="num muted">${z.time ? duration(z.time) : DASH}</td>
    <td class="num">${rate(z.perHour)}</td>
    <td class="num muted">${p.lootTotal ? percent(Math.round((z.value / p.lootTotal) * 100)) : ''}</td></tr>`).join('');

  const itemRows = (p.items || []).slice(0, 30).map((it) => `<tr>
    <td><span class="item">${itemIcon(it.id, it.quality, 'sm')}<span class="${quality(it.quality)}">${esc(it.name)}</span></span></td>
    <td class="muted small">${esc(CAT_LABEL[it.cat] ? t(CAT_LABEL[it.cat]) : (it.cat || ''))}</td>
    <td class="num">${num(it.count)}</td>
    <td class="num">${money(it.value)}</td>
    <td class="num muted">${money(it.vendorValue || 0)}</td></tr>`).join('');

  const runStatRows = runStats.map((r) => `<tr>
    <td><b>${esc(r.instance)}</b></td>
    <td class="num">${num(r.runs)}</td>
    <td class="num">${duration(r.avgElapsed)}</td>
    <td class="num">${money(r.avgTotal)}</td>
    <td class="num">${rate(r.avgPerHour)}</td>
    <td class="num">${money(r.total)}</td></tr>`).join('');

  const recentRunRows = runs.slice(0, 20).map((r) => `<tr>
    <td class="nowrap">${when(r.started)}</td>
    <td><b>${esc(r.instance)}</b>${r.difficulty ? ` <span class="muted small">${esc(r.difficulty)}</span>` : ''}</td>
    <td class="muted small">${esc(r.char)}</td>
    <td class="num nowrap">${duration(r.elapsed)}</td>
    <td class="num">${money(r.dungeonCoin)}</td>
    <td class="num">${money(r.lootValue)}</td>
    <td class="num"><b>${money(r.total)}</b></td>
    <td class="num">${rate(r.perHour)}</td></tr>`).join('');

  const dropRows = drops.slice(0, 30).map((dr) => `<tr>
    <td class="nowrap muted small">${when(dr.t)}</td>
    <td><span class="item">${itemIcon(dr.id, dr.quality, 'sm')}<span class="${quality(dr.quality)}">${esc(dr.name)}</span></span>${dr.count > 1 ? ` <span class="muted">×${num(dr.count)}</span>` : ''}</td>
    <td class="muted small">${esc(dr.zone)}</td>
    <td class="muted small">${esc(dr.char)}</td>
    <td class="num"><b>${money(dr.value)}</b></td></tr>`).join('');

  const catSummary = GPH_CATS.filter(([k]) => (p.lootByCat?.[k] || 0) > 0)
    .map(([k, lbl]) => `<span class="badge">${esc(t(lbl))}: ${money(p.lootByCat[k])}</span>`).join(' ');

  return `<div class="gph-bar-head" style="margin-bottom:14px">
      <div><h2 style="margin:0">${esc(t('Always-on loot & farm spots'))}</h2>
        ${catSummary ? `<div style="margin-top:6px">${catSummary}</div>` : ''}</div>
      ${periodButtons('range', periodKey)}
    </div>
    <div class="gph-kpi-strip">
      <div class="card gph-kpi"><span class="muted small">${esc(t('Total loot value'))}</span><div class="big">${money(p.lootTotal || 0)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Guaranteed vendor value'))}</span><div class="big">${money(p.vendorTotal || 0)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Dungeon runs'))}</span><div class="big">${num(runs.length)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Top drops'))}</span><div class="big">${num(drops.length)}</div></div>
    </div>
    <div class="section gph-two">
      <div class="card"><h2>${esc(t('Top looted items'))}</h2>
        ${itemRows ? `<table class="compact"><thead><tr>${th([t('Item')], [t('Category')], [t('Count'), 'num'], [t('Value'), 'num'], [t('Vendor'), 'num'])}</tr></thead><tbody>${itemRows}</tbody></table>`
          : `<p class="muted small">${esc(t('No items recorded in this period yet.'))}</p>`}</div>
      <div class="card"><h2>${esc(t('Best farm zones'))}</h2>
        ${zoneRows ? `<table class="compact"><thead><tr>${th([t('Zone')], [t('Gold'), 'num'], [t('Coins'), 'num'], [t('Time'), 'num'], [t('per hour'), 'num'], [t('Share'), 'num'])}</tr></thead><tbody>${zoneRows}</tbody></table>`
          : `<p class="muted small">${esc(t('No zone data recorded in this period yet.'))}</p>`}</div>
    </div>
    <div class="section"><h2>${esc(t('Dungeon runs'))}</h2>
      <div class="card">${runStatRows ? `<h3>${esc(t('By instance'))}</h3>
        <table class="compact" style="margin-bottom:14px"><thead><tr>${th([t('Instance')], [t('Runs'), 'num'], [t('Avg. duration'), 'num'], [t('Avg. gold'), 'num'], [t('per hour'), 'num'], [t('Total'), 'num'])}</tr></thead><tbody>${runStatRows}</tbody></table>
        <h3>${esc(t('Recent runs'))}</h3>
        <table class="compact"><thead><tr>${th([t('Started')], [t('Instance')], [t('Character')], [t('Duration'), 'num'], [t('Shared gold'), 'num'], [t('Loot'), 'num'], [t('Total'), 'num'], [t('per hour'), 'num'])}</tr></thead><tbody>${recentRunRows}</tbody></table>`
        : `<p class="muted small">${esc(t('No dungeon runs recorded yet. Entering and leaving a dungeon or raid automatically tracks the run.'))}</p>`}</div></div>
    <div class="section"><h2>${esc(t('Top drops'))}</h2>
      <div class="card">${dropRows ? `<table class="compact"><thead><tr>${th([t('When')], [t('Item')], [t('Zone')], [t('Character')], [t('Value'), 'num'])}</tr></thead><tbody>${dropRows}</tbody></table>`
        : `<p class="muted small">${esc(t('No notable drops recorded yet.'))}</p>`}</div></div>`;
}

// ---------- Tab 4: Auction House ----------

function renderAhTab(d) {
  const periodKey = state.gph.ahRange || 'days30';
  const ah = d.auctionHouse || { summary: {}, pending: {}, topItems: [], sales: [] };
  const s = ah.summary?.[periodKey] || { sales: 0, gross: 0, cut: 0, deposit: 0, net: 0, buys: 0, buyTotal: 0 };
  const pending = ah.pending || { pendingMailCopper: 0, listedCopper: 0, listedCount: 0 };
  const q = (state.gph.ahQuery || '').trim().toLowerCase();

  const sales = (ah.sales || []).filter((e) => !q || (e.item || '').toLowerCase().includes(q) || (e.counterparty || '').toLowerCase().includes(q));
  const topRows = (ah.topItems || []).slice(0, 25).map((it) => `<tr>
    <td><span class="item">${it.itemID ? itemIcon(it.itemID, 1, 'sm') : ''}<span>${esc(it.item)}</span></span></td>
    <td class="num">${num(it.sales)}</td>
    <td class="num">${num(it.count)}</td>
    <td class="num">${money(it.avgUnit)}</td>
    <td class="num muted">${money(it.cut)}</td>
    <td class="num"><b>${money(it.net)}</b></td></tr>`).join('');

  const saleRows = sales.slice(0, 60).map((e) => `<tr>
    <td class="nowrap muted small">${when(e.t)}</td>
    <td><span class="item">${e.itemID ? itemIcon(e.itemID, 1, 'sm') : ''}<span>${esc(e.item || t('Auction House'))}</span></span>${e.count > 1 ? ` <span class="muted">×${num(e.count)}</span>` : ''}</td>
    <td class="muted small">${esc(e.counterparty || '') || DASH}</td>
    <td class="muted small">${esc(e.char)}</td>
    <td class="num muted">${money(e.gross || e.amount)}</td>
    <td class="num loss">${e.cut ? `−${money(e.cut)}` : DASH}</td>
    <td class="num good"><b>${money(e.amount)}</b></td></tr>`).join('');

  return `<div class="gph-bar-head" style="margin-bottom:14px">
      <h2 style="margin:0">${esc(t('Auction House earnings'))}</h2>
      ${periodButtons('ahRange', periodKey)}
    </div>
    <div class="gph-kpi-strip">
      <div class="card gph-kpi"><span class="muted small">${esc(t('Net sales'))}</span><div class="big good">${money(s.net || 0)}</div>
        <div class="small muted">${esc(t('{n} sales', { n: num(s.sales || 0) }))} · ${esc(t('Gross'))}: ${money(s.gross || 0)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('AH fees (5%)'))}</span><div class="big loss">${money(s.cut || 0)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('AH purchases'))}</span><div class="big">${money(s.buyTotal || 0)}</div>
        <div class="small muted">${esc(t('{n} purchases', { n: num(s.buys || 0) }))}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Pending / listed'))}</span><div class="big">${money(pending.pendingMailCopper || 0)}</div>
        <div class="small muted">${esc(t('Listed ({n}):', { n: num(pending.listedCount || 0) }))} ${money(pending.listedCopper || 0)}</div></div>
    </div>
    <div class="section"><h2>${esc(t('Top sold items'))}</h2>
      <div class="card">${topRows ? `<table class="compact"><thead><tr>${th([t('Item')], [t('Sales'), 'num'], [t('Count'), 'num'], [t('Avg. unit'), 'num'], [t('AH cut'), 'num'], [t('Net'), 'num'])}</tr></thead><tbody>${topRows}</tbody></table>`
        : `<p class="muted small">${esc(t('No auction house sales recorded yet. Sales are recorded when you collect the gold from your mailbox.'))}</p>`}</div></div>
    <div class="section">
      <div class="gph-bar-head" style="margin-bottom:8px">
        <h2 style="margin:0">${esc(t('Sales history'))}</h2>
        <input type="search" id="gph-ah-search" value="${esc(state.gph.ahQuery || '')}" placeholder="${esc(t('Filter item or buyer …'))}">
      </div>
      <div class="card">${saleRows ? `<table class="compact"><thead><tr>${th([t('When')], [t('Item')], [t('Buyer')], [t('Character')], [t('Gross'), 'num'], [t('AH cut'), 'num'], [t('Net'), 'num'])}</tr></thead><tbody>${saleRows}</tbody></table>`
        : `<p class="muted small">${esc(t('No matching sales found.'))}</p>`}</div></div>`;
}

// ---------- Tab 5: Cashflow & Transaction Ledger ----------

function dailyNetChart(daily) {
  const recent = (daily || []).slice(0, 30).reverse();
  if (!recent.length) return `<p class="muted small">${esc(t('No daily cashflow data recorded yet.'))}</p>`;
  const W = 720, H = 140;
  const maxAbs = Math.max(10000, ...recent.map((d) => Math.max(Math.abs(d.net || 0), d.income || 0, d.expense || 0)));
  const mid = H / 2;
  const slot = W / recent.length;
  const bw = Math.max(6, Math.min(24, slot * 0.65));
  const bars = recent.map((d, i) => {
    const net = d.net || 0;
    const h = Math.max(2, (Math.abs(net) / maxAbs) * (mid - 8));
    const x = i * slot + (slot - bw) / 2;
    const y = net >= 0 ? mid - h : mid;
    const cls = net >= 0 ? 'cf-pos' : 'cf-neg';
    return `<rect class="${cls}" x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${bw.toFixed(1)}" height="${h.toFixed(1)}"><title>${esc(d.day)}: ${net >= 0 ? '+' : '-'}${fmtGoldAxis(Math.abs(net))}</title></rect>`;
  }).join('');
  return `<div class="chart-axis small muted"><span>+${fmtGoldAxis(maxAbs)}</span><span>${esc(t('Daily net balance'))}</span></div>
    <svg class="gph-chart gph-cf-chart" viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" aria-label="${esc(t('Daily net cashflow'))}">
      <line class="axis" x1="0" x2="${W}" y1="${mid}" y2="${mid}"/>
      ${bars}
    </svg>
    <div class="chart-axis small muted"><span>${esc(recent[0].day)}</span><span>−${fmtGoldAxis(maxAbs)}</span><span>${esc(recent[recent.length - 1].day)}</span></div>`;
}

function cashflowTable(mapObj, labels, total, cls) {
  const entries = Object.entries(mapObj || {}).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]);
  if (!entries.length) return `<p class="muted small">${esc(t('Nothing recorded in this period.'))}</p>`;
  const rows = entries.map(([k, v]) => `<tr>
    <td>${esc(labels[k] ? t(labels[k]) : k)}</td>
    <td class="num">${money(v)}</td>
    <td>${shareBar(total ? (v / total) * 100 : 0, cls)}</td></tr>`).join('');
  return `<table class="compact"><thead><tr>${th([t('Source')], [t('Amount'), 'num'], [t('Share')])}</tr></thead><tbody>${rows}</tbody></table>`;
}

function renderCashflowTab(d) {
  const periodKey = state.gph.cashflowRange || 'days30';
  const p = d.periods?.[periodKey] || { income: 0, expense: 0, net: 0, incomeByKind: {}, expenseByKind: {} };
  const ledger = d.ledger || [];

  const kindLabel = (e) => {
    const map = e.dir === 'in' ? INCOME_LABELS : EXPENSE_LABELS;
    return map[e.kind] ? t(map[e.kind]) : e.kind;
  };
  const ledgerRows = ledger.slice(0, 60).map((e) => {
    const detail = [e.item && (e.count > 1 ? `${e.item} ×${e.count}` : e.item), e.counterparty, e.note].filter(Boolean).join(' · ');
    const amt = e.dir === 'in' ? signedMoney(e.amount) : signedMoney(-e.amount);
    return `<tr>
      <td class="nowrap muted small">${when(e.t)}</td>
      <td>${esc(kindLabel(e))}</td>
      <td class="muted small">${esc(detail) || DASH}</td>
      <td class="muted small">${esc(e.char)}</td>
      <td class="num">${amt}</td></tr>`;
  }).join('');

  return `<div class="gph-bar-head" style="margin-bottom:14px">
      <h2 style="margin:0">${esc(t('Income & expenses'))}</h2>
      ${periodButtons('cashflowRange', periodKey)}
    </div>
    <div class="gph-kpi-strip">
      <div class="card gph-kpi"><span class="muted small">${esc(t('Total income'))}</span><div class="big good">${money(p.income || 0)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Total expenses'))}</span><div class="big loss">${money(p.expense || 0)}</div></div>
      <div class="card gph-kpi"><span class="muted small">${esc(t('Net balance'))}</span><div class="big">${signedMoney(p.net || 0)}</div></div>
    </div>
    <div class="section gph-two-even">
      <div class="card"><h2>${esc(t('Income by source'))}</h2>${cashflowTable(p.incomeByKind, INCOME_LABELS, p.income, 'pos')}</div>
      <div class="card"><h2>${esc(t('Expenses by category'))}</h2>${cashflowTable(p.expenseByKind, EXPENSE_LABELS, p.expense, 'neg')}</div>
    </div>
    <div class="section"><div class="card">${dailyNetChart(d.daily)}</div></div>
    <div class="section"><h2>${esc(t('Recent transactions'))}</h2>
      <div class="card">${ledgerRows ? `<table class="compact"><thead><tr>${th([t('When')], [t('Category')], [t('Details')], [t('Character')], [t('Amount'), 'num'])}</tr></thead><tbody>${ledgerRows}</tbody></table>`
        : `<p class="muted small">${esc(t('No transactions recorded yet.'))}</p>`}</div></div>`;
}

// ---------- Tab 6: Settings ----------

function gphSettingsCard(d) {
  const s = d.settings;
  const disabled = state.gphAddon?.installed ? '' : 'disabled';
  const check = (id, on, label) => `<label class="check"><input type="checkbox" id="${id}" ${on ? 'checked' : ''}> ${esc(label)}</label>`;
  const dropQualOpts = QUALITY_OPTIONS.map(([q, lbl]) => selectOption(q, t(lbl), s.minDropQuality ?? 3)).join('');
  return `<div class="card" id="gph-settings"><h2>${esc(t('Settings in the addon'))}</h2>
    <div class="gph-form-checks">
      ${check('gph-shown', s.shown, t('Show the window'))}
      ${check('gph-resume', s.resumeAfterReload, t('keeps running after /reload'))}
      ${check('gph-auto', s.autoStart, t('starts with the first loot'))}
      ${check('gph-always', s.trackAlways !== false, t('always record loot, AH & cashflow in background'))}
      ${check('gph-ahcut', s.deductAhCut !== false, t('deduct 5% AH fee from loot values'))}
      ${check('gph-vfloor', s.vendorFloor !== false, t('use vendor price whenever higher than AH'))}
    </div>
    <div class="gph-form-fields">
      <label>${esc(t('Window scale'))}<input type="number" id="gph-scale" min="0.5" max="2" step="0.05" value="${esc(s.scale)}"></label>
      <label>${esc(t('Items in the tooltip'))}<input type="number" id="gph-tooltip" min="1" max="30" value="${esc(s.tooltipItems)}"></label>
      <label><span class="label">${esc(t('Prices'))} ${help(esc(t('Applies to newly looted items; what was looted before keeps its value.')))}</span><select id="gph-price">
        ${selectOption('auctionator', t('Auctionator, otherwise vendor'), s.priceSource === 'vendor' ? 'vendor' : 'auctionator')}
        ${selectOption('vendor', t('vendor price only'), s.priceSource === 'vendor' ? 'vendor' : 'auctionator')}</select></label>
      <label><span class="label">${esc(t('BoE min AH value (gold)'))} ${help(esc(t('Weapons and armor whose AH price is below this value count at vendor price instead. 0 disables this filter.')))}</span>
        <input type="number" id="gph-ahmin" min="0" step="0.5" value="${esc((s.ahMinValue || 0) / 10000)}"></label>
      <label><span class="label">${esc(t('Wealth snapshot interval (min)'))}</span>
        <input type="number" id="gph-wint" min="1" max="60" value="${esc(Math.round((s.wealthInterval || 300) / 60))}"></label>
      <label><span class="label">${esc(t('Top drop min quality'))}</span>
        <select id="gph-dropq">${dropQualOpts}</select></label>
      <label><span class="label">${esc(t('Top drop min value (gold)'))}</span>
        <input type="number" id="gph-dropv" min="0" step="1" value="${esc((s.minDropValue ?? 50000) / 10000)}"></label>
      <label><span class="label">${esc(t('More herb spells (IDs)'))} ${help(esc(t('Herb Gathering ({id}) and spells with the same name always count as herbs. Add the spell IDs of other ways to gather here.', { id: d.herbGathering })))}</span>
        <input type="text" id="gph-herbs" value="${esc((s.herbSpells || []).join(', '))}" placeholder="${esc(t('e.g. 2368, 3570'))}"></label>
      <label><span class="label">${esc(t('More mining spells (IDs)'))}</span>
        <input type="text" id="gph-mining" value="${esc((s.miningSpells || []).join(', '))}" placeholder="${esc(t('e.g. 32606'))}"></label>
    </div>
    <div class="row"><button class="primary" data-action="gph-save" ${disabled}>${esc(t('Send to the addon'))}</button>
      ${d.legacy && d.found ? `<span class="small muted">${esc(t('Version 0.2 does not know these settings yet: update first.'))}</span>` : ''}</div>
    ${gphRequestState(d)}</div>`;
}

// ---------- Page Render & Events ----------

export async function renderGoldPerHour() {
  if (!state.status) await loadStatus();
  const a = state.gphAddon;
  if (!state.cache.has('goldperhour')) content.innerHTML = loading(t('Loading GoldPerHour …'));
  const d = await cached('goldperhour', () => api.bridge('goldperhour'));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  const top = [];
  if (!a.installed || !a.upToDate || a.lastInstall) top.push(`<div class="grid" style="margin-bottom:16px">${addonCard(a, gphAbout())}</div>`);
  if (state.gph.notice) top.push(`<div class="notice">${state.gph.notice}</div>`);
  if (!d.found) {
    setTabs([], null, null);
    content.innerHTML = `${top.join('')}<div class="notice">${esc(t('No GoldPerHour data yet.'))} ${a.installed
      ? t('Type <code>/reload</code> in the game, then <code>/gph</code> opens the window. WoW saves the values at every /reload and logout.')
      : esc(t('Install the addon first.'))}</div>${gphSettingsCard(d)}`;
    return;
  }

  const tabs = [
    { key: 'session', label: t('Session') },
    { key: 'wealth', label: t('Wealth') },
    { key: 'history', label: t('History & spots') },
    { key: 'ah', label: t('Auction House') },
    { key: 'cashflow', label: t('Cashflow') },
    { key: 'settings', label: t('Settings') },
  ];
  const activeTab = tabs.some((x) => x.key === state.gph.tab) ? state.gph.tab : 'session';
  state.gph.tab = activeTab;
  setTabs(tabs, activeTab, async (key) => {
    state.gph.tab = key;
    await renderGoldPerHour();
    await hydrateIcons();
  });

  const saved = d.updated || d.modified;
  let body = '';
  if (activeTab === 'session') body = renderSessionTab(d, a);
  else if (activeTab === 'wealth') body = renderWealthTab(d);
  else if (activeTab === 'history') body = renderHistoryTab(d);
  else if (activeTab === 'ah') body = renderAhTab(d);
  else if (activeTab === 'cashflow') body = renderCashflowTab(d);
  else if (activeTab === 'settings') body = gphSettingsCard(d);

  content.innerHTML = `${top.join('')}
    <div class="page-meta">${esc(t('As of the last save {when} ({ago})', { when: when(saved), ago: ago(saved) }))}
      ${help(t('WoW writes the values only on <code>/reload</code> or logout; a running session keeps counting in the game.'))}</div>
    ${body}`;
}

function parseSpellList(text) {
  return text.split(/[\s,;]+/).filter(Boolean).map(Number).filter((n) => Number.isInteger(n) && n > 0);
}

function gphFormSettings() {
  const v = (id) => $(`#${id}`);
  return {
    shown: v('gph-shown').checked,
    resumeAfterReload: v('gph-resume').checked,
    autoStart: v('gph-auto').checked,
    trackAlways: v('gph-always').checked,
    deductAhCut: v('gph-ahcut').checked,
    vendorFloor: v('gph-vfloor').checked,
    scale: Number(v('gph-scale').value),
    tooltipItems: Number(v('gph-tooltip').value),
    priceSource: v('gph-price').value,
    ahMinValue: Math.round(Math.max(0, Number(v('gph-ahmin').value) || 0) * 10000),
    wealthInterval: Math.round(Math.max(1, Number(v('gph-wint').value) || 5) * 60),
    minDropQuality: Number(v('gph-dropq').value),
    minDropValue: Math.round(Math.max(0, Number(v('gph-dropv').value) || 0) * 10000),
    herbSpells: parseSpellList(v('gph-herbs').value),
    miningSpells: parseSpellList(v('gph-mining').value),
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
  await renderGoldPerHour();
  await hydrateIcons();
}

content.addEventListener('click', async (e) => {
  const button = e.target.closest('button[data-action^="gph-"]');
  if (button) { gphAction(button.dataset.action, button); return; }
  const more = e.target.closest('[data-gph-more]');
  if (more) { state.gph.more[more.dataset.gphMore] = !state.gph.more[more.dataset.gphMore]; await renderGoldPerHour(); await hydrateIcons(); return; }
  const periodBtn = e.target.closest('[data-gph-period]');
  if (periodBtn) {
    const [key, val] = periodBtn.dataset.gphPeriod.split(':');
    state.gph[key] = val;
    await renderGoldPerHour();
    await hydrateIcons();
    return;
  }
  const wr = e.target.closest('[data-gph-wrange]');
  if (wr) {
    state.gph.wealthRange = Number(wr.dataset.gphWrange);
    await renderGoldPerHour();
    return;
  }
  const wc = e.target.closest('[data-gph-wchar]');
  if (wc) {
    state.gph.wealthChar = state.gph.wealthChar === wc.dataset.gphWchar ? 'account' : wc.dataset.gphWchar;
    await renderGoldPerHour();
    return;
  }
  const row = e.target.closest('[data-gph-open]');
  if (!row) return;
  const id = Number(row.dataset.gphOpen);
  state.gph.open = state.gph.open === id ? null : id;
  await renderGoldPerHour();
  await hydrateIcons();
});

content.addEventListener('change', async (e) => {
  if (e.target.id === 'gph-wealth-char') {
    state.gph.wealthChar = e.target.value;
    await renderGoldPerHour();
  }
});

content.addEventListener('input', async (e) => {
  if (e.target.id === 'gph-ah-search') {
    state.gph.ahQuery = e.target.value;
    const pos = e.target.selectionStart;
    await renderGoldPerHour();
    await hydrateIcons();
    const inp = $('#gph-ah-search');
    if (inp) { inp.focus(); inp.setSelectionRange(pos, pos); }
  }
});
