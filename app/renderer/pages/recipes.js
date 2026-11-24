// Page "Professions": profit per craft for learned recipes and which missing recipes pay more, calculated by
// scripts/recipes.py. Real tabs separate learned and unlearned recipes; search and filters run in-window.
import { $, DASH, ago, api, cached, clearTabs, content, daysOld, errorBox, esc, help, helpAttr, hintAttr, itemIcon, loadStatus, loading, money, num, percent, quality, saveView, selectOption, setTabs, shortDay, state, th, upTo } from '../core.js';
import { locale, mark, t } from '../i18n.js';

const DIFFICULTY = [['orange', mark('orange: certain skill point')], ['yellow', mark('yellow: usually a skill point')],
  ['green', mark('green: rarely a skill point')], ['grey', mark('grey: no skill point')]];
const TREND = { falling: ['down', mark('↘ falling')], rising: ['up', mark('↗ rising')], stable: ['flat', mark('→ stable')] };

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
  const dot = diff ? `<span class="skill-dot ${diff[0]}"${hintAttr(t(diff[1]) + (r.yellow ? `\n${t('yellow from {yellow}, grey from {grey}', { yellow: r.yellow, grey: r.grey })}` : ''))}></span>` : '';
  const qty = r.qty && r.qty !== 1 ? ` <span class="muted">×${upTo(r.qty, 1)}</span>` : '';
  const reagents = r.reagents.map((g) => {
    const price = g.unit === null ? t('no price') : t('{price} s each ({source})', { price: upTo(g.unit / 100, 2), source: reagentSource(g) });
    const owned = g.owned ? ` · ${t('{n} owned', { n: g.owned })}` : '';
    return `<span class="reagent"${hintAttr(`${g.name}: ${price}${owned}`)}>`
      + `${itemIcon(g.id, g.quality, 'sm')}${g.count}× ${esc(g.name)}${g.source === 'vendor' ? ` <span class="muted">(${esc(t('vendor'))})</span>` : ''}</span>`;
  }).join('');
  const badges = [
    r.isNew ? `<span class="badge forever">${esc(t('NEW in Forever'))}</span>` : '',
    r.cooldown ? `<span class="badge">${esc(t('cooldown {time}', { time: r.cooldown }))}</span>` : '',
    r.craftable ? `<span class="badge ok"${hintAttr(t('from bags and bank'))}>${esc(t('{n}× craftable', { n: r.craftable }))}</span>` : '',
  ].join(' ');
  return `<div class="recipe"><span class="recipe-name">${dot}<span class="item">${itemIcon(r.output, r.quality)}<span class="${quality(r.quality)}">${esc(r.name)}</span></span>${qty}</span> ${badges}</div>
    <div class="reagents">${reagents}</div>
    ${r.notes.length ? `<div class="small warn-text">${esc(r.notes.join(' · '))}</div>` : ''}`;
}

// Profit per craft on the last full scan days as a small line; the dashed line is zero.
function profitTrend(r) {
  const h = r.history || [];
  if (h.length < 2) return `<span class="muted"${hintAttr(t('A trend needs prices from at least two full scan days.'))}>–</span>`;
  const W = 116, H = 30, pad = 4;
  const time = h.map((p) => Date.parse(p.date));
  const values = h.map((p) => p.profit);
  const lo = Math.min(0, ...values), hi = Math.max(0, ...values);
  const x = (i) => pad + ((time[i] - time[0]) / (time[h.length - 1] - time[0])) * (W - 2 * pad);
  const y = (v) => pad + ((hi - v) / (hi - lo || 1)) * (H - 2 * pad);
  const points = h.map((p, i) => `${x(i).toFixed(1)},${y(p.profit).toFixed(1)}`).join(' ');
  const [cls, label] = TREND[r.trend] || ['flat', ''];
  // The daily values behind the line, newest last.
  const tip = `<table><thead><tr>${th([t('Day')], [t('Profit'), 'num'], [t('Price'), 'num'], [t('in the AH'), 'num'])}</tr></thead><tbody>${
    h.map((p) => `<tr><td>${esc(shortDay(p.date))}</td><td class="num ${p.profit < 0 ? 'loss' : 'good'}">${esc(goldText(p.profit))}</td>`
      + `<td class="num">${num(p.price / 10000, 2)} g</td><td class="num">${esc(p.listed)}</td></tr>`).join('')}</tbody></table>`;
  return `<div class="trend ${cls}"${helpAttr(tip)}>
      <svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" role="img" aria-label="${esc(t('Profit trend'))}">
        <line class="zero" x1="0" x2="${W}" y1="${y(0).toFixed(1)}" y2="${y(0).toFixed(1)}"/>
        <polyline points="${points}"/>
        ${h.map((p, i) => `<circle cx="${x(i).toFixed(1)}" cy="${y(p.profit).toFixed(1)}" r="${i === h.length - 1 ? 2.6 : 1.4}"/>`).join('')}
      </svg>
      <div class="small">${esc(label && t(label))}</div></div>`;
}

