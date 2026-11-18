// Page "Writs": the best craftsman's writ per tier by net silver per reputation, and the trade crates by
// silver per Merchant's Favor. The numbers come from scripts/writs.py.
import { $, DASH, api, cached, content, errorBox, esc, itemIcon, loadStatus, loading, money, num, state } from '../core.js';
import { t } from '../i18n.js';

// Net cost: negative means the coin pouch pays more than the writ and item cost.
function netMoney(copper) {
  if (copper === null || copper === undefined) return money(copper);
  const c = Math.round(copper);
  return c < 0 ? `<span class="good">${t('{money} profit', { money: money(-c) })}</span>` : money(c);
}

// Copper per unit (reputation or favor) as silver with two decimals.
function perUnit(copper) {
  if (copper === null || copper === undefined) return DASH;
  return `${num(copper / 100, 2)} s`;
}

const th = (...cells) => cells.map(([label, cls]) => `<th${cls ? ` class="${cls}"` : ''}>${esc(label)}</th>`).join('');

export async function renderWrits() {
  if (!state.status) await loadStatus();
  const o = state.writs;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Suggestions per tier'))}<input type="number" id="w-top" min="1" max="10" value="${o.top}"></label>
      <label class="check"><input type="checkbox" id="w-own" ${o.ownedAtAhPrice ? 'checked' : ''}> ${esc(t('count own items at the AH price'))}</label>
    </div><div id="w-body">${loading(t('Calculating writs …'))}</div>`;
  $('#w-top').addEventListener('change', (e) => { o.top = Number(e.target.value) || 3; renderWrits(); });
  $('#w-own').addEventListener('change', (e) => { o.ownedAtAhPrice = e.target.checked; renderWrits(); });
  const key = `writs:${state.char}:${o.top}:${o.ownedAtAhPrice}`;
  const d = await cached(key, () => api.bridge('writs', { char: state.char, top: o.top, ownedAtAhPrice: o.ownedAtAhPrice }));
  const body = $('#w-body');
  if (!body) return;
  if (d.error) { body.innerHTML = errorBox(d.error); return; }
  const rep = d.reputation;
  const repHtml = rep ? `<div class="card" style="margin-bottom:16px"><div class="row">
      <div><h3>${esc(rep.name)}</h3><div class="muted">${esc(t('{standing} · {value} / {max} · {left} to the next standing',
        { standing: rep.standing, value: rep.value, max: rep.max, left: rep.max - rep.value }))}</div></div>
      <span class="spacer"></span><div class="muted small">${esc(t('AH scan {date}', { date: d.scanDate }))}</div></div>
      <div class="bar" style="margin-top:10px"><span style="width:${Math.round((rep.value / rep.max) * 100)}%"></span></div></div>` : '';
  const tiers = d.tiers.map((tier) => {
    const rows = tier.best.map((b, i) => `<tr class="${i === 0 ? 'best' : ''}">
        <td><span class="item">${itemIcon(b.writId, 1)}<span>${i === 0 ? '★ ' : ''}${esc(b.name)}${b.qty > 1 ? ` <span class="muted">(${b.qty}x)</span>` : ''}</span></span></td>
        <td class="muted">${esc(b.prof)}</td>
        <td class="num">${money(b.wcost)}</td><td class="num">${money(b.icost)}</td>
        <td class="num">${money(b.cost)}</td>
        <td class="num" title="${esc(b.payoutSource)}"><span class="good">−${b.payoutMeasured ? '' : '≈'}${money(b.payout)}</span></td>
        <td class="num"><b>${netMoney(b.net)}</b></td>
        <td class="num">${perUnit(b.per)}</td>
        <td class="num muted">${b.writs}</td>
        <td class="small">${esc(b.note)}</td></tr>`).join('');
    const sealed = tier.sealed ? `<p class="small notice">${esc(t('You have {n}x “Sealed Writ” of this tier. Open it first, it may be cheaper.', { n: tier.sealed }))}</p>` : '';
    return `<div class="card section"><div class="row"><h3>${esc(tier.name)}</h3></div>
      ${rows ? `<table><thead><tr>${th([t('Writ')], [t('Profession')], [t('Writ'), 'num'], [t('Item'), 'num'], [t('Total'), 'num'], [t('Coin pouch'), 'num'],
        [t('Net'), 'num'], [t('per reputation'), 'num'], [t('in the AH'), 'num'], [t('Note')])}</tr></thead><tbody>${rows}</tbody></table>`
        : `<p class="muted">${esc(t('No writ with price data.'))}</p>`}${sealed}</div>`;
  }).join('');
  const sources = [...new Set(d.tiers.flatMap((tier) => tier.best.map((b) => b.payoutSource)))];
  const estimated = d.tiers.some((tier) => tier.best.some((b) => !b.payoutMeasured));
  body.innerHTML = `${repHtml}
    <div class="card"><div class="row"><div><h2>${esc(t('Per day'))}</h2><div class="big">${netMoney(d.dailyNet)}</div>
        <div class="muted small">${t('net after coin pouches · {money} to buy', { money: money(d.dailyTotal) })}</div></div>
      <span class="spacer"></span><div class="muted">${esc(t('for {rep} reputation (the best writ of each tier)', { rep: d.dailyRep }))}</div></div></div>
    ${tiers}
    <p class="small muted">${esc(t('Items you own count as 0. The quest itself pays no gold, it gives a coin pouch; net = purchase − content of the pouch, sorted by net per reputation. Coin pouch: {sources}.',
      { sources: sources.join(' · ') }))}
      ${estimated ? esc(t('Values with ≈ are estimates; from addon 1.5 on the addon measures the content of every pouch you open.')) : ''}
      ${esc(t('Writ list: wow.gg (Nov 2026).'))}</p>
    ${cratesHtml(d)}`;
}

function cratesHtml(d) {
  const c = d.crates;
  if (!c) return '';
  const bundle = (o) => (o ? `<span class="item">${itemIcon(o.id, 1, 'sm')}<span>${o.qty}x ${esc(o.name)}</span></span>` : DASH);
  const alternatives = (r) => r.options.filter((o) => o !== r.best && r.best && o.id !== r.best.id).map((o) =>
    `${o.qty}x ${esc(o.name)} ${o.cost === null ? `<span class="muted">${esc(t('no price'))}</span>` : money(o.cost)}`
    + `${o.enough ? '' : ` <span class="warn-text">(${esc(t('too few in the AH'))})</span>`}`).join(' · ');
  const best = c.best.map((r, i) => `<tr class="${i === 0 ? 'best' : ''}">
      <td><span class="item">${itemIcon(r.crateId, 1)}<span>${i === 0 ? '★ ' : ''}${esc(r.kind)} <span class="muted">(${esc(r.tierName)})</span></span></span></td>
      <td>${bundle(r.best)}</td><td class="num"><b>${money(Math.round(r.cost))}</b></td>
      <td class="num">${num(r.favor)}</td><td class="num">${perUnit(r.per)}</td></tr>`).join('');
  const tiers = c.tiers.map((tier) => {
    const rows = tier.rows.map((r, i) => `<tr class="${i === 0 && r.complete ? 'best' : ''}">
        <td><span class="item" title="${esc(r.name || '')}">${itemIcon(r.crateId, 1)}<span>${i === 0 && r.complete ? '★ ' : ''}${esc(r.kind)}</span></span></td>
        <td class="num">${r.crateCost === null ? `<span class="muted">${esc(t('not in the AH'))}</span>` : money(r.crateCost)}</td>
        <td>${bundle(r.best)}${alternatives(r) ? `<div class="small muted">${t('or {alternatives}', { alternatives: alternatives(r) })}</div>` : ''}</td>
        <td class="num">${r.best ? money(r.best.cost) : DASH}</td>
        <td class="num"><b>${r.complete ? money(Math.round(r.cost)) : DASH}</b></td>
        <td class="num">${perUnit(r.per)}</td><td class="num muted">${perUnit(r.perGreen)}</td>
        <td class="small">${esc(r.note)}</td></tr>`).join('');
    const favor = t('{favor} favor · green {green}', { favor: num(tier.favor), green: num(tier.favorGreen) }) + (tier.favorGuess ? ` (${t('estimated')})` : '');
    return `<div class="card section"><div class="row"><h3>${esc(tier.name)}</h3>
        <span class="badge ${tier.favorGuess ? 'warn' : ''}" title="${esc(tier.favorSource)}">${esc(favor)}</span>
        ${tier.tooLow ? `<span class="badge bad">${esc(t('from level {n} only', { n: tier.level }))}</span>` : `<span class="muted small">${esc(t('from level {n}', { n: tier.level }))}</span>`}</div>
      <table><thead><tr>${th([t('Crate')], [t('empty crate'), 'num'], [t('cheapest bundle')], [t('Bundle'), 'num'], [t('Total'), 'num'],
        [t('per favor'), 'num'], [t('per favor (green)'), 'num'], [t('Note')])}</tr></thead>
        <tbody>${rows}</tbody></table></div>`;
  }).join('');
  const own = d.favor !== null && d.favor !== undefined
    ? `<div><div class="muted small">${esc(t("Your Merchant's Favor"))}</div><div class="big">${num(d.favor)}</div></div>` : '';
  return `<h2 class="section" id="crates">${esc(t('Trade crates'))}</h2>
    <div class="card"><div class="row"><div><h2>${esc(t("Cheapest Merchant's Favor"))}</h2>
        <div class="muted small">${esc(t('one empty crate + one bundle, handed in sealed at the trade representative'))}</div></div>
      <span class="spacer"></span>${own}</div>
      ${best ? `<table><thead><tr>${th([t('Crate')], [t('Bundle')], [t('Total'), 'num'], [t('Favor'), 'num'], [t('per favor'), 'num'])}</tr></thead><tbody>${best}</tbody></table>`
        : `<p class="muted">${esc(t('No crate with complete prices.'))}</p>`}</div>
    ${tiers}
    <p class="small muted">${esc(t("Crates pay no gold and no reputation, only Merchant's Favor. Total = empty crate (AH, 0 if you own it) + the cheapest bundle the AH can supply (own items 0). “Per favor” uses the common (white) sealed crate, the green one gives twice as much; what the quality depends on is not known. Favor per crate from urgametips.com/wow.gg (Nov 2026), Artisan estimated; from addon 1.5 on the addon measures every hand-in. Bundle amounts are checked against the game data. Elite crates are not included (content unknown)."))}</p>`;
}
