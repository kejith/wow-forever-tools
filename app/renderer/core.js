// Shared parts of the page: state, formatting, item tooltips, item icons and the data every page needs.
// Plain DOM, no framework; all data comes from window.wow (preload.js).
import { language, locale, t } from './i18n.js';

export const api = window.wow;

export const state = {
  page: 'connection',
  char: null,
  status: null,
  addon: null,
  gphAddon: null,
  game: null,
  ingame: null, // last sync of the lists the addon shows in the game (api.gameData())
  cache: new Map(),
  lastLoad: null,
  writs: { top: 3, ownedAtAhPrice: false },
  upgrades: { maxPrice: '', preview: 4, top: 3, profile: '', sort: 'gain', engine: 'auto', fight: 60, target: '', metric: '', kills: '' },
  recipes: { skillLine: null, minListed: 3, onlyProfit: true, onlyLearnable: false },
  consumables: { top: 5, onlyAvailable: false, allBuffs: false },
  prices: { query: '', item: null, cls: -1, sort: 'name', recent: false },
  gph: { open: null, notice: null, more: {} },
};

// What the user chose survives a restart: the open page, the character and the options of the pages.
const VIEW_KEY = 'wow-forever-tools:view';
const VIEW = ['writs', 'upgrades', 'recipes', 'consumables', 'prices'];

export function saveView() {
  try {
    const view = { page: state.page, char: state.char };
    for (const key of VIEW) view[key] = state[key];
    localStorage.setItem(VIEW_KEY, JSON.stringify(view));
  } catch { /* no storage: the choices last until the app closes */ }
}

// Takes over only values of the type the option has, so an entry of an older version cannot break a page.
export function restoreView() {
  let view;
  try { view = JSON.parse(localStorage.getItem(VIEW_KEY)); } catch { return; }
  if (!view || typeof view !== 'object') return;
  if (typeof view.page === 'string') state.page = view.page;
  if (typeof view.char === 'string') state.char = view.char;
  for (const key of VIEW) {
    for (const [option, initial] of Object.entries(state[key])) {
      const value = view[key]?.[option];
      if (initial === null ? Number.isFinite(value) : typeof value === typeof initial) state[key][option] = value;
    }
  }
}

export const $ = (sel) => document.querySelector(sel);
export const content = $('#content');

// The pages register themselves here (app.js), so one page can redraw whichever page is open.
export const pages = {};
export const rerender = () => pages[state.page].render();

// ---------- Formatting ----------

export function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

export const DASH = '<span class="muted">–</span>';

// A number in the format of the app's language; `digits` fixes the decimals.
export function num(value, digits) {
  const options = digits === undefined ? undefined : { minimumFractionDigits: digits, maximumFractionDigits: digits };
  return Number(value).toLocaleString(locale(), options);
}

export const upTo = (value, digits) => Number(value).toLocaleString(locale(), { maximumFractionDigits: digits });
export const percent = (n) => t('{n}%', { n });

export function money(copper) {
  if (copper === null || copper === undefined) return DASH;
  const g = Math.floor(copper / 10000);
  const s = Math.floor((copper % 10000) / 100);
  const c = copper % 100;
  const parts = [];
  if (g) parts.push(`${g}<span class="g">g</span>`);
  if (g || s) parts.push(`${g ? String(s).padStart(2, '0') : s}<span class="s">s</span>`);
  if (!g) parts.push(`${s ? String(c).padStart(2, '0') : c}<span class="c">c</span>`);
  return `<span class="money">${parts.join(' ')}</span>`;
}

export function ago(ts) {
  if (!ts) return t('never');
  const sec = Math.max(0, Date.now() / 1000 - ts);
  if (sec < 90) return t('just now');
  if (sec < 3600) return t('{n} min ago', { n: Math.round(sec / 60) });
  if (sec < 86400 * 2) return t('{n} h ago', { n: Math.round(sec / 3600) });
  return t('{n} days ago', { n: Math.round(sec / 86400) });
}

