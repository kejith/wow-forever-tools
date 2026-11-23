// Page "Consumables": the strongest potions, food, drinks, bandages and buffs the selected character can use
// at its level, with AH or vendor price, bags and own recipes; and, rated by the combat engine, the
// consumables for a fight by kind with the best set of them: the strongest one, or the one by value (the
// most gain per gold, as a buying order, up to a budget). Calculated by wowtools consumables. The filters work
// on the loaded data; the number per category, the fight, the set and its budget are bridge options. The
// Character page shows the same data as its section "Consumables" (consumablesSection).
import { $, api, cached, content, errorBox, esc, help, hintAttr, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, state, terms } from '../core.js';
import { t } from '../i18n.js';

// The bridge call both pages share: one answer per character, number per category, fight and set.
export function loadConsumables(char) {
  const o = state.consumables;
  const byValue = o.set === 'value';
  const budget = byValue && Number(o.budget) > 0 ? Number(o.budget) : 0;
  return cached(`consumables:${char}:${o.top}:${o.fight}:${o.target}:${byValue}:${budget}`, () => api.bridge('consumables', {
    char, top: o.top, fightSeconds: Number(o.fight) || 60, targetDelta: o.target === '' ? -1 : Number(o.target),
    set: byValue ? 'value' : 'best', budget,
  }));
}

function priceCell(r) {
  if (!r.price) return `<td class="num muted">–</td>`;
  const source = r.source === 'vendor'
    ? (r.vendorNpc ? t('vendor {npc}', { npc: r.vendorNpc }) : t('vendor'))
    : t('AH, {n} listed', { n: r.listed });
  // Both prices when the vendor is cheaper than the AH or the other way round.
  const other = r.source === 'vendor' && r.ahPrice ? `AH ${money(r.ahPrice)}`
    : r.source === 'ah' && r.vendorPrice ? `${esc(t('vendor'))} ${money(r.vendorPrice)}` : '';
  return `<td class="num nowrap">${money(r.price)}<div class="small muted">${esc(source)}</div>${other ? `<div class="small muted">${other}</div>` : ''}</td>`;
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
  // Up to two names; a longer list is behind the hover.
  const names = r.alternatives.map((a) => a.name);
  const shown = names.length > 3 ? `${names.slice(0, 2).join(', ')} ${t('+{n} more', { n: names.length - 2 })}` : names.join(', ');
  const alternatives = names.length
    ? `<div class="small muted"><span${names.length > 3 ? hintAttr(names.join(', ')) : ''}>${esc(t('same effect: {items}', { items: shown }))}</span></div>` : '';
  return `<td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}">${esc(r.name)}</span></span> ${badges}
      <div class="small">${esc(details)}</div>
      ${from ? `<div class="small muted">${esc(from)}</div>` : ''}
      ${r.available ? '' : `<div class="small warn-text">${esc(t('not in the AH right now'))}</div>`}${alternatives}</td>`;
}

// What the marks on the cards mean; every card has the same ones, so the explanation sits at the top once.
const pageTerms = () => terms([
  [t('best now'), t('The strongest item you can use at your level and get right now: in your bags, in the AH, at a vendor or with one of your recipes.')],
  [t('Price'), t('Lowest buyout at the last scan or the vendor price, whichever is cheaper.')],
  [t('same effect: {items}', { items: '…' }), t('Items that do the same are merged into one row.')],
  [t('Next:'), t('The next stronger item, which needs a higher level or skill.')],
], t('Strongest first. What works together in a fight is in the section above.'));

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

// ---------- consumables for a fight ----------

const signed = (n) => `${n < 0 ? '−' : '+'}${num(Math.abs(n), 1)}`;
const itemName = (r, size) => `<span class="item">${itemIcon(r.id, r.quality, size)}<span class="${quality(r.quality)}">${esc(r.name)}</span></span>`;
const bags = (r) => `<td class="num">${r.owned || '<span class="muted">–</span>'}</td>`;