function marketCells(r) {
  return `<td class="num">${money(r.cost)}</td>
    <td class="num">${money(r.sell)}${r.sellSource === 'vendor' ? `<div class="small muted">${esc(t('vendor'))}</div>` : ''}</td>
    <td class="num"><b>${signedMoney(r.profit)}</b>${r.margin !== null ? `<div class="small muted">${r.margin > 0 ? '+' : ''}${percent(r.margin)}</div>` : ''}</td>
    <td>${profitTrend(r)}</td>
    <td class="num ${r.listed < 3 ? 'warn-text' : 'muted'}">${r.listed}</td>`;
}

// Where the recipe comes from and whether the skill is high enough to learn it.
function acquireCell(r, rank) {
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
  const crafts = Math.ceil(r.payback || 0);
  const payback = crafts ? `<div class="small muted"><span${hintAttr(t('Price of the recipe divided by the profit per craft.'))}>${
    esc(t(crafts === 1 ? mark('paid off after 1 craft') : mark('paid off after {n} crafts'), { n: crafts }))}</span></div>` : '';
  return `${parts}${text}${payback}<div class="learn">${learnBadge(r, rank)}</div>`;
}

function learnBadge(r, rank) {
  const a = r.acquire;
  if (a.learnable === true) return `<span class="badge ok">${esc(a.reqSkill ? t('learnable now (from {n})', { n: a.reqSkill }) : t('learnable now'))}</span>`;
  if (a.reqSkill) return `<span class="badge warn">${esc(t('from skill {n}, {left} to go', { n: a.reqSkill, left: a.reqSkill - rank }))}</span>`;
  if (a.trainer && r.yellow) return `<span class="badge">${esc(t('learnable at {n} at the latest', { n: r.yellow }))}</span>`;
  return `<span class="muted">${esc(t('required skill unknown'))}</span>`;
}

let recipeData = null;

function activateRecipeTab(key) {
  state.recipes.tab = key;
  for (const panel of content.querySelectorAll('[data-rec-panel]')) panel.hidden = panel.dataset.recPanel !== key;
  content.scrollTop = 0;
  saveView();
}

function matchesQuery(r, words) {
  if (!words.length) return true;
  const hay = [r.name, ...(r.reagents || []).map((g) => g.name)].join(' ').toLocaleLowerCase(locale());
  return words.every((w) => hay.includes(w));
}

