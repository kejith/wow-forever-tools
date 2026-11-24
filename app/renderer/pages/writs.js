// Page "Writs": the best craftsman's writ per tier by net silver per reputation, and the trade crates by
// silver per Merchant's Favor. Real tabs separate Writs from Trade Crates.
import { $, DASH, api, cached, content, errorBox, esc, help, helpAttr, hintAttr, itemIcon, loadStatus, loading, money, num, saveView, setTabs, state, terms, th } from '../core.js';
import { t } from '../i18n.js';

// Net cost: negative means the coin pouch pays more than the writ and item cost.
function netMoney(copper) {
  if (copper === null || copper === undefined) return money(copper);
  const c = Math.round(copper);
  return c < 0 ? `<span class="good">${t('{money} profit', { money: money(-c) })}</span>` : money(c);
}

// Copper per unit (reputation or favor) as silver with two decimals and the matching silver coin.
function perUnit(copper) {
  if (copper === null || copper === undefined) return DASH;
  return `<span class="money">${num(copper / 100, 2)}<span class="s">s</span></span>`;
}

// What the columns of the tier tables mean; the tables repeat, so the explanation sits at the heading once.
const writTerms = () => terms([
  [t('Purchase'), t('The writ plus the item it asks for, at the lowest buyout of the last scan. What you own counts as 0. Hover the price for the two parts.')],
  [t('Coin pouch'), t('The quest pays no gold itself, it gives a coin pouch. Values with ≈ are estimates; from addon 1.5 on the addon measures every pouch you open. Hover the value for its source.')],
  [t('Net'), t('Purchase minus the content of the pouch. Green: the pouch pays more than you spend.')],
  [t('per reputation'), t('Net in silver per point of reputation. Each tier is sorted by it, ★ is the cheapest.')],
  [t('in the AH'), t('Offers of the writ at the last scan.')],
], t('Writ list: wow.gg (Nov 2026).'));

const crateTerms = () => terms([
  [t('Total'), t('The empty crate from the AH (0 if you own one) plus the cheapest bundle the AH can supply in full (own items 0).')],
  [t('per favor'), t('Total divided by the favor of the common (white) sealed crate. The green one gives twice as much; what the quality depends on is not known.')],
  [t('Favor'), t('Per crate from urgametips.com/wow.gg (Nov 2026), Artisan estimated. From addon 1.5 on the addon measures every hand-in.')],
], t("Crates pay no gold and no reputation, only Merchant's Favor. Elite crates are not included (content unknown)."));

function activateWritTab(key) {
  state.writs.tab = key;
  for (const panel of content.querySelectorAll('[data-writ-panel]')) panel.hidden = panel.dataset.writPanel !== key;
  content.scrollTop = 0;
  saveView();
}

