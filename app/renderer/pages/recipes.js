// Page "Professions": profit per craft for learned recipes and which missing recipes pay more, calculated by
// scripts/recipes.py. The filters work on the loaded data: one bridge call per character.
import { $, DASH, ago, api, cached, content, daysOld, errorBox, esc, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, shortDay, state, upTo } from '../core.js';
import { mark, t } from '../i18n.js';

const DIFFICULTY = [['orange', mark('orange: certain skill point')], ['yellow', mark('yellow: usually a skill point')],
  ['green', mark('green: rarely a skill point')], ['grey', mark('grey: no skill point')]];
const TREND = { falling: ['down', mark('↘ falling')], rising: ['up', mark('↗ rising')], stable: ['flat', mark('→ stable')] };

const th = (...cells) => cells.map(([label, cls]) => `<th${cls ? ` class="${cls}"` : ''}>${esc(label)}</th>`).join('');

function signedMoney(copper) {
  if (copper === null || copper === undefined) return DASH;
  return copper < 0 ? `<span class="loss">−${money(-copper)}</span>` : `<span class="good">+${money(copper)}</span>`;
}

const goldText = (copper) => `${copper < 0 ? '−' : '+'}${num(Math.abs(copper) / 10000, 2)} g`;

// Where the cheapest price of a reagent comes from: the auction house or a vendor (with the name, if the
// addon saw one in the game).
const reagentSource = (g) => (g.source === 'vendor' ? (g.vendor ? t('vendor {npc}', { npc: g.vendor }) : t('vendor')) : 'AH');

function recipeName(r) {
  const diff = DIFFICULTY[r.difficulty];
  const dot = diff ? `<span class="skill-dot ${diff[0]}" title="${esc(t(diff[1]))}${r.yellow ? ` · ${esc(t('yellow from {yellow}, grey from {grey}', { yellow: r.yellow, grey: r.grey }))}` : ''}"></span>` : '';
  const qty = r.qty && r.qty !== 1 ? ` <span class="muted">×${upTo(r.qty, 1)}</span>` : '';
  const reagents = r.reagents.map((g) => {
    const price = g.unit === null ? t('no price') : t('{price} s each ({source})', { price: upTo(g.unit / 100, 2), source: reagentSource(g) });
    const owned = g.owned ? ` · ${t('{n} owned', { n: g.owned })}` : '';
    return `<span class="reagent" title="${esc(`${g.name}: ${price}${owned}`)}">`
      + `${itemIcon(g.id, g.quality, 'sm')}${g.count}× ${esc(g.name)}${g.source === 'vendor' ? ` <span class="muted">(${esc(t('vendor'))})</span>` : ''}</span>`;
  }).join('');
  const badges = [
    r.isNew ? `<span class="badge new">${esc(t('NEW in Forever'))}</span>` : '',
    r.cooldown ? `<span class="badge">${esc(t('cooldown {time}', { time: r.cooldown }))}</span>` : '',
    r.craftable ? `<span class="badge ok" title="${esc(t('from bags and bank'))}">${esc(t('{n}× craftable', { n: r.craftable }))}</span>` : '',
  ].join(' ');
  return `<div class="recipe">${dot}<span class="item">${itemIcon(r.output, r.quality)}<span class="${quality(r.quality)}">${esc(r.name)}</span></span>${qty} ${badges}</div>
    <div class="reagents">${reagents}</div>
    ${r.notes.length ? `<div class="small warn-text">${esc(r.notes.join(' · '))}</div>` : ''}`;
}

// Profit per craft on the last full scan days as a small line; the dashed line is zero.
function profitTrend(r) {
  const h = r.history || [];
  if (h.length < 2) return `<span class="muted" title="${esc(t('A trend needs prices from at least two full scan days.'))}">–</span>`;
  const W = 116, H = 30, pad = 4;
  const time = h.map((p) => Date.parse(p.date));
  const values = h.map((p) => p.profit);
  const lo = Math.min(0, ...values), hi = Math.max(0, ...values);
  const x = (i) => pad + ((time[i] - time[0]) / (time[h.length - 1] - time[0])) * (W - 2 * pad);
  const y = (v) => pad + ((hi - v) / (hi - lo || 1)) * (H - 2 * pad);
  const points = h.map((p, i) => `${x(i).toFixed(1)},${y(p.profit).toFixed(1)}`).join(' ');
  const [cls, label] = TREND[r.trend] || ['flat', ''];
  const tip = h.map((p) => t('{day}  {profit}  ·  price {price} g  ·  {listed} in the AH',
    { day: shortDay(p.date), profit: goldText(p.profit), price: num(p.price / 10000, 2), listed: p.listed })).join('\n');
  return `<div class="trend ${cls}" title="${esc(tip)}">
      <svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" role="img" aria-label="${esc(t('Profit trend'))}">
        <line class="zero" x1="0" x2="${W}" y1="${y(0).toFixed(1)}" y2="${y(0).toFixed(1)}"/>
        <polyline points="${points}"/>
        ${h.map((p, i) => `<circle cx="${x(i).toFixed(1)}" cy="${y(p.profit).toFixed(1)}" r="${i === h.length - 1 ? 2.6 : 1.4}"/>`).join('')}
      </svg>
      <div class="small">${esc(label && t(label))}<span class="muted"> · ${esc(t('{n} scans since {day}', { n: h.length, day: shortDay(h[0].date) }))}</span></div></div>`;
}