export function when(ts) {
  if (!ts) return '–';
  return new Date(ts * 1000).toLocaleString(locale(), { dateStyle: 'short', timeStyle: 'short' });
}

// "2026-11-14" as day and month: 11/14 in English, 14.11. in German.
export function shortDay(iso) {
  const month = iso.slice(5, 7);
  const day = iso.slice(8, 10);
  return language() === 'de' ? `${day}.${month}.` : `${month}/${day}`;
}

export const daysOld = (n) => (n === 1 ? t('1 day old') : t('{n} days old', { n }));
export const quality = (q) => `q${q ?? 1}`;
export const loading = (text) => `<div class="loading"><div class="spinner"></div>${esc(text)}</div>`;
export const errorBox = (msg) => `<div class="notice error">${esc(msg)}</div>`;
export const selectOption = (value, label, current) => `<option value="${esc(value)}"${String(value) === String(current) ? ' selected' : ''}>${esc(label)}</option>`;

// ---------- Explanations ----------
// What a number means and how it is calculated sits behind a "?" instead of in paragraphs on the page:
// help(html) renders the button, helpAttr(html) puts the same popover on any element (hintAttr for plain
// text), where a native title would be slow and unstyled. Hover or keyboard focus shows it, a click on the
// button pins it until the next click, Escape or scroll.

export const helpAttr = (html) => (html ? ` data-help="${esc(html)}"` : '');
export const hintAttr = (text) => (text ? helpAttr(esc(text).replace(/\n/g, '<br>')) : '');
export const help = (html) => (html ? `<button type="button" class="help" aria-label="${esc(t('Explanation'))}"${helpAttr(html)}>?</button>` : '');

// A list of terms with their meaning, for the "?" of a section whose tables share their columns.
export const terms = (rows, foot = '') => `<dl class="terms">${rows.map(([term, text]) => `<dt>${esc(term)}</dt><dd>${esc(text)}</dd>`).join('')}</dl>`
  + (foot ? `<p>${esc(foot)}</p>` : '');

// Header cells from [label, class, explanation] triples; the explanation is the HTML behind a "?".
export const th = (...cells) => cells.map(([label, cls, info]) => `<th${cls ? ` class="${cls}"` : ''}>${esc(label)}${info ? ` ${help(info)}` : ''}</th>`).join('');

const popBox = $('#pop');
let popAnchor = null;
let popPinned = false;

function showPop(el, pinned) {
  popAnchor = el;
  popPinned = pinned;
  popBox.innerHTML = el.dataset.help;
  popBox.classList.add('show');
  const a = el.getBoundingClientRect();
  const { width, height } = popBox.getBoundingClientRect();
  const left = Math.min(Math.max(8, a.left + a.width / 2 - 28), window.innerWidth - width - 8);
  const below = a.bottom + 8;
  const top = below + height > window.innerHeight - 8 && a.top - height - 8 >= 8 ? a.top - height - 8 : below;
  popBox.style.left = `${Math.round(left)}px`;
  popBox.style.top = `${Math.round(top)}px`;
}

function hidePop() {
  popAnchor = null;
  popPinned = false;
  popBox.classList.remove('show');
}

