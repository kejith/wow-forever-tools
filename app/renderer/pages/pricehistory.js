// Page "Price history": every item in Auctionator's price database (scripts/pricehistory.py) with its
// prices per day. Account-wide like Auctionator's data, so the character selection does not matter here.
// One bridge call; searching, filtering and drawing happen in the window.
import { $, DASH, api, cached, content, errorBox, esc, help, helpAttr, itemIcon, loading, money, num, percent, quality, saveView, selectOption, shortDay, state, terms, th } from '../core.js';
import { locale, mark, t } from '../i18n.js';

const MAX_ROWS = 100;
const CLASSES = {
  0: mark('Consumables'), 1: mark('Containers'), 2: mark('Weapons'), 3: mark('Gems'), 4: mark('Armor'),
  5: mark('Reagents'), 6: mark('Projectiles'), 7: mark('Trade goods'), 8: mark('Item enhancements'),
  9: mark('Recipes'), 11: mark('Quivers'), 12: mark('Quest items'), 13: mark('Keys'), 15: mark('Miscellaneous'),
  16: mark('Glyphs'),
};
const OTHER = -2; // items without a known class
const SORTS = [['name', mark('name')], ['price', mark('price, highest first')], ['offers', mark('offers, most first')],
  ['rising', mark('change, rising first')], ['falling', mark('change, falling first')]];
const RANGES = [[0, mark('all')], [30, mark('30 days')], [90, mark('90 days')], [365, mark('1 year')]];

const DAY_MS = 86400000;
const isoOf = (d, day) => new Date(Date.parse(d.day0) + day * DAY_MS).toISOString().slice(0, 10);
const dateText = (iso) => new Date(`${iso}T12:00:00Z`).toLocaleDateString(locale(), { dateStyle: 'medium', timeZone: 'UTC' });
const nameOf = (it) => it.name || t('Item #{id}', { id: it.id });
const classOf = (it) => (it.cls !== null && it.cls !== undefined && CLASSES[it.cls] ? it.cls : OTHER);

// Per data set once: lowered names and the numbers the sorting needs.
const prepared = new WeakMap();
function prepare(d) {
  if (prepared.has(d)) return prepared.get(d);
  for (const it of d.items) {
    const first = it.days[0];
    const last = it.days[it.days.length - 1];
    it.lower = (it.name || '').toLocaleLowerCase(locale());
    it.lastDay = last[0];
    it.offers = last[3];
    it.change = first[1] > 0 && it.days.length > 1 ? (last[1] - first[1]) / first[1] : null;
  }
  const counts = new Map();
  for (const it of d.items) counts.set(classOf(it), (counts.get(classOf(it)) || 0) + 1);
  const p = { counts };
  prepared.set(d, p);
  return p;
}

const changeText = (change) => {
  if (change === null || change === undefined) return DASH;
  const n = Math.round(change * 100);
  if (n === 0) return `<span class="muted">±${percent(0)}</span>`;
  return `<span class="${n > 0 ? 'good' : 'loss'}">${n > 0 ? '+' : '−'}${percent(Math.abs(n))}</span>`;
};