function marketCells(r) {
  return `<td class="num">${money(r.cost)}</td>
    <td class="num">${money(r.sell)}${r.sellSource === 'vendor' ? `<div class="small muted">${esc(t('vendor'))}</div>` : ''}</td>
    <td class="num"><b>${signedMoney(r.profit)}</b>${r.margin !== null ? `<div class="small muted">${r.margin > 0 ? '+' : ''}${percent(r.margin)}</div>` : ''}</td>
    <td>${profitTrend(r)}</td>
    <td class="num ${r.listed < 3 ? 'warn-text' : 'muted'}">${r.listed}</td>`;
}

function acquireCell(r) {
  const a = r.acquire;
  const parts = a.options.map((o) => {
    if (o.kind === 'ah') {
      return `<div><span class="item">${itemIcon(o.id, o.quality, 'sm')}AH ${money(o.price)}</span> <span class="muted small">(${esc(t('{n} listed', { n: o.listed }))}${o.age ? `, ${esc(daysOld(o.age))}` : ''})</span></div>`;
    }
    if (o.kind === 'vendor') {
      return `<div><span class="item">${itemIcon(o.id, o.quality, 'sm')}${esc(o.npc ? t('Vendor {npc}', { npc: o.npc }) : t('Vendor'))} ${money(o.price)}</span>${o.limited ? ` <span class="badge warn">${esc(t('limited'))}</span>` : ''}</div>`;
    }
    if (o.kind === 'trainer') return `<div>${esc(t('Trainer'))}</div>`;
    return `<div><span class="item">${itemIcon(o.id, o.quality, 'sm')}<span class="${quality(o.quality)}">${esc(o.name)}</span></span> <span class="muted small">${esc(t('not in the AH'))}</span></div>`;
  }).join('');
  const text = a.sourceText ? `<div class="small muted">${esc(a.sourceText.replace(/\|c\w{8}|\|r|\|n/g, ' '))}</div>` : '';
  return parts + text;
}

function learnCell(r, rank) {
  const a = r.acquire;
  if (a.learnable === true) return `<span class="badge ok">${esc(t('now'))}</span>${a.reqSkill ? `<div class="small muted">${esc(t('from {n}', { n: a.reqSkill }))}</div>` : ''}`;
  if (a.reqSkill) return `<span class="badge warn">${esc(t('from {n}', { n: a.reqSkill }))}</span><div class="small muted">${esc(t('{n} to go', { n: a.reqSkill - rank }))}</div>`;
  if (a.trainer && r.yellow) return `<span class="badge">${esc(t('at {n} at the latest', { n: r.yellow }))}</span>`;
  return '<span class="muted">?</span>';
}

