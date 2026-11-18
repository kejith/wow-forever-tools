// Page "Consumables": the strongest potions, food, drinks, bandages and buffs the selected character can use
// at its level, with AH or vendor price, bags and own recipes; calculated by scripts/consumables.py. The
// filters work on the loaded data, the number per category is a bridge option.
import { $, api, cached, content, errorBox, esc, itemIcon, loadStatus, loading, money, quality, state } from '../core.js';
import { t } from '../i18n.js';

function priceCell(r) {
  if (!r.price) return `<td class="num muted">–</td>`;
  const source = r.source === 'vendor'
    ? (r.vendorNpc ? t('vendor {npc}', { npc: r.vendorNpc }) : t('vendor'))
    : t('AH, {n} listed', { n: r.listed });
  // Both prices when the vendor is cheaper than the AH or the other way round.
  const other = r.source === 'vendor' && r.ahPrice ? ` · AH ${money(r.ahPrice)}`
    : r.source === 'ah' && r.vendorPrice ? ` · ${esc(t('vendor'))} ${money(r.vendorPrice)}` : '';
  return `<td class="num">${money(r.price)}<div class="small muted">${esc(source)}${other}</div></td>`;
}

function itemCell(r) {
  const badges = [
    r.best ? `<span class="badge new">${esc(t('best now'))}</span>` : '',
    r.isNew ? `<span class="badge">${esc(t('NEW in Forever'))}</span>` : '',
    r.craft?.learned ? `<span class="badge ok">${esc(t('you can craft it'))}</span>` : '',
  ].join(' ');
  const details = [r.effect, r.duration, r.type].filter(Boolean).join(' · ');
  const from = [
    r.craft && !r.craft.learned ? t('made with {profession}', { profession: r.craft.profession }) : '',
    r.reqSkill ? t('needs {skill} {rank}', { skill: r.reqSkill.name, rank: r.reqSkill.rank }) : '',
    r.reqLevel > 1 ? t('from level {n}', { n: r.reqLevel }) : '',
  ].filter(Boolean).join(' · ');
  const alternatives = r.alternatives.length
    ? `<div class="small muted">${esc(t('same effect: {items}', { items: r.alternatives.map((a) => a.name).join(', ') }))}</div>` : '';
  return `<td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}">${esc(r.name)}</span></span> ${badges}
      <div class="small">${esc(details)}</div>
      ${from ? `<div class="small muted">${esc(from)}</div>` : ''}
      ${r.available ? '' : `<div class="small warn-text">${esc(t('not in the AH right now'))}</div>`}${alternatives}</td>`;
}

function nextLine(n) {
  if (!n) return '';
  const when = n.reqSkill && !n.reqSkill.ok
    ? t('with {skill} {rank}', { skill: n.reqSkill.name, rank: n.reqSkill.rank })
    : t('from level {n}', { n: n.reqLevel });
  return `<div class="small muted cons-next">${esc(t('Next:'))} <span class="item">${itemIcon(n.id, n.quality, 'sm')}<span class="${quality(n.quality)}">${esc(n.name)}</span></span>
    ${esc(when)} <span class="muted">(${esc(n.effect)})</span></div>`;
}

function groupCard(g, o) {
  const rows = g.items.filter((r) => !o.onlyAvailable || r.available);
  const body = rows.length
    ? `<table><thead><tr><th>${esc(t('Item'))}</th><th class="num">${esc(t('Price'))}</th><th class="num">${esc(t('in bags'))}</th></tr></thead>
      <tbody>${rows.map((r) => `<tr class="${r.best ? 'best' : ''}">${itemCell(r)}${priceCell(r)}<td class="num">${r.owned || '<span class="muted">–</span>'}</td></tr>`).join('')}</tbody></table>`
    : `<p class="muted">${esc(t('Nothing for your level that you can get right now.'))}</p>`;
  return `<div class="card cons-card"><h3>${esc(g.label)}</h3>${body}${nextLine(g.next)}</div>`;
}

export async function renderConsumables() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = `<div class="notice">${esc(t('No character exported.'))}</div>`; return; }
  const o = state.consumables;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Items per category'))}<input type="number" id="c-top" min="1" max="20" value="${o.top}"></label>
      <label class="check"><input type="checkbox" id="c-available" ${o.onlyAvailable ? 'checked' : ''}> ${esc(t('only what you can get now'))}</label>
      <label class="check"><input type="checkbox" id="c-all" ${o.allBuffs ? 'checked' : ''}> ${esc(t('also buffs your class does not need'))}</label>
    </div><div id="c-body">${loading(t('Looking for consumables for your level …'))}</div>`;
  $('#c-top').addEventListener('change', (e) => { o.top = Math.min(20, Math.max(1, Number(e.target.value) || 5)); renderConsumables(); });
  $('#c-available').addEventListener('change', (e) => { o.onlyAvailable = e.target.checked; renderConsumables(); });
  $('#c-all').addEventListener('change', (e) => { o.allBuffs = e.target.checked; renderConsumables(); });
  const d = await cached(`consumables:${state.char}:${o.top}`, () => api.bridge('consumables', { char: state.char, top: o.top }));
  const body = $('#c-body');
  if (!body) return;
  if (d.error) { body.innerHTML = errorBox(d.error); return; }
  const buffs = d.buffs.filter((g) => o.allBuffs || g.relevant);
  const hidden = d.buffs.length - buffs.length;
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(t('Character'))}</h2><div class="big">${esc(d.name || d.character)}</div><div class="small muted">${esc(d.class || '')}</div></div>
      <div><h2>${esc(t('Level'))}</h2><div class="big">${d.level}</div></div>
      <div><h2>${esc(t('AH scan'))}</h2><div class="big">${esc(d.scanDate || '–')}</div></div>
      <span class="spacer"></span></div>
      <p class="small muted">${esc(t('Strongest first. “Best now” = the strongest item you can use at your level and get right now: in your bags, in the AH, at a vendor or with one of your recipes. Prices = lowest buyout at the last scan or the vendor price. “Next” = the next stronger item at a higher level. Whether two elixirs stack is not checked.'))}</p>
      ${d.recipesScanned ? '' : `<p class="small muted">${esc(t('Open your profession windows once in the game and type /reload, then the page also knows what you can craft.'))}</p>`}</div>
    <div class="section"><h2>${esc(t('Restoring health and mana'))}</h2><div class="cons-grid">${d.categories.map((g) => groupCard(g, o)).join('')}</div></div>
    <div class="section" id="c-buffs"><h2>${esc(t('Buffs'))}</h2>
      ${hidden ? `<p class="small muted">${esc(t('{n} more groups that do not help your class are hidden.', { n: hidden }))}</p>` : ''}
      <div class="cons-grid">${buffs.map((g) => groupCard(g, o)).join('')}</div></div>`;
}