const fightTerms = (p) => terms([
  [t('Best set'), t('Built one item at a time: always the one that adds the most on top of those already in it, among what you can get now and what does not exclude an item of the set.')],
  [t('Adds'), t('What the item adds on top of the ones above it. Gains do not add up: once the mana lasts, more mana adds nothing.')],
  [t('Alone'), t('What the item adds to your {unit} when it is the only one you use. The combat model picks the best rotation again with it.', { unit: p.unit })],
  [t('To buy'), t('One of each item of the set you do not have, at the price of the last scan. A flask lasts two hours, a potion one fight.')],
  [t('instead: {name}', { name: '…' }), t('Shares its place with that item of the set, which adds more: use one or the other.')],
  [t('on you'), t('Its buff was on you when the snapshot was taken: your numbers have it already.')],
  [t('assumption'), t('A rule nobody has confirmed for Forever. The “?” of the card says which.')],
], t('What a potion gives is spread over the fight; a buff counts for the part of the fight it lasts.'));

function setTable(p) {
  if (!p.chosen.length) return `<p class="muted">${esc(t('No consumable you can get now adds anything.'))}</p>`;
  const most = Math.max(1, ...p.chosen.map((r) => r.adds));
  const labels = Object.fromEntries(p.kinds.map((k) => [k.key, k.label]));
  return `<table class="compact cons-set"><thead><tr><th>${esc(t('Adds'))}</th><th>${esc(t('Item'))}</th><th>${esc(t('Kind'))}</th>
      <th class="num">${esc(t('Price'))}</th><th class="num">${esc(t('in bags'))}</th></tr></thead><tbody>
    ${p.chosen.map((r) => `<tr><td><div class="gain"><b>${signed(r.adds)}</b><div class="bar"><span style="width:${Math.round((r.adds / most) * 100)}%"></span></div></div></td>
      <td>${itemName(r)}<div class="small muted">${esc([r.effect, r.duration].filter(Boolean).join(' · '))}</div></td>
      <td class="muted">${esc(labels[r.kind] || '')}</td>${priceCell(r)}${bags(r)}</tr>`).join('')}</tbody></table>`;
}

// ---------- the set by value ----------

const perHour = (copper) => t('{cost} an hour', { cost: money(copper) });
const perGoldText = (n) => (n === null || n === undefined ? '<span class="muted">–</span>' : num(n, 1));

const valueTerms = (p) => terms([
  [t('Buying order'), t('The set grows one step at a time: always the step that adds the most per gold. What is in your bags comes first. A stronger item replaces a weaker one of its kind once the gold it costs more buys the most there.')],
  [t('Adds'), t('What the step adds on top of the ones above it; for a replacement, what the stronger item adds more. Gains do not add up: once the mana lasts, more mana adds nothing.')],
  [t('Per hour'), t('What an hour of fighting costs more with the step. A buff counts for the time it lasts: a flask of two hours costs half its price an hour. A potion or a rune counts once in every fight. What is in your bags costs nothing.')],
  [t('Per gold'), t('What the step adds to your {unit} for each gold of that. The list is in the order of this number: stop where it gets too low for you.', { unit: p.unit })],
  [t('So far'), t('Your {unit} and what an hour of fighting costs with every step down to this one.', { unit: p.unit })],
  [t('Budget'), t('The most an hour of fighting may cost. The set ends where no further step fits.')],
  [t('Alone'), t('What the item adds to your {unit} when it is the only one you use. The combat model picks the best rotation again with it.', { unit: p.unit })],
  [t('To buy'), t('One of each item of the set you do not have, at the price of the last scan. A flask lasts two hours, a potion one fight.')],
  [t('on you'), t('Its buff was on you when the snapshot was taken: your numbers have it already.')],
  [t('assumption'), t('A rule nobody has confirmed for Forever. The “?” of the card says which.')],
], t('What a potion gives is spread over the fight; a buff counts for the part of the fight it lasts.'));