export async function renderRecipes() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = `<div class="notice">${esc(t('No character exported.'))}</div>`; return; }
  const o = state.recipes;
  content.innerHTML = loading(t('Calculating recipes against the AH scan …'));
  const d = await cached(`recipes:${state.char}`, () => api.bridge('recipes', { char: state.char }));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  if (!d.professions.length) {
    content.innerHTML = `<div class="notice">${esc(t('This character has no crafting profession (gathering professions have no recipes).'))}</div>`;
    return;
  }
  if (!d.professions.some((p) => p.skillLine === o.skillLine)) o.skillLine = d.professions[0].skillLine;
  const p = d.professions.find((x) => x.skillLine === o.skillLine);

  const keep = (r) => (!o.onlyProfit || (r.profit ?? -1) > 0) && r.listed >= o.minListed;
  const learned = p.learned.filter(keep);
  const missing = p.missing.filter((r) => keep(r) && (!o.onlyLearnable || r.acquire.learnable === true));
  const scanned = !!p.scanned;

  const header = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(p.name)}</h2><div class="big">${p.rank} <span class="muted small">/ ${p.max}</span></div>
        <div class="bar" style="width:160px;margin-top:6px"><span style="width:${Math.round((p.rank / p.max) * 100)}%"></span></div></div>
      <div><h2>${esc(t('Learned'))}</h2><div class="big">${scanned ? p.learnedCount : '?'}</div><div class="small muted">${esc(scanned ? t('{n} with profit', { n: p.profitableCount }) : t('profession window not read yet'))}</div></div>
      <div><h2>${esc(t('Best recipe'))}</h2><div class="big">${scanned ? signedMoney(p.bestLearned) : '–'}</div><div class="small muted">${esc(t('per craft, with at least 3 in the AH'))}</div></div>
      <div><h2>${esc(t('Worth getting'))}</h2><div class="big">${scanned ? p.missing.filter((r) => r.better).length : '–'}</div><div class="small muted">${esc(t('more profit than your best'))}</div></div>
      <div><h2>${esc(t('AH scan'))}</h2><div class="big">${esc(d.scanDate || '–')}</div><div class="small muted">${scanned ? esc(t('recipes read {ago}', { ago: ago(p.scanned) })) : ''}</div></div>
      <span class="spacer"></span></div></div>`;

  const notice = scanned ? '' : `<div class="notice"><b>${esc(t('The app does not know yet which recipes you know.'))}</b>
      ${d.addonVersion && d.addonVersion < '1.4' ? t('The addon in the game is {version}. On the “Connection” page choose “Update addon” first, then type <code>/reload</code> in the game.', { version: esc(d.addonVersion) }) : ''}
      <ol class="steps"><li>${esc(t('Open the {profession} window once in the game (spellbook or the K key).', { profession: p.name }))}</li>
      <li>${t('Type <code>/reload</code>. The app then separates learned and missing recipes.')}</li></ol>
      ${esc(t('Until then all recipes of the profession are listed below.'))}</div>`;

  const columns = [[t('Recipe and reagents')], [t('Reagents'), 'num'], [t('Sale (−5%)'), 'num'], [t('Profit'), 'num'], [t('Trend')], [t('in the AH'), 'num']];
  const learnedTable = scanned ? `<div class="section"><h2>${esc(t('Your recipes ({shown} of {total})', { shown: learned.length, total: p.learnedCount }))}</h2><div class="card">
      ${learned.length ? `<table><thead><tr>${th(...columns)}</tr></thead>
      <tbody>${learned.map((r) => `<tr><td>${recipeName(r)}</td>${marketCells(r)}</tr>`).join('')}</tbody></table>`
        : `<p class="muted">${esc(t('No learned recipe matches the filter.'))}</p>`}</div></div>` : '';

  const missingTitle = scanned ? t('Not learned yet ({shown} of {total})', { shown: missing.length, total: p.missingCount })
    : t('All recipes ({shown} of {total})', { shown: missing.length, total: p.missingCount });
  const missingTable = `<div class="section"><h2>${esc(missingTitle)}</h2><div class="card">
      ${missing.length ? `<table><thead><tr>${th(...columns, [t('Learn')], [t('Where from')], [t('paid off after'), 'num'])}</tr></thead>
      <tbody>${missing.map((r) => `<tr class="${r.better ? 'best' : ''}"><td>${r.better ? `<span class="badge new">${esc(t('pays more'))}</span> ` : ''}${recipeName(r)}</td>${marketCells(r)}
        <td>${learnCell(r, p.rank)}</td><td class="small">${acquireCell(r)}</td>
        <td class="num small">${r.payback ? esc(t('{n} crafts', { n: Math.ceil(r.payback) })) : DASH}</td></tr>`).join('')}</tbody></table>`
        : `<p class="muted">${esc(t('No recipe matches the filter.'))}</p>`}</div></div>`;

  content.innerHTML = `<div class="controls">
      <label>${esc(t('Profession'))}<select id="r-prof">${d.professions.map((x) => selectOption(x.skillLine, `${x.name} ${x.rank}/${x.max}`, o.skillLine)).join('')}</select></label>
      <label>${esc(t('At least in the AH'))}<input type="number" id="r-listed" min="0" max="1000" value="${o.minListed}"></label>
      <label class="check"><input type="checkbox" id="r-profit" ${o.onlyProfit ? 'checked' : ''}> ${esc(t('only with profit'))}</label>
      <label class="check"><input type="checkbox" id="r-learnable" ${o.onlyLearnable ? 'checked' : ''}> ${esc(t('only learnable now'))}</label>
    </div>
    ${notice}${header}${learnedTable}${missingTable}
    <p class="small muted">${esc(t('Profit per craft = lowest buyout of the result at the scan × amount, minus the 5% AH fee, minus the reagents at the cheaper price from the AH or a vendor (your own reagents count at the market price). “In the AH” = offers of the result at the last scan: with few offers the price is uncertain. “Trend” = the same profit on the last full scan days (price of the day: the middle of the highest and lowest minimum price, the dashed line is zero). “Falling” or “rising” means the profit changed by more than 20% over the last three scan days. The tooltip has the details. The dot in front of the name is the skill colour. “Paid off after” = price of the recipe divided by the profit. The addon reads vendor prices when you talk to a vendor ({n} so far).',
      { n: d.vendorPrices }))}</p>`;
  $('#r-prof').addEventListener('change', (e) => { o.skillLine = Number(e.target.value); renderRecipes(); });
  $('#r-listed').addEventListener('change', (e) => { o.minListed = Math.max(0, Number(e.target.value) || 0); renderRecipes(); });
  $('#r-profit').addEventListener('change', (e) => { o.onlyProfit = e.target.checked; renderRecipes(); });
  $('#r-learnable').addEventListener('change', (e) => { o.onlyLearnable = e.target.checked; renderRecipes(); });
}