document.addEventListener('mouseover', (e) => {
  const el = e.target.closest?.('[data-help]');
  if (el && el !== popAnchor) showPop(el, false);
});
document.addEventListener('mouseout', (e) => {
  const el = e.target.closest?.('[data-help]');
  if (el && el === popAnchor && !popPinned && !el.contains(e.relatedTarget)) hidePop();
});
document.addEventListener('focusin', (e) => {
  if (e.target.matches?.('.help:focus-visible') && e.target !== popAnchor) showPop(e.target, false);
});
document.addEventListener('focusout', (e) => {
  if (e.target === popAnchor) hidePop();
});
document.addEventListener('click', (e) => {
  const button = e.target.closest?.('.help');
  if (!button) { hidePop(); return; }
  e.preventDefault();
  if (button === popAnchor && popPinned) hidePop();
  else showPop(button, true);
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') hidePop();
});
document.addEventListener('scroll', hidePop, true);
window.addEventListener('resize', hidePop);
window.__showHelp = async (selector) => {
  // Used by "npm run capture" to screenshot an explanation.
  const el = document.querySelector(selector);
  if (!el) return false;
  el.scrollIntoView({ block: 'center' });
  await new Promise((resolve) => setTimeout(resolve, 150)); // the scroll event would close it again
  showPop(el, true);
  return true;
};

// ---------- Item tooltips ----------
// Elements carry data-tip="<n>"; the tooltip data lives in a registry that is reset on every page render.

const QUALITY_HEX = ['9d9d9d', 'ffffff', '1eff00', '3d8fe8', 'b569f0', 'ff8000'];
let tips = [];
const tipBox = $('#wtip');

export function tipAttr(tip, compare) {
  if (!tip || !tip.name) return '';
  tips.push({ tip, compare });
  return ` data-tip="${tips.length - 1}"`;
}

export function resetTips() {
  tips = [];
  tipBox.classList.remove('show');
  hidePop();
}

// Colours for exports from addon 1.1 (text only, in the client's language); 1.2 sends the game's own colour
// per line.
function guessColor(line) {
  if (/^(Equip|Use|Chance on hit|Anlegen|Benutzen|Trefferchance):/.test(line)) return '1eff00';
  if (/^".*"$/.test(line)) return 'ffd100';
  if (/Pattern|Vorlage/.test(line)) return '8080ff';
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
    ${tip.ilvl ? `<div class="wtip-ilvl">${esc(t('Item Level {n}', { n: tip.ilvl }))}</div>` : ''}
    ${lines}
    ${tip.rebuilt ? `<div class="wtip-foot">${esc(t('Rebuilt from the stats. After the next /reload the original tooltip is shown.'))}</div>` : ''}
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
  tipBox.innerHTML = tipHtml(entry.tip) + (entry.compare ? tipHtml(entry.compare, t('Currently equipped')) : '');
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

export function itemIcon(id, q, size = '') {
  if (!id) return `<span class="icon empty ${size}"></span>`;
  const known = iconOf.has(Number(id)) ? (iconOf.get(Number(id)) ?? iconFallback) : null;
  return `<span class="icon ${quality(q)} ${size}" data-icon="${Number(id)}"${known ? ` style="background-image:${iconUrl(known)}"` : ''}></span>`;
}

let hydrating = Promise.resolve();
export function hydrateIcons() {
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
  if (popAnchor && !popAnchor.isConnected) hidePop(); // the page was redrawn under the explanation
  clearTimeout(hydrateTimer);
  hydrateTimer = setTimeout(hydrateIcons, 30);
}).observe(content, { childList: true, subtree: true });

// ---------- Data ----------

export async function cached(key, fn) {
  if (!state.cache.has(key)) state.cache.set(key, fn());
  const result = await state.cache.get(key);
  if (result && result.error) state.cache.delete(key); // retry next time
  return result;
}

export async function loadStatus() {
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

export function updateLiveState() {
  $('#nav-dot').className = `dot ${connectionHealth()}`;
  const time = state.lastLoad ? new Date(state.lastLoad).toLocaleTimeString(locale(), { timeStyle: 'short' }) : '–';
  const game = state.game?.running ? t('WoW Forever is running') : t('WoW Forever is closed');
  $('#live-state').innerHTML = `${esc(game)}<br>${esc(t('Live sync on · {time}', { time }))} ${help(
    t('The app watches the SavedVariables of the game and reloads as soon as WoW writes them: on <code>/reload</code> or logout. The time is the last load.'))}`;
}