function valueTable(p) {
  if (!p.steps.length) return `<p class="muted">${esc(t('No consumable you can get now adds anything.'))}</p>`;
  const most = Math.max(1, ...p.steps.map((s) => s.adds));
  const labels = Object.fromEntries(p.kinds.map((k) => [k.key, k.label]));
  const row = (s) => {
    const r = s.item;
    const note = [
      s.replaces.length ? t('replaces {names}', { names: s.replaces.map((x) => x.name).join(', ') }) : '',
      s.replacedBy ? t('replaced further down by {name}', { name: s.replacedBy }) : '',
    ].filter(Boolean).join(' · ');
    const cost = s.costPerHour > 0 ? `+${perHour(s.costPerHour)}`
      : s.costPerHour < 0 ? `−${perHour(-s.costPerHour)}`
        : r.owned ? `<span class="muted">${esc(t('{n} in bags', { n: r.owned }))}</span>` : '<span class="muted">–</span>';
    return `<tr class="${s.replacedBy ? 'cons-replaced' : ''}"><td class="num muted">${s.n}</td>
      <td><div class="gain"><b>${signed(s.adds)}</b><div class="bar"><span style="width:${Math.round((Math.max(0, s.adds) / most) * 100)}%"></span></div></div></td>
      <td>${itemName(r)}<div class="small muted">${esc([r.effect, r.duration, labels[r.kind]].filter(Boolean).join(' · '))}</div>
        ${note ? `<div class="small ${s.replaces.length ? 'cons-swap' : 'muted'}">${esc(note)}</div>` : ''}</td>
      <td class="num nowrap">${cost}</td>
      <td class="num nowrap"><b>${perGoldText(s.perGold)}</b></td>
      <td class="num nowrap">${num(s.value, 1)} <span class="small muted">${esc(p.unit)}</span><div class="small muted">${perHour(s.spent)}</div></td>
      ${priceCell(r)}</tr>`;
  };
  return `<table class="compact cons-set cons-value"><thead><tr><th class="num">#</th><th>${esc(t('Adds'))}</th><th>${esc(t('Item'))}</th>
      <th class="num">${esc(t('Per hour'))}</th><th class="num">${esc(t('Per gold'))}</th><th class="num">${esc(t('So far'))}</th>
      <th class="num">${esc(t('Price'))}</th></tr></thead><tbody>${p.steps.map(row).join('')}</tbody></table>`;
}

function kindRow(r) {
  const badges = [
    r.chosen ? `<span class="badge new">${esc(t('in the set'))}</span>` : '',
    r.active ? `<span class="badge ok">${esc(t('on you'))}</span>` : '',
    r.owned ? `<span class="badge ok">${esc(t('{n} in bags', { n: r.owned }))}</span>` : '',
    r.craft?.learned ? `<span class="badge ok">${esc(t('you can craft it'))}</span>` : '',
  ].join(' ');
  const details = [r.effect, r.duration, r.uses > 1 ? t('{n} per fight', { n: r.uses }) : ''].filter(Boolean).join(' · ');
  const note = r.active ? ''
    : r.onYou ? `<div class="small muted">${esc(t('{name} is on you already', { name: r.onYou }))}</div>`
    : !r.available ? `<div class="small warn-text">${esc(t('not in the AH right now'))}</div>`
      : r.tooCostly ? `<div class="small warn-text">${esc(t('costs more than half your health'))}</div>`
      : r.noPrice ? `<div class="small muted">${esc(t('no price known, so not rated per gold'))}</div>`
      : r.overBudget ? `<div class="small warn-text">${esc(t('does not fit the budget'))}</div>`
      : r.blockedBy ? `<div class="small muted">${esc(t('instead: {name}', { name: r.blockedBy }))}</div>` : '';
  // The set by value: what the item adds alone for each gold an hour of fighting costs with it.
  const worth = r.perGold === undefined || r.active ? '' : `<div class="small muted">${esc(t('{n} per gold', { n: num(r.perGold, 1) }))}</div>`;
  return `<tr class="${r.chosen ? 'best' : ''}"><td>${itemName(r)} ${badges}<div class="small">${esc(details)}</div>${note}</td>
      <td class="num nowrap">${r.active ? '<span class="muted">–</span>' : `<b class="${r.gain > 0 ? 'good' : 'muted'}">${signed(r.gain)}</b>`}${worth}</td>
      ${priceCell(r)}</tr>`;
}

