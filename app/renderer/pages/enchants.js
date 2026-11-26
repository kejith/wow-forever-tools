// Section "Enchants" of the Character page: permanent enchant recommendations per equipped slot,
// rated by the combat engine as net gain over the currently active enchant, with reagent/item costs
// from the AH and vendor prices.
import { api, cached, esc, help, hintAttr, itemIcon, money, num, quality, terms, th, tipAttr } from '../core.js';
import { t } from '../i18n.js';

export function loadEnchants(char) {
  return cached(`enchants:${char}`, () => api.bridge('enchants', { char, top: 5 }));
}

const signed = (n) => `${n < 0 ? '−' : '+'}${num(Math.abs(n), 1)}`;

const enchantTerms = (unit) => terms([
  [t('Gain'), t('Net {unit} change over the enchant currently on this item, evaluated by the combat model.', { unit })],
  [t('Cost'), t('Estimated reagent cost from the AH / vendor for Enchanting recipes, or AH price for item-applied enchants (armor kits, scopes). Hover for reagent details.')],
  [t('{unit}/gold', { unit }), t('Net gain per gold spent.')],
  [t('best value'), t('Highest gain per gold among priced upgrades for this slot.')],
], t('Special / proc enchants (e.g. Crusader, Fiery Weapon, Minor Speed) are listed separately because procs are not simulated as static stats yet.'));

function reagentHint(r) {
  if (!r.reagents?.length) return '';
  return r.reagents.map((rg) => {
    const p = rg.missing ? t('no AH price') : money(rg.totalPrice);
    return `${rg.count}× ${rg.name}: ${p}`;
  }).join('\n');
}

function costCell(r) {
  if (r.owned > 0) return `<td class="num"><span class="badge ok">${esc(t('{n} owned', { n: r.owned }))}</span></td>`;
  if (r.cost === null || r.cost === undefined) {
    const tip = reagentHint(r);
    return `<td class="num muted"${tip ? hintAttr(tip) : ''}>–</td>`;
  }
  const tip = reagentHint(r);
  return `<td class="num nowrap"${tip ? hintAttr(tip) : ''}>${money(r.cost)}</td>`;
}

function badges(r) {
  return [
    r.current ? `<span class="badge ok">${esc(t('equipped'))}</span>` : '',
    r.best ? `<span class="badge new">${esc(t('best'))}</span>` : '',
    r.bestValue ? `<span class="badge ok">${esc(t('best value'))}</span>` : '',
    r.canCraft ? `<span class="badge ok">${esc(t('you can craft it'))}</span>` : '',
    r.proc ? `<span class="badge warn">${esc(t('proc'))}</span>` : '',
  ].filter(Boolean).join(' ');
}

export function enchantsSection(d) {
  const unit = d.unit || 'DPS';
  const perGoldLabel = t('{unit}/gold', { unit });
  const maxGain = Math.max(1, ...(d.slots || []).flatMap((s) => (s.candidates || []).map((c) => c.gain || 0)));

  const weBox = d.weaponEnchants?.length
    ? `<p class="small muted" style="margin:10px 0 0">${esc(t('Active temporary weapon enchant:'))} ${d.weaponEnchants.map((w) => {
      const hand = w.hand === 'offHand' ? t('Off Hand') : t('Main Hand');
      const mins = w.remainingSec ? ` (${Math.ceil(w.remainingSec / 60)} min)` : '';
      return `${esc(hand)}: <b>${esc(w.name)}</b>${esc(mins)}`;
    }).join(' · ')}</p>` : '';

  const summary = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(t('{unit} now', { unit }))}</h2><div class="big">${num(d.baseValue, 1)}</div></div>
      <div><h2>${esc(t('With best enchants'))} ${help(enchantTerms(unit))}</h2>
        <div class="big good">${num(d.totalValue, 1)} <span class="small">(${signed(d.totalGain)} ${esc(unit)})</span></div></div>
      <div><h2>${esc(t('Enchanted slots'))}</h2><div class="big">${d.enchantedSlots}/${d.totalSlots}</div></div>
      <div><h2>${esc(t('Estimated cost'))}</h2><div class="big">${d.totalCost ? money(d.totalCost) : '–'}</div>
        ${d.unpricedCount ? `<div class="small muted">${esc(t('{n} without AH price', { n: d.unpricedCount }))}</div>` : ''}</div>
      <span class="spacer"></span></div>${weBox}</div>`;

  const slotsHtml = (d.slots || []).map((s) => {
    const it = s.item || {};
    const cur = s.current;
    const rows = (s.candidates || []).map((c) => `<tr>
        <td>${c.current ? `<span class="muted">0.0</span>`
          : `<div class="gain"><b>${signed(c.gain)}</b><div class="bar"><span style="width:${Math.round((Math.max(0, c.gain) / maxGain) * 100)}%"></span></div></div>`}</td>
        <td><div class="item-head"><span>${esc(c.name)}</span> ${badges(c)}</div>
          <div class="small muted">${esc(c.effect)}</div></td>
        ${costCell(c)}
        <td class="num muted">${c.perGold !== null && c.perGold !== undefined ? num(c.perGold, 1) : '–'}</td>
      </tr>`).join('');
    const table = rows
      ? `<table><thead><tr>${th([t('Gain')], [t('Enchant')], [t('Cost'), 'num'], [perGoldLabel, 'num'])}</tr></thead><tbody>${rows}</tbody></table>`
      : `<p class="small muted" style="margin:6px 0">${esc(t('No modeled stat enchant improves this slot.'))}</p>`;
    const specials = s.specials?.length
      ? `<div class="small muted" style="margin-top:8px"><b>${esc(t('Proc / special enchants:'))}</b> ${s.specials.map((sp) => {
        const p = sp.cost ? ` (${money(sp.cost)})` : '';
        return `<span class="badge"${reagentHint(sp) ? hintAttr(reagentHint(sp)) : ''}>${esc(sp.name)}${p}</span>`;
      }).join(' ')}</div>` : '';
    return `<div class="card section slot-card">
      <div><div class="slot-name">${esc(s.slotName)}</div>
        <div class="slot-current">
          <div class="small muted">${esc(t('Equipped'))}</div>
          <div class="small" style="margin-top:3px"><span class="item">${itemIcon(it.id, it.quality)}<span class="${quality(it.quality)}"${tipAttr(it.tip)}>${esc(it.name || '–')}</span></span></div>
          <div class="small ${cur ? 'good' : 'muted'}" style="margin-top:3px">${esc(cur ? cur.name : t('Not enchanted'))}</div>
        </div></div>
      <div>${table}${specials}</div></div>`;
  }).join('');

  return `${summary}${slotsHtml || `<div class="notice section">${esc(t('No enchantable items equipped.'))}</div>`}`;
}