function renderRecipeBody(d) {
  const o = state.recipes;
  const p = d.professions.find((x) => x.skillLine === o.skillLine);
  const words = (o.query || '').trim().toLocaleLowerCase(locale()).split(/\s+/).filter(Boolean);
  const keep = (r) => (!o.onlyProfit || (r.profit ?? -1) > 0) && r.listed >= o.minListed && matchesQuery(r, words);
  const learned = p.learned.filter(keep);
  const missing = p.missing.filter((r) => keep(r) && (!o.onlyLearnable || r.acquire.learnable === true));
  const scanned = !!p.scanned;
  const activeTab = scanned && o.tab === 'missing' ? 'missing' : 'learned';

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

  // The explanations of the numbers sit behind a "?" in the column they belong to.
  const columns = [
    [t('Recipe'), '', esc(t('The dot is the skill colour: orange always gives a skill point, yellow usually, green rarely, grey never. Hover a reagent for its price and where it is cheapest.'))],
    [t('Reagents'), 'num', esc(t('Cost of one craft: every reagent at the cheaper price from the AH or a vendor. Your own reagents count at the market price. The addon reads vendor prices when you talk to a vendor ({n} so far).', { n: d.vendorPrices }))],
    [t('Sale'), 'num', esc(t('Lowest buyout of the result at the last scan × amount, minus the 5% AH fee. Results that are bound or not in the AH count at the vendor price.'))],
    [t('Profit'), 'num', esc(t('Sale minus reagents for one craft, below it in percent of the reagent cost.'))],
    [t('Trend'), '', esc(t('The same profit on the last full scan days; the dashed line is zero. “Falling” or “rising”: the profit changed by more than 20% over the last three scan days. Hover the line for the daily values.'))],
    [t('in the AH'), 'num', esc(t('Offers of the result at the last scan. With few offers the price is uncertain.'))],
  ];
  const learnedTable = `<div class="card">
      ${learned.length ? `<table class="sticky"><thead><tr>${th(...columns)}</tr></thead>
      <tbody>${learned.map((r) => `<tr><td>${recipeName(r)}</td>${marketCells(r)}</tr>`).join('')}</tbody></table>`
        : `<p class="muted">${esc(t('No learned recipe matches the filter.'))}</p>`}</div>`;

  const missingTable = `<div class="card">
      ${missing.length ? `<table class="sticky"><thead><tr>${th(...columns,
        [t('Where from'), '', esc(t('Where the recipe comes from: the AH, a vendor the addon has seen, the trainer, or a recipe item that is not in the AH (drop, reputation or a vendor). The badge says whether your skill is high enough to learn it, or from which skill on.'))])}</tr></thead>
      <tbody>${missing.map((r) => `<tr class="${r.better ? 'best' : ''}"><td>${r.better ? `<span class="badge new">${esc(t('pays more'))}</span> ` : ''}${recipeName(r)}</td>${marketCells(r)}
        <td class="small where">${acquireCell(r, p.rank)}</td></tr>`).join('')}</tbody></table>`
        : `<p class="muted">${esc(t('No recipe matches the filter.'))}</p>`}</div>`;

  if (state.page === 'recipes') {
    if (scanned) {
      setTabs([
        { key: 'learned', label: t('Your recipes'), meta: `${learned.length}/${p.learnedCount}` },
        { key: 'missing', label: t('Not learned yet'), meta: `${missing.length}/${p.missingCount}` },
      ], activeTab, activateRecipeTab);
    } else {
      clearTabs();
    }
  }
  const body = $('#rec-body');
  if (!body) return;
  if (!scanned) {
    const missingTitle = t('All recipes ({shown} of {total})', { shown: missing.length, total: p.missingCount });
    body.innerHTML = `${notice}${header}<div class="section"><h2>${esc(missingTitle)}</h2>${missingTable}</div>`;
    return;
  }
  body.innerHTML = `${header}
    <div class="tab-panel section" data-rec-panel="learned"${activeTab === 'learned' ? '' : ' hidden'}>${learnedTable}</div>
    <div class="tab-panel section" data-rec-panel="missing"${activeTab === 'missing' ? '' : ' hidden'}>${missingTable}</div>`;
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
  recipeData = d;
  if (!d.professions.some((p) => p.skillLine === o.skillLine)) o.skillLine = d.professions[0].skillLine;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Profession'))}<select id="r-prof">${d.professions.map((x) => selectOption(x.skillLine, `${x.name} ${x.rank}/${x.max}`, o.skillLine)).join('')}</select></label>
      <label>${esc(t('Search'))}<input type="search" id="r-query" value="${esc(o.query)}" placeholder="${esc(t('Recipe or reagent'))}" autocomplete="off"></label>
      <label><span class="label">${esc(t('At least in the AH'))} ${help(esc(t('Hides recipes whose result has fewer offers in the AH: with few offers the price says little.')))}</span>
        <input type="number" id="r-listed" min="0" max="1000" value="${o.minListed}"></label>
      <label class="check"><input type="checkbox" id="r-profit" ${o.onlyProfit ? 'checked' : ''}> ${esc(t('only with profit'))}</label>
      <label class="check"><input type="checkbox" id="r-learnable" ${o.onlyLearnable ? 'checked' : ''}> ${esc(t('only learnable now'))}</label>
    </div>
    <div id="rec-body"></div>`;
  renderRecipeBody(d);
  let timer;
  $('#r-query').addEventListener('input', (e) => {
    clearTimeout(timer);
    timer = setTimeout(() => { o.query = e.target.value; saveView(); if (recipeData) renderRecipeBody(recipeData); }, 100);
  });
  $('#r-prof').addEventListener('change', (e) => { o.skillLine = Number(e.target.value); renderRecipeBody(d); });
  $('#r-listed').addEventListener('change', (e) => { o.minListed = Math.max(0, Number(e.target.value) || 0); renderRecipeBody(d); });
  $('#r-profit').addEventListener('change', (e) => { o.onlyProfit = e.target.checked; renderRecipeBody(d); });
  $('#r-learnable').addEventListener('change', (e) => { o.onlyLearnable = e.target.checked; renderRecipeBody(d); });
}