function kindCard(k, o, byValue) {
  const rows = k.items.filter((r) => !o.onlyAvailable || r.available || r.active);
  const rule = `<p>${esc(k.rule)}</p>${k.assumed ? `<p><span class="badge warn">${esc(t('assumption'))}</span> ${esc(k.assumed)}</p>` : ''}`;
  const body = rows.length
    ? `<table><thead><tr><th>${esc(t('Item'))}</th><th class="num">${esc(t('Alone'))}</th><th class="num">${esc(t('Price'))}</th></tr></thead>
      <tbody>${rows.map(kindRow).join('')}</tbody></table>`
    : `<p class="muted">${esc(t('Nothing for your level that you can get right now.'))}</p>`;
  return `<div class="card cons-card" data-kind="${esc(k.key)}"><h3>${esc(k.label)} ${help(rule)}${k.assumed ? ` <span class="badge warn"${hintAttr(k.assumed)}>${esc(t('assumption'))}</span>` : ''}</h3>
      ${body}${k.more ? `<p class="small muted cons-more">${esc(byValue ? t('… and {n} more that are worth less', { n: k.more }) : t('… and {n} more that add less', { n: k.more }))}</p>` : ''}</div>`;
}

function planSection(p, o) {
  const fight = [t('Fight of {seconds} s', { seconds: num(p.seconds) }), t('enemy level +{n}', { n: p.targetDelta }), t('combat model {version}', { version: p.engine })].join(' · ');
  const tile = (label, value, sub = '') => `<div><h2>${esc(label)}</h2><div class="big">${value}</div>${sub ? `<div class="small muted">${sub}</div>` : ''}</div>`;
  const cost = p.cost ? money(p.cost) : '<span class="muted">–</span>';
  const byValue = p.mode === 'value';
  const hourly = byValue ? tile(t('Per hour of fighting'), p.costPerHour ? money(p.costPerHour) : '<span class="muted">–</span>',
    p.budget ? t('of {budget} budget', { budget: money(p.budget) }) : esc(t('a potion in every fight'))) : '';
  return `<div class="section cons-fight" id="cons-${esc(p.metric)}">
      <h2 class="heading">${esc(t('Consumables for a fight: {title}', { title: p.title }))} ${help(byValue ? valueTerms(p) : fightTerms(p))}
        <span class="spacer"></span><span class="small muted">${esc(fight)}</span></h2>
      <div class="card cons-plan"><div class="row cons-tiles">
          ${tile(t('Without'), `${num(p.base, 1)} <span class="small muted">${esc(p.unit)}</span>`)}
          ${tile(byValue ? t('With this set') : t('With the best set'), `<span class="good">${num(p.value, 1)}</span> <span class="small muted">${esc(p.unit)}</span>`, `${signed(p.gain)} (${signed(p.gainPct)} %)`)}
          ${hourly}
          ${tile(t('To buy'), cost, p.unpriced ? esc(t('{n} without a price', { n: p.unpriced })) : esc(t('one of each')))}
          <span class="spacer"></span></div>
        <h3>${esc(byValue ? t('Buying order') : t('Best set'))}</h3>${byValue ? valueTable(p) : setTable(p)}
        ${p.active.length ? `<p class="small muted">${esc(t('On you at the snapshot, so already in the numbers: {names}', { names: p.active.join(', ') }))}</p>` : ''}</div>
      <div class="cons-grid">${p.kinds.map((k) => kindCard(k, o, byValue)).join('')}</div></div>`;
}

// The consumables for a fight: one block per metric of the class, or why there is none.
export function fightSections(d, o = state.consumables) {
  if (d.plans?.length) return d.plans.map((p) => planSection(p, o)).join('');
  return d.planNote ? `<div class="notice small">${esc(t('No consumables for a fight: {why}', { why: d.planNote }))}</div>` : '';
}

// The section "Consumables" of the Character page: by kind the best and its alternatives with the best set,
// then what restores health and mana. Without the combat engine the buffs by stat stand in.
export function consumablesSection(d) {
  const o = state.consumables;
  const buffs = d.plans?.length ? '' : `<div class="cons-grid" style="margin-bottom:16px">${d.buffs.filter((g) => g.relevant).map((g) => groupCard(g, o)).join('')}</div>`;
  return `${fightSections(d, o)}${buffs}
    <div class="section"><h2 class="heading">${esc(t('Restoring health and mana'))} ${help(pageTerms())}
        <span class="spacer"></span><span class="small muted">${esc(d.scanDate ? t('AH scan {date}', { date: d.scanDate }) : '')}</span></h2>
      <div class="cons-grid">${d.categories.map((g) => groupCard(g, o)).join('')}</div></div>`;
}