export async function renderWrits() {
  if (!state.status) await loadStatus();
  const o = state.writs;
  const activeTab = o.tab === 'crates' ? 'crates' : 'writs';
  if (state.page === 'writs') {
    setTabs([
      { key: 'writs', label: t("Craftsman's writs") },
      { key: 'crates', label: t('Trade crates') },
    ], activeTab, activateWritTab);
  }
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Suggestions per tier'))}<input type="number" id="w-top" min="1" max="10" value="${o.top}"></label>
      <label class="check"><input type="checkbox" id="w-own" ${o.ownedAtAhPrice ? 'checked' : ''}> ${esc(t('count own items at the AH price'))}
        ${help(esc(t('Items you already own normally count as 0. Tick this to count them at the AH price: what you give up by not selling them.')))}</label>
      <span class="spacer"></span><div class="page-meta" id="w-meta"></div>
    </div><div id="w-body">${loading(t('Calculating writs …'))}</div>`;
  $('#w-top').addEventListener('change', (e) => { o.top = Number(e.target.value) || 3; renderWrits(); });
  $('#w-own').addEventListener('change', (e) => { o.ownedAtAhPrice = e.target.checked; renderWrits(); });
  const key = `writs:${state.char}:${o.top}:${o.ownedAtAhPrice}`;
  const d = await cached(key, () => api.bridge('writs', { char: state.char, top: o.top, ownedAtAhPrice: o.ownedAtAhPrice }));
  const body = $('#w-body');
  if (!body) return;
  if (d.error) { body.innerHTML = errorBox(d.error); return; }
  $('#w-meta').textContent = t('AH scan {date}', { date: d.scanDate });
  const rep = d.reputation;
  const repHtml = rep ? `<div><h3>${esc(rep.name)}</h3><div class="muted">${esc(t('{standing} · {value} / {max} · {left} to the next standing',
        { standing: rep.standing, value: rep.value, max: rep.max, left: rep.max - rep.value }))}</div>
      <div class="bar" style="margin-top:10px"><span style="width:${Math.round((rep.value / rep.max) * 100)}%"></span></div></div>` : '';
  const tiers = d.tiers.map((tier) => {
    const rows = tier.best.map((b, i) => `<tr class="${i === 0 ? 'best' : ''}">
        <td><span class="item">${itemIcon(b.writId, 1)}<span>${i === 0 ? '★ ' : ''}${esc(b.name)}${b.qty > 1 ? ` <span class="muted">(${b.qty}x)</span>` : ''}
          <div class="sub">${esc([b.prof, b.note].filter(Boolean).join(' · '))}</div></span></span></td>
        <td class="num"><span${helpAttr(`${esc(t('Writ'))} ${money(b.wcost)} + ${esc(t('Item'))} ${money(b.icost)}`)}>${money(b.cost)}</span></td>
        <td class="num"><span class="good"${hintAttr(b.payoutSource)}>${b.payoutMeasured ? '' : '≈ '}−${money(b.payout)}</span></td>
        <td class="num"><b>${netMoney(b.net)}</b></td>
        <td class="num">${perUnit(b.per)}</td>
        <td class="num muted">${b.writs}</td></tr>`).join('');
    const sealed = tier.sealed ? `<p class="small notice">${esc(t('You have {n}x “Sealed Writ” of this tier. Open it first, it may be cheaper.', { n: tier.sealed }))}</p>` : '';
    return `<div class="card section"><h3>${esc(tier.name)}</h3>
      ${rows ? `<table><thead><tr>${th([t('Writ')], [t('Purchase'), 'num'], [t('Coin pouch'), 'num'],
        [t('Net'), 'num'], [t('per reputation'), 'num'], [t('in the AH'), 'num'])}</tr></thead><tbody>${rows}</tbody></table>`
        : `<p class="muted">${esc(t('No writ with price data.'))}</p>`}${sealed}</div>`;
  }).join('');
  const writsPanel = `<div class="tab-panel" data-writ-panel="writs" id="writs-panel"${activeTab === 'writs' ? '' : ' hidden'}>
      <div class="card writ-summary">${repHtml}
        <div><h2>${esc(t('Per day'))}</h2><div class="big">${netMoney(d.dailyNet)}</div>
          <div class="muted small">${t('net after coin pouches · {money} to buy', { money: money(d.dailyTotal) })}</div>
          <div class="muted small">${esc(t('for {rep} reputation (the best writ of each tier)', { rep: d.dailyRep }))}</div></div></div>
      <h2 class="section heading">${esc(t('Best writs per tier'))} ${help(writTerms())}</h2>
      ${tiers}
    </div>`;
  const cratesPanel = `<div class="tab-panel" data-writ-panel="crates" id="crates"${activeTab === 'crates' ? '' : ' hidden'}>${cratesHtml(d)}</div>`;
  body.innerHTML = `${writsPanel}${cratesPanel}`;
}

function cratesHtml(d) {
  const c = d.crates;
  if (!c) return `<p class="muted">${esc(t('No crate with complete prices.'))}</p>`;
  const bundle = (o) => (o ? `<span class="item">${itemIcon(o.id, 1, 'sm')}<span>${o.qty}x ${esc(o.name)}</span></span>` : DASH);
  const alternatives = (r) => {
    const alts = r.options.filter((o) => o !== r.best && r.best && o.id !== r.best.id);
    if (!alts.length) return '';
    return `<div class="alt-chips">${alts.map((o) => `<span class="alt-chip">${o.qty}x ${esc(o.name)} ${
      o.cost === null ? `<span class="muted">${esc(t('no price'))}</span>` : money(o.cost)
    }${o.enough ? '' : ` <span class="warn-text">(${esc(t('too few in the AH'))})</span>`}</span>`).join('')}</div>`;
  };
  const best = c.best.map((r, i) => `<tr class="${i === 0 ? 'best' : ''}">
      <td><span class="item">${itemIcon(r.crateId, 1)}<span>${i === 0 ? '★ ' : ''}${esc(r.kind)} <span class="muted">(${esc(r.tierName)})</span></span></span></td>
      <td>${bundle(r.best)}</td><td class="num"><b>${money(Math.round(r.cost))}</b></td>
      <td class="num">${num(r.favor)}</td><td class="num">${perUnit(r.per)}</td></tr>`).join('');
  const tiers = c.tiers.map((tier) => {
    const rows = tier.rows.map((r, i) => {
      const green = r.perGreen === null || r.perGreen === undefined ? '' : hintAttr(t('with the green crate: {value} s', { value: num(r.perGreen / 100, 2) }));
      return `<tr class="${i === 0 && r.complete ? 'best' : ''}">
        <td><span class="item"${hintAttr(r.name)}>${itemIcon(r.crateId, 1)}<span>${i === 0 && r.complete ? '★ ' : ''}${esc(r.kind)}
          ${r.note ? `<div class="sub">${esc(r.note)}</div>` : ''}</span></span></td>
        <td class="num">${r.crateCost === null ? `<span class="muted">${esc(t('not in the AH'))}</span>` : money(r.crateCost)}</td>
        <td>${bundle(r.best)}${alternatives(r)}</td>
        <td class="num">${r.best ? money(r.best.cost) : DASH}</td>
        <td class="num"><b>${r.complete ? money(Math.round(r.cost)) : DASH}</b></td>
        <td class="num"><span${green}>${perUnit(r.per)}</span></td></tr>`;
    }).join('');
    const favor = t('{favor} favor · green {green}', { favor: num(tier.favor), green: num(tier.favorGreen) }) + (tier.favorGuess ? ` (${t('estimated')})` : '');
    return `<div class="card section"><div class="row"><h3>${esc(tier.name)}</h3>
        <span class="badge ${tier.favorGuess ? 'warn' : ''}"${hintAttr(tier.favorSource)}>${esc(favor)}</span>
        ${tier.tooLow ? `<span class="badge bad">${esc(t('from level {n} only', { n: tier.level }))}</span>` : `<span class="muted small">${esc(t('from level {n}', { n: tier.level }))}</span>`}</div>
      <table><thead><tr>${th([t('Crate')], [t('empty crate'), 'num'], [t('cheapest bundle')], [t('Bundle'), 'num'], [t('Total'), 'num'],
        [t('per favor'), 'num'])}</tr></thead>
        <tbody>${rows}</tbody></table></div>`;
  }).join('');
  const own = d.favor !== null && d.favor !== undefined
    ? `<div style="text-align:right"><div class="muted small">${esc(t("Your Merchant's Favor"))}</div><div class="big">${num(d.favor)}</div></div>` : '';
  return `<div class="card"><div class="row" style="margin-bottom:10px"><div><h3>${esc(t("Cheapest Merchant's Favor"))} ${help(crateTerms())}</h3>
        <div class="muted small">${esc(t('one empty crate + one bundle, handed in sealed at the trade representative'))}</div></div>
      <span class="spacer"></span>${own}</div>
      ${best ? `<table><thead><tr>${th([t('Crate')], [t('Bundle')], [t('Total'), 'num'], [t('Favor'), 'num'], [t('per favor'), 'num'])}</tr></thead><tbody>${best}</tbody></table>`
        : `<p class="muted">${esc(t('No crate with complete prices.'))}</p>`}</div>
    ${tiers}`;
}
