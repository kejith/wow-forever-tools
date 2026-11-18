// Page "AH upgrades": gear from the auction house against the equipped gear, rated by scripts/upgrades.py.
import { $, ago, api, cached, content, errorBox, esc, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, state, tipAttr, upTo } from '../core.js';
import { t } from '../i18n.js';

const th = (...cells) => cells.map(([label, cls]) => `<th${cls ? ` class="${cls}"` : ''}>${esc(label)}</th>`).join('');

export async function renderUpgrades() {
  if (!state.status) await loadStatus();
  const o = state.upgrades;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Budget (gold, empty = your own gold)'))}<input type="number" id="u-max" min="0" step="0.5" value="${esc(o.maxPrice)}" placeholder="${esc(t('your own gold'))}"></label>
      <label>${esc(t('Scale'))}<select id="u-profile">${selectOption('', t('automatic'), o.profile)}${selectOption('leveling', t('Leveling'), o.profile)}${selectOption('dungeon', t('Dungeon/raid'), o.profile)}</select></label>
      <label>${esc(t('Sorting'))}<select id="u-sort">${selectOption('gain', t('biggest gain'), o.sort)}${selectOption('price', t('points per gold'), o.sort)}</select></label>
      <label>${esc(t('Preview (levels ahead)'))}<input type="number" id="u-pre" min="0" max="10" value="${o.preview}"></label>
      <label>${esc(t('Suggestions per slot'))}<input type="number" id="u-top" min="1" max="10" value="${o.top}"></label>
    </div><div id="u-body">${loading(t('Comparing AH items with your gear …'))}</div>`;
  const bind = (id, field, parse) => $(id).addEventListener('change', (e) => { o[field] = parse(e.target.value); renderUpgrades(); });
  bind('#u-max', 'maxPrice', (v) => v);
  bind('#u-profile', 'profile', (v) => v);
  bind('#u-sort', 'sort', (v) => v);
  bind('#u-pre', 'preview', (v) => Number(v) || 0);
  bind('#u-top', 'top', (v) => Number(v) || 3);
  const key = `upgrades:${state.char}:${o.maxPrice}:${o.preview}:${o.top}:${o.profile}:${o.sort}`;
  const d = await cached(key, () => api.bridge('upgrades', {
    char: state.char, maxPrice: o.maxPrice ? Number(o.maxPrice) : 0, preview: o.preview, top: o.top,
    profile: o.profile, sort: o.sort,
  }));
  const body = $('#u-body');
  if (!body) return;
  if (d.error) {
    body.innerHTML = `${errorBox(d.error)}<div class="notice">${esc(t('Scan the auction house with Auctionator and close it: the addon then reads new items by itself. Or request an item export on the “Connection” page.'))}</div>`;
    return;
  }
  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => c.gain)));
  const signed = (n) => `${n < 0 ? '−' : '+'}${num(Math.abs(n), 1)}`;
  const pctOf = (c) => (c.gainPct === null ? esc(t('new')) : `${c.gainPct < 0 ? '−' : '+'}${percent(Math.abs(c.gainPct))}`);
  const perGold = (c) => upTo(c.perGold, 1);
  const itemCell = (c, compare) => `<span class="item">${itemIcon(c.id, c.quality)}<span class="${quality(c.quality)}"${tipAttr(c.tip, compare)}>${esc(c.name)}</span></span>
          ${c.isNew ? `<span class="badge new">${esc(t('NEW in Forever'))}</span>` : ''}
          ${c.soon ? `<span class="badge warn">${esc(t('from level {n}', { n: c.reqLevel }))}</span>` : ''}
          ${c.twoHand ? `<span class="badge">${esc(t('Two-hand, also replaces the off hand'))}</span>` : ''}
          <div class="small muted">${esc(c.stats)}</div>
          ${c.effects.map((text) => `<div class="effects">${esc(text)}</div>`).join('')}`;
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
        <div class="small muted" style="margin-top:4px">${esc(t('now:'))}</div>
        <div class="small"><span class="item">${itemIcon(cur?.id, cur?.quality)}<span class="${quality(cur?.quality)}"${tipAttr(cur?.tip)}>${esc(cur?.name || '–')}</span></span></div>
        <div class="small muted">${esc(cur?.stats || '')}</div></div>
      <table><thead><tr>${th([t('Gain')], [t('vs. item'), 'num'], [t('Price'), 'num'], [t('Points/gold'), 'num'], [t('Item')])}</tr></thead><tbody>${rows}</tbody></table></div>`;
  }).join('');
  const pk = d.package;
  const packageHtml = pk.items.length ? `<div class="section"><h2>${t('Shopping list: best package for {money}', { money: money(d.budget) })}</h2><div class="card">
      <div class="row" style="gap:36px;align-items:flex-start;margin-bottom:10px">
        <div><h2>${esc(t('Gain'))}</h2><div class="big good">${signed(pk.gain)}${pk.gainPct === null ? ''
          : ` <span class="small muted">(${esc(t('+{pct} gear value', { pct: percent(pk.gainPct) }))})</span>`}</div></div>
        <div><h2>${esc(t('Cost'))}</h2><div class="big">${money(pk.cost)}</div></div>
        <div><h2>${esc(t('Purchases'))}</h2><div class="big">${pk.items.length}</div></div><span class="spacer"></span></div>
      <table><thead><tr>${th([t('Slot')], [t('Gain'), 'num'], [t('Price'), 'num'], [t('Points/gold'), 'num'], [t('Item')], [t('replaces')], [t('up to here'), 'num'])}</tr></thead><tbody>
      ${pk.items.map((c) => `<tr><td class="muted">${esc(c.slotName)}</td>
        <td class="num"><b class="${c.gain < 0 ? '' : 'good'}">${signed(c.gain)}</b></td>
        <td class="num">${money(c.price)}</td><td class="num muted">${perGold(c)}</td>
        <td>${itemCell(c, c.compareTip)}</td><td class="small muted">${esc(c.replaces.join(' + ') || '–')}</td>
        <td class="num small">${money(c.cumCost)}<div class="muted">${signed(c.cumGain)}</div></td></tr>`).join('')}</tbody></table>
      <p class="small muted">${esc(t('Best combination across all slots that fits the budget (rings as a pair, main hand + off hand against two-hand, no items above your level). Sorted by points per gold: buy from top to bottom and stop where it gets too expensive for you.'))}</p>
      </div></div>` : '';
  const scale = `<details class="scale"><summary>${esc(t('Scale “{name}”: 1 point = 1 shadow spell power. {weights}', { name: d.profileLabel, weights: d.weights }))}</summary>
      <table><tbody>${d.scale.map((r) => `<tr><td>${esc(r.label)}</td><td class="num">${num(r.weight)}</td>
        <td class="small muted">${esc(r.note)}</td></tr>`).join('')}</tbody></table></details>`;
  const review = d.review.length ? `<div class="section"><h2>${esc(t('With a special effect – judge for yourself'))}</h2><div class="card"><table>
      <thead><tr>${th([t('Slot')], [t('Price'), 'num'], [t('Item')], [t('Effect')])}</tr></thead><tbody>
      ${d.review.map((r) => `<tr><td class="muted">${esc(r.slot)}</td><td class="num">${money(r.price)}</td>
        <td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}"${tipAttr(r.tip)}>${esc(r.name)}</span></span> ${r.isNew ? `<span class="badge new">${esc(t('NEW'))}</span>` : ''}</td>
        <td class="small">${esc(r.effects.join(' / '))}</td></tr>`).join('')}</tbody></table></div></div>` : '';
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(t('Budget'))}</h2><div class="big">${money(d.budget)}</div></div>
      <div><h2>${esc(t('Level'))}</h2><div class="big">${d.level}</div></div>
      <div><h2>${esc(t('Gear value'))}</h2><div class="big">${num(d.setValue)}</div></div>
      <div><h2>${esc(t('AH scan'))}</h2><div class="big">${esc(d.scanDate)}</div></div>
      <div><h2>${esc(t('Item export'))}</h2><div class="big">${num(d.exportCount)}</div><div class="small muted">${esc(ago(d.exportUpdated))}</div></div>
      <span class="spacer"></span></div>
      ${scale}
      <p class="small muted">${esc(t('Gain = change of the gear value when swapping, “vs. item” = relative to the replaced item. Prices = lowest buyout at the scan. Auctionator cannot tell items with random stats (“of Shadow Wrath”) apart, so they are missing here.'))}</p></div>
    ${packageHtml}
    ${slots ? `<div class="section"><h2>${esc(t('All slots'))}</h2></div>${slots}` : `<div class="notice section">${esc(t('No upgrades within this budget.'))}</div>`}${review}`;
}