export async function renderConsumables() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = `<div class="notice">${esc(t('No character exported.'))}</div>`; return; }
  const o = state.consumables;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Items per category'))}<input type="number" id="c-top" min="1" max="20" value="${o.top}"></label>
      <label><span class="label">${esc(t('Fight length (s)'))} ${help(esc(t('The fight the consumables are rated for. In a longer fight mana counts more and a short buff less.')))}</span>
        <input type="number" id="c-fight" min="10" max="600" step="10" value="${o.fight}"></label>
      <label>${esc(t('Enemy level'))}<select id="c-target">${selectOption('', t('automatic'), o.target)}${[0, 1, 2, 3].map((n) => selectOption(String(n), `+${n}`, o.target)).join('')}</select></label>
      <label><span class="label">${esc(t('Set'))} ${help(esc(t('Which consumables the set for a fight holds. The strongest: whatever adds the most, at any price. Best value: in the order of what an item adds per gold, so you see where more gold stops paying.')))}</span>
        <select id="c-set">${selectOption('best', t('the strongest'), o.set)}${selectOption('value', t('best value per gold'), o.set)}</select></label>
      ${o.set === 'value' ? `<label><span class="label">${esc(t('Budget (gold per hour)'))} ${help(esc(t('The most an hour of fighting may cost with the set. Empty: no limit, the list then runs on to the strongest set.')))}</span>
        <input type="number" id="c-budget" class="wide" min="0" step="0.5" value="${esc(o.budget)}" placeholder="${esc(t('no limit'))}"></label>` : ''}
      <label class="check"><input type="checkbox" id="c-available" ${o.onlyAvailable ? 'checked' : ''}> ${esc(t('only what you can get now'))}</label>
      <label class="check"><input type="checkbox" id="c-all" ${o.allBuffs ? 'checked' : ''}> ${esc(t('also buffs your class does not need'))}</label>
      <span class="spacer"></span><div class="page-meta"><span id="c-meta"></span>${help(pageTerms())}</div>
    </div><div id="c-body">${loading(t('Looking for consumables for your level …'))}</div>`;
  $('#c-top').addEventListener('change', (e) => { o.top = Math.min(20, Math.max(1, Number(e.target.value) || 5)); renderConsumables(); });
  $('#c-fight').addEventListener('change', (e) => { o.fight = Math.min(600, Math.max(10, Number(e.target.value) || 60)); renderConsumables(); });
  $('#c-target').addEventListener('change', (e) => { o.target = e.target.value; renderConsumables(); });
  $('#c-set').addEventListener('change', (e) => { o.set = e.target.value === 'value' ? 'value' : 'best'; renderConsumables(); });
  $('#c-budget')?.addEventListener('change', (e) => { o.budget = Number(e.target.value) > 0 ? String(Number(e.target.value)) : ''; renderConsumables(); });
  $('#c-available').addEventListener('change', (e) => { o.onlyAvailable = e.target.checked; renderConsumables(); });
  $('#c-all').addEventListener('change', (e) => { o.allBuffs = e.target.checked; renderConsumables(); });
  const d = await loadConsumables(state.char);
  const body = $('#c-body');
  if (!body) return;
  if (d.error) { body.innerHTML = errorBox(d.error); return; }
  const buffs = d.buffs.filter((g) => o.allBuffs || g.relevant);
  const hidden = d.buffs.length - buffs.length;
  $('#c-meta').textContent = [t('Level {level}', { level: d.level }), d.scanDate ? t('AH scan {date}', { date: d.scanDate }) : ''].filter(Boolean).join(' · ');
  body.innerHTML = `${d.recipesScanned ? '' : `<div class="notice small">${t('Open your profession windows once in the game and type <code>/reload</code>, then the page also knows what you can craft.')}</div>`}
    ${fightSections(d, o)}
    <div class="section" id="c-restore"><h2>${esc(t('Restoring health and mana'))}</h2><div class="cons-grid">${d.categories.map((g) => groupCard(g, o)).join('')}</div></div>
    <div class="section" id="c-buffs"><h2>${esc(t('Buffs by stat'))}</h2>
      ${hidden ? `<p class="small muted">${esc(t('{n} more groups that do not help your class are hidden.', { n: hidden }))}</p>` : ''}
      <div class="cons-grid">${buffs.map((g) => groupCard(g, o)).join('')}</div></div>`;
}