// What the search box asks for: an item ID (also "#123" or a pasted item link), or words that must all be
// in the name. Ranked like the addon: exact name, then names starting with the text, then shorter names.
function search(d, o) {
  const text = o.query.trim();
  const id = Number((text.match(/item:(\d+)/) || text.match(/^#?(\d+)$/) || [])[1]);
  let hits = d.items;
  if (o.cls !== -1) hits = hits.filter((it) => classOf(it) === o.cls);
  if (o.recent) hits = hits.filter((it) => it.lastDay >= d.lastScanDay);
  if (id) return hits.filter((it) => it.id === id);
  const query = text.toLocaleLowerCase(locale());
  const words = query.split(/\s+/).filter(Boolean);
  if (words.length) hits = hits.filter((it) => words.every((w) => it.lower.includes(w)));
  const rank = (it) => (it.lower === query ? 0 : it.lower.startsWith(query) ? 1 : 2);
  const by = {
    name: (a, b) => (words.length ? rank(a) - rank(b) || a.lower.length - b.lower.length : 0) || (!a.name - !b.name)
      || a.lower.localeCompare(b.lower, locale()) || a.id - b.id,
    price: (a, b) => b.m - a.m,
    offers: (a, b) => (b.offers ?? -1) - (a.offers ?? -1),
    rising: (a, b) => (b.change ?? -Infinity) - (a.change ?? -Infinity),
    falling: (a, b) => (a.change ?? Infinity) - (b.change ?? Infinity),
  }[o.sort] || (() => 0);
  return [...hits].sort(by);
}

function resultsHtml(d, hits, selected) {
  if (!hits.length) return `<p class="muted small ph-none">${esc(t('No item matches.'))}</p>`;
  const rows = hits.slice(0, MAX_ROWS).map((it) => `<button type="button" class="ph-row${it.id === selected ? ' active' : ''}" data-ph-item="${it.id}">
      ${itemIcon(it.id, it.q, 'sm')}<span class="ph-name ${quality(it.q)}${it.name ? '' : ' muted'}">${esc(nameOf(it))}</span>
      <span class="ph-price">${money(it.m)}</span></button>`).join('');
  const more = hits.length > MAX_ROWS ? `<p class="muted small ph-none">${esc(t('{n} more: narrow the search.', { n: num(hits.length - MAX_ROWS) }))}</p>` : '';
  return rows + more;
}

function visibleDays(d, it) {
  const range = state.prices.range || 0;
  if (!range) return it.days;
  const cutoff = d.lastDay - range + 1;
  const sub = it.days.filter((p) => p[0] >= cutoff);
  return sub.length ? sub : it.days;
}

// Price of the item per day: a line through the lowest price, a band up to the highest minimum price of
// the day, bars for the offers along the bottom. The time axis runs to the newest day of the whole
// database, so an item that left the auction house ends early. Hover a day for its numbers.
function chart(d, it, days) {
  const W = 760, H = 240, BARS = 0.22;
  const first = days[0][0];
  const span = Math.max(d.lastDay, it.lastDay) - first + 1;
  const slot = W / span;
  const x = (day) => (day - first + 0.5) * slot;
  let lo = Math.min(...days.map((p) => p[1]));
  let hi = Math.max(...days.map((p) => p[2]));
  const pad = (hi - lo) * 0.12 || Math.max(1, hi * 0.1);
  lo = Math.max(0, lo - pad);
  hi += pad;
  const y = (v) => H - ((v - lo) / (hi - lo)) * H;
  const most = Math.max(0, ...days.map((p) => p[3] || 0));
  const width = Math.max(2, Math.min(18, slot * 0.6));
  const grid = [0, 1, 2, 3].map((i) => lo + ((hi - lo) * i) / 3);
  const parts = [];
  for (const v of grid) parts.push(`<line class="grid" x1="0" x2="${W}" y1="${y(v).toFixed(1)}" y2="${y(v).toFixed(1)}"/>`);
  for (const [day, low, high, offers] of days) {
    const cx = x(day);
    if (most && offers) {
      const h = (offers / most) * H * BARS;
      parts.push(`<rect class="offers" x="${(cx - width / 2).toFixed(1)}" y="${(H - h).toFixed(1)}" width="${width.toFixed(1)}" height="${h.toFixed(1)}"/>`);
    }
    if (high > low) {
      parts.push(`<rect class="band" x="${(cx - width / 4).toFixed(1)}" y="${y(high).toFixed(1)}" width="${(width / 2).toFixed(1)}" height="${(y(low) - y(high)).toFixed(1)}"/>`);
    }
  }
  const line = days.map((p) => `${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join(' ');
  parts.push(`<polyline class="low" points="${line}"/>`);
  // Zero-length paths with round caps: dots that stay round although the drawing stretches.
  for (const p of days) parts.push(`<path class="dot" d="M${x(p[0]).toFixed(1)} ${y(p[1]).toFixed(1)}h0.01"/>`);
  for (const [day, low, high, offers] of days) {
    const tip = `<b>${esc(dateText(isoOf(d, day)))}</b><dl class="terms"><dt>${esc(t('Lowest price'))}</dt><dd>${money(low)}</dd>
      ${high > low ? `<dt>${esc(t('Highest minimum price'))}</dt><dd>${money(high)}</dd>` : ''}
      ${offers !== null ? `<dt>${esc(t('Offers'))}</dt><dd>${num(offers)}</dd>` : ''}</dl>`;
    parts.push(`<rect class="hit" x="${(x(day) - slot / 2).toFixed(1)}" y="0" width="${slot.toFixed(1)}" height="${H}"${helpAttr(tip)}/>`);
  }
  const yLabels = grid.map((v) => `<span style="top:${((y(v) / H) * 100).toFixed(1)}%">${money(Math.round(v))}</span>`).join('');
  const lastDay = first + span - 1;
  const middle = Math.floor((first + lastDay) / 2);
  const xDays = [first, ...(lastDay - first >= 4 ? [middle] : []), ...(lastDay > first ? [lastDay] : [])];
  const xLabels = xDays.map((day) => `<span style="left:${((x(day) / W) * 100).toFixed(1)}%">${esc(shortDay(isoOf(d, day)))}</span>`).join('');
  return `<div class="ph-chart"><div class="ph-y small">${yLabels}</div>
      <svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" role="img" aria-label="${esc(t('Price history of {name}', { name: nameOf(it) }))}">${parts.join('')}</svg>
      <div class="ph-x small muted">${xLabels}</div></div>
    <div class="legend small"><span class="key ph-low"></span>${esc(t('lowest price of the day'))}
      <span class="key ph-band"></span>${esc(t('up to the highest minimum price'))} <span class="key ph-offers"></span>${esc(t('offers'))}</div>`;
}

function itemHtml(d, it) {
  if (!it) return `<div class="card"><p class="muted">${esc(t('Pick an item on the left.'))}</p></div>`;
  const days = visibleDays(d, it);
  const lows = days.map((p) => p[1]);
  const first = days[0];
  const last = days[days.length - 1];
  const rangeChange = first[1] > 0 && days.length > 1 ? (last[1] - first[1]) / first[1] : null;
  const average = Math.round(lows.reduce((sum, v) => sum + v, 0) / lows.length);
  const gone = it.lastDay < d.lastScanDay;
  const stat = (label, value, sub = '') => `<div><h2>${esc(label)}</h2><div class="big">${value}</div>${sub ? `<div class="small muted">${sub}</div>` : ''}</div>`;
  const rows = [...days].reverse().map(([day, low, high, offers]) => `<tr><td>${esc(dateText(isoOf(d, day)))}</td>
      <td class="num">${money(low)}</td><td class="num">${high > low ? money(high) : DASH}</td><td class="num">${offers !== null ? num(offers) : DASH}</td></tr>`).join('');
  return `<div class="card">
      <div class="ph-head">${itemIcon(it.id, it.q)}<div><h3 class="${quality(it.q)}">${esc(nameOf(it))}</h3>
        <div class="small muted">${esc(t('Item ID {id}', { id: it.id }))}${gone ? ` · <span class="warn-text">${esc(t('not in the last scan'))}</span>` : ''}</div></div></div>
      <div class="ph-stats">
        ${stat(t('Last price'), money(it.m), esc(t('seen on {date}', { date: dateText(isoOf(d, it.lastDay)) })))}
        ${stat(t('Lowest'), money(Math.min(...lows)))}
        ${stat(t('Highest'), money(Math.max(...lows)))}
        ${stat(t('Average'), money(average))}
        ${stat(t('Change'), changeText(rangeChange), esc(t('{n} days with prices', { n: days.length })))}
        ${stat(t('Offers'), last[3] !== null ? num(last[3]) : DASH, esc(t('on the last day')))}
      </div>
      ${chart(d, it, days)}</div>
    <div class="section"><h2>${esc(t('Days'))}</h2><div class="card"><table class="compact"><thead><tr>${th([t('Day')], [t('Lowest price'), 'num'],
      [t('Highest minimum price'), 'num', esc(t('Auctionator notes the lowest and the highest minimum price of a day when it sees the item more than once that day.'))],
      [t('Offers'), 'num', esc(t('The most offers Auctionator saw that day.'))])}</tr></thead><tbody>${rows}</tbody></table></div></div>`;
}

function about(d) {
  return terms([
    [t('Price'), t('The lowest buyout Auctionator saw, per day. Lowest, highest and average are over the lowest price of each day.')],
    [t('Change'), t('From the first to the last day with a price.')],
    [t('Last scan'), t('The last day on which Auctionator saw at least 30% as many items as on its biggest day. Days with fewer come from single searches.')],
    [t('Days'), t('Auctionator keeps a number of days (21 unless set otherwise in its options) and also notes prices from single searches.')],
    [t('Without a name'), t('Items the game data and the addon do not name yet. Search them by item ID; the next item export (closing the auction house) usually brings their names.')],
  ], d.unnamed ? t('{n} items have no name yet.', { n: num(d.unnamed) }) : '');
}

let data = null; // the data set the open page shows; the search redraws only the list

function selectItem(d, id) {
  state.prices.item = id;
  saveView();
  for (const el of content.querySelectorAll('[data-ph-item]')) el.classList.toggle('active', Number(el.dataset.phItem) === id);
  $('#ph-item').innerHTML = itemHtml(d, d.items.find((it) => it.id === id));
}

function refreshResults(d, pickFirst) {
  const o = state.prices;
  const hits = search(d, o);
  if (pickFirst && hits.length && !hits.some((it) => it.id === o.item)) o.item = hits[0].id;
  $('#ph-count').textContent = t(hits.length === 1 ? mark('1 item') : mark('{n} items'), { n: num(hits.length) });
  $('#ph-results').innerHTML = resultsHtml(d, hits, o.item);
  return hits;
}

export async function renderPriceHistory() {
  if (!state.cache.has('pricehistory')) content.innerHTML = loading(t('Reading Auctionator’s price database …'));
  const d = await cached('pricehistory', () => api.bridge('pricehistory'));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  data = d;
  const p = prepare(d);
  const o = state.prices;
  if (o.cls !== -1 && !p.counts.has(o.cls)) o.cls = -1;
  const classes = [...p.counts.keys()].sort((a, b) => (a === OTHER) - (b === OTHER) || t(CLASSES[a] || '').localeCompare(t(CLASSES[b] || ''), locale()));
  content.innerHTML = `<div class="controls">
      <label><span class="label">${esc(t('Search'))} ${help(esc(t('Words that must all be in the name, or an item ID.')))}</span>
        <input type="search" id="ph-query" value="${esc(o.query)}" placeholder="${esc(t('Name or item ID'))}" autocomplete="off"></label>
      <label>${esc(t('Category'))}<select id="ph-cls">${selectOption(-1, t('all ({n})', { n: num(d.count) }), o.cls)}${classes.map((c) => selectOption(c,
        `${c === OTHER ? t('other') : t(CLASSES[c])} (${num(p.counts.get(c))})`, o.cls)).join('')}</select></label>
      <label>${esc(t('Sort by'))}<select id="ph-sort">${SORTS.map(([v, label]) => selectOption(v, t(label), o.sort)).join('')}</select></label>
      <label>${esc(t('Range'))}<select id="ph-range">${RANGES.map(([v, label]) => selectOption(v, t(label), o.range)).join('')}</select></label>
      <label class="check"><input type="checkbox" id="ph-recent" ${o.recent ? 'checked' : ''}> ${esc(t('only items of the last scan'))}</label>
      <span class="spacer"></span>
      <div class="page-meta">${d.synced && d.market ? `${esc(d.market)} · <span class="badge ok">${esc(t('synced'))}</span> · ` : ''}${esc(t('Last scan {date}', { date: dateText(d.lastScanDate) }))}${d.unnamed ? ` · ${esc(t('{n} without a name', { n: num(d.unnamed) }))}` : ''} ${help(about(d))}</div>
    </div>
    <div class="ph-layout">
      <div class="card ph-list"><div class="small muted" id="ph-count"></div><div id="ph-results"></div></div>
      <div id="ph-item"></div>
    </div>`;
  refreshResults(d, true);
  $('#ph-item').innerHTML = itemHtml(d, d.items.find((it) => it.id === o.item));
  let timer;
  $('#ph-query').addEventListener('input', (e) => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      o.query = e.target.value;
      saveView();
      const before = o.item;
      refreshResults(d, true);
      if (o.item !== before) selectItem(d, o.item);
    }, 120);
  });
  const redraw = () => { const before = o.item; refreshResults(d, true); if (o.item !== before) selectItem(d, o.item); };
  $('#ph-cls').addEventListener('change', (e) => { o.cls = Number(e.target.value); redraw(); });
  $('#ph-sort').addEventListener('change', (e) => { o.sort = e.target.value; redraw(); });
  $('#ph-range').addEventListener('change', (e) => {
    o.range = Number(e.target.value);
    saveView();
    $('#ph-item').innerHTML = itemHtml(d, d.items.find((it) => it.id === o.item));
  });
  $('#ph-recent').addEventListener('change', (e) => { o.recent = e.target.checked; redraw(); });
}

content.addEventListener('click', (e) => {
  const row = e.target.closest('[data-ph-item]');
  if (row && data && state.page === 'pricehistory') selectItem(data, Number(row.dataset.phItem));
});
