// Page "AH upgrades": gear from the auction house against the equipped gear, rated by scripts/upgrades.py.
import { $, ago, api, cached, content, errorBox, esc, help, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, state, terms, th, tipAttr } from '../core.js';
import { t } from '../i18n.js';

// What the columns of the cards per slot mean; the cards repeat, so the explanation sits at the heading once.
// With the combat engine the gains are simulated DPS, with the weight scale points.
const slotTerms = (engine) => terms([
  [t('Gain'), engine ? t('Change of your DPS in the simulated fight when you swap the item in.')
    : t('Change of the gear value when you swap the item in, in points of the scale.')],
  [t('vs. item'), engine ? t('The gain relative to the DPS the replaced item contributes.')
    : t('The gain relative to the points of the item it replaces.')],
  [t('Price'), t('Lowest buyout at the last scan, not a live offer.')],
  [engine ? t('DPS/gold') : t('Points/gold'), t('Gain per gold spent.')],
], t('Auctionator cannot tell items with random stats (“of Shadow Wrath”) apart, so they are missing here.'));

export async function renderUpgrades() {
  if (!state.status) await loadStatus();
  const o = state.upgrades;
  const fightControls = o.engine === 'off' ? '' : `
      <label><span class="label">${esc(t('Fight length (s)'))} ${help(esc(t('The combat model computes DPS over a fight of this length. A longer fight makes mana, regeneration and Life Tap matter more.')))}</span>
        <input type="number" id="u-fight" min="10" max="600" step="10" value="${o.fight}"></label>
      <label>${esc(t('Enemy level'))}<select id="u-target">${selectOption('', t('from the scale (+2/+3)'), o.target)}${[0, 1, 2, 3].map((n) => selectOption(String(n), `+${n}`, o.target)).join('')}</select></label>`;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Budget (gold)'))}<input type="number" id="u-max" class="wide" min="0" step="0.5" value="${esc(o.maxPrice)}" placeholder="${esc(t('your own gold'))}"></label>
      <label><span class="label">${esc(t('Rating'))} ${help(esc(t('The combat model simulates your rotation (DPS over the fight); when it cannot run, the weight scale answers instead. The weight scale rates stats with fixed weights.')))}</span>
        <select id="u-engine">${selectOption('auto', t('Combat model (DPS)'), o.engine)}${selectOption('off', t('Weight scale'), o.engine)}</select></label>
      <label>${esc(t('Scale'))}<select id="u-profile">${selectOption('', t('automatic'), o.profile)}${selectOption('leveling', t('Leveling'), o.profile)}${selectOption('dungeon', t('Dungeon/raid'), o.profile)}</select></label>${fightControls}
      <label>${esc(t('Sorting'))}<select id="u-sort">${selectOption('gain', t('biggest gain'), o.sort)}${selectOption('price', t('points per gold'), o.sort)}</select></label>
      <label><span class="label">${esc(t('Preview (levels)'))} ${help(esc(t('Also lists items you can only equip within this many levels. They are marked “from level …” and never part of the shopping list.')))}</span>
        <input type="number" id="u-pre" min="0" max="10" value="${o.preview}"></label>
      <label>${esc(t('Suggestions per slot'))}<input type="number" id="u-top" min="1" max="10" value="${o.top}"></label>
    </div><div id="u-body">${loading(t('Comparing AH items with your gear …'))}</div>`;
  const bind = (id, field, parse) => { const el = $(id); if (el) el.addEventListener('change', (e) => { o[field] = parse(e.target.value); renderUpgrades(); }); };
  bind('#u-max', 'maxPrice', (v) => v);
  bind('#u-engine', 'engine', (v) => v);
  bind('#u-profile', 'profile', (v) => v);
  bind('#u-fight', 'fight', (v) => Math.min(600, Math.max(10, Number(v) || 60)));
  bind('#u-target', 'target', (v) => v);
  bind('#u-sort', 'sort', (v) => v);
  bind('#u-pre', 'preview', (v) => Number(v) || 0);
  bind('#u-top', 'top', (v) => Number(v) || 3);
  const key = `upgrades:${state.char}:${o.maxPrice}:${o.preview}:${o.top}:${o.profile}:${o.sort}:${o.engine}:${o.fight}:${o.target}`;
  const d = await cached(key, () => api.bridge('upgrades', {
    char: state.char, maxPrice: o.maxPrice ? Number(o.maxPrice) : 0, preview: o.preview, top: o.top,
    profile: o.profile, sort: o.sort, engine: o.engine, fightSeconds: Number(o.fight) || 60,
    targetDelta: o.target === '' ? -1 : Number(o.target),
  }));
  const body = $('#u-body');
  if (!body) return;
  if (d.error) {
    body.innerHTML = `${errorBox(d.error)}<div class="notice">${esc(t('Scan the auction house with Auctionator and close it: the addon then reads new items by itself. Or request an item export on the “Connection” page.'))}</div>`;
    return;
  }
  const eng = d.engine; // set when the combat engine rated the items (gains are DPS, not points)
  const perGoldLabel = eng ? t('DPS/gold') : t('Points/gold');
  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => c.gain)));
  const signed = (n) => `${n < 0 ? '−' : '+'}${num(Math.abs(n), 1)}`;
  const pctOf = (c) => (c.gainPct === null ? esc(t('new')) : `${c.gainPct < 0 ? '−' : '+'}${percent(Math.abs(c.gainPct))}`);
  const perGold = (c) => num(c.perGold, 1);
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
      <table><thead><tr>${th([t('Gain')], [t('vs. item'), 'num'], [t('Price'), 'num'], [perGoldLabel, 'num'], [t('Item')])}</tr></thead><tbody>${rows}</tbody></table></div>`;
  }).join('');
  const pk = d.package;
  const packageHtml = pk.items.length ? `<div class="section"><h2 class="heading">${t('Shopping list: best package for {money}', { money: money(d.budget) })}
      ${help(esc(t('Best combination across all slots that fits the budget (rings as a pair, main hand + off hand against two-hand, no items above your level). Sorted by gain per gold: buy from top to bottom and stop where it gets too expensive for you.')))}</h2><div class="card">
      <div class="row" style="gap:36px;align-items:flex-start;margin-bottom:10px">
        <div><h2>${esc(t('Gain'))}</h2><div class="big good">${signed(pk.gain)}${pk.gainPct === null ? ''
          : ` <span class="small muted">(${esc(eng ? t('+{pct} DPS', { pct: percent(pk.gainPct) }) : t('+{pct} gear value', { pct: percent(pk.gainPct) }))})</span>`}</div></div>
        <div><h2>${esc(t('Cost'))}</h2><div class="big">${money(pk.cost)}</div></div>
        <div><h2>${esc(t('Purchases'))}</h2><div class="big">${pk.items.length}</div></div><span class="spacer"></span></div>
      <table><thead><tr>${th([t('Slot')], [t('Gain'), 'num'], [t('Price'), 'num'], [perGoldLabel, 'num'], [t('Item')], [t('replaces')],
        [t('up to here'), 'num', esc(t('Cost and gain in total if you buy from the top down to this row.'))])}</tr></thead><tbody>
      ${pk.items.map((c) => `<tr><td class="muted">${esc(c.slotName)}</td>
        <td class="num"><b class="${c.gain < 0 ? '' : 'good'}">${signed(c.gain)}</b></td>
        <td class="num">${money(c.price)}</td><td class="num muted">${perGold(c)}</td>
        <td>${itemCell(c, c.compareTip)}</td><td class="small muted">${esc(c.replaces.join(' + ') || '–')}</td>
        <td class="num small">${money(c.cumCost)}<div class="muted">${signed(c.cumGain)}</div></td></tr>`).join('')}</tbody></table>
      </div></div>` : '';
  const scale = `<details class="scale"><summary>${eng
    ? `${esc(d.weights)} — ${esc(t('show which spell contributes what'))}`
    : esc(t('Scale “{name}”: 1 point = 1 shadow spell power. Show the weights and why.', { name: d.profileLabel }))}</summary>
      <table><tbody>${d.scale.map((r) => `<tr><td>${esc(r.label)}</td><td class="num">${num(r.weight)}</td>
        <td class="small muted">${esc(r.note)}</td></tr>`).join('')}</tbody></table></details>`;
  const review = d.review.length ? `<div class="section"><h2>${esc(t('With a special effect – judge for yourself'))}</h2><div class="card"><table>
      <thead><tr>${th([t('Slot')], [t('Price'), 'num'], [t('Item')], [t('Effect')])}</tr></thead><tbody>
      ${d.review.map((r) => `<tr><td class="muted">${esc(r.slot)}</td><td class="num">${money(r.price)}</td>
        <td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}"${tipAttr(r.tip)}>${esc(r.name)}</span></span> ${r.isNew ? `<span class="badge new">${esc(t('NEW'))}</span>` : ''}</td>
        <td class="small">${esc(r.effects.join(' / '))}</td></tr>`).join('')}</tbody></table></div></div>` : '';
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(t('Budget'))}</h2><div class="big">${money(d.budget)}</div></div>
      <div><h2>${esc(eng ? t('DPS now') : t('Gear value'))} ${help(esc(eng
    ? t('Your damage per second in the simulated fight with the gear you wear now. Every gain below is the change of this number.')
    : t('Everything you wear, rated as one set in points of the scale. Hit counts only up to the cap, so the value of an item depends on the rest of the gear.')))}</h2>
        <div class="big">${num(d.setValue)}</div></div>
      <div><h2>${esc(t('AH scan'))}</h2><div class="big">${esc(d.scanDate)}</div></div>
      <div><h2>${esc(t('Item export'))} ${help(esc(t('Items of the last AH scans whose stats the WoWMCP addon has read in the game. Only these can be compared.')))}</h2>
        <div class="big">${num(d.exportCount)}</div><div class="small muted">${esc(ago(d.exportUpdated))}</div></div>
      <span class="spacer"></span></div>
      ${scale}</div>
    ${packageHtml}
    ${slots ? `<h2 class="section heading">${esc(t('All slots'))} ${help(slotTerms(eng))}</h2>${slots}` : `<div class="notice section">${esc(t('No upgrades within this budget.'))}</div>`}${review}`;
}
