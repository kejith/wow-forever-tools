// Page "Upgrades": two tabs — "AH upgrades" (auction house & owned gear against equipped gear)
// and "BiS list" (top N best-in-slot items per equipment slot, filterable by source category & level).
import { $, ago, api, cached, content, errorBox, esc, help, hintAttr, itemIcon, loadStatus, loading, money, num, percent, quality, saveView, selectOption, setTabs, state, terms, th, tipAttr } from '../core.js';
import { mark, t } from '../i18n.js';

// What the columns of the AH slot cards mean; the cards repeat, so the explanation sits at the heading once.
const slotTerms = (engine, unit) => terms([
  [t('Gain'), engine ? t('Change of your {unit} in the simulated fight when you swap the item in.', { unit })
    : t('Change of the gear value when you swap the item in, in points of the scale.')],
  [t('vs. item'), engine ? t('The gain relative to the {unit} the replaced item contributes.', { unit })
    : t('The gain relative to the points of the item it replaces.')],
  [t('Price'), t('Lowest buyout at the last scan, not a live offer. Gear you already have costs nothing: the row says where it is (your bags, your bank, or unbound on another character).')],
  [engine ? t('{unit}/gold', { unit }) : t('Points/gold'), t('Gain per gold spent.')],
], t('Auctionator cannot tell items with random stats (“of Shadow Wrath”) apart, so they are missing here.'));

const bisTerms = (engine, unit) => terms([
  [t('#'), t('Rank of this item for the slot among the active source categories.')],
  [t('Gain'), engine ? t('Change of your {unit} when you swap this item in against what you wear now. Negative values mean your currently equipped item is already stronger.', { unit })
    : t('Change of your gear value against what you wear now. Negative values mean your currently equipped item is already stronger.')],
  [t('vs. item'), engine ? t('The gain relative to the {unit} the replaced item contributes.', { unit })
    : t('The gain relative to the points of the item it replaces.')],
  [t('Source'), t('Where the item drops, is rewarded, or is crafted. Quests you already completed are marked when you do not own the item.')],
], t('Items with random suffixes (“of Shadow Wrath”) are not listed. Items with strong unmodeled On-Use or Proc effects appear under “Special effects” below the slot table.'));

const BIS_SOURCE_DEFS = [
  { key: 'quest', label: mark('Quests'), primary: true },
  { key: 'dungeon', label: mark('Dungeons'), primary: true },
  { key: 'raid', label: mark('Raids'), primary: true },
  { key: 'crafted', label: mark('Professions'), primary: true },
  { key: 'vendor', label: mark('Vendor'), primary: false },
  { key: 'reputation', label: mark('Reputation'), primary: false },
  { key: 'pvp', label: mark('PvP'), primary: false },
  { key: 'world', label: mark('World drops'), primary: false },
];

const signed = (n) => `${n < 0 ? '−' : '+'}${num(Math.abs(n), 1)}`;
const pctOf = (c) => (c.gainPct === null || c.gainPct === undefined ? esc(t('new')) : `${c.gainPct < 0 ? '−' : '+'}${percent(Math.abs(c.gainPct))}`);
const priceCell = (c) => (c.price ? money(c.price) : `<span class="badge ok">${esc(c.where || t('owned'))}</span>`);
const perGoldCell = (c) => (c.price ? num(c.perGold, 1) : '–');

function setBadges(c) {
  return (c.setChanges || []).map((sc) => {
    const label = sc.kind === 'gain' ? t('+{set} ({n})', { set: sc.set, n: sc.threshold }) : t('−{set} ({n})', { set: sc.set, n: sc.threshold });
    const tip = sc.bonus ? (sc.kind === 'gain' ? t('Activates set bonus: {bonus}', { bonus: sc.bonus }) : t('Breaks set bonus: {bonus}', { bonus: sc.bonus })) : '';
    return `<span class="badge ${sc.kind === 'gain' ? 'ok' : 'warn'}"${tip ? hintAttr(tip) : ''}>${esc(label)}</span>`;
  }).join(' ');
}

function enchBadge(c) {
  return c.replacesEnchants?.length
    ? `<span class="badge warn"${hintAttr(t('Replaces enchanted item ({enchants}). Comparison is base stats vs. base stats — remember to enchant the new item.', { enchants: c.replacesEnchants.map((e) => e.name).join(', ') }))}>${esc(t('re-enchant'))}</span>`
    : '';
}

function renderModelDetails(d, eng) {
  const scale = `<details class="scale"><summary>${eng
    ? `${esc(d.weights)} — ${esc(t('show which spell contributes what'))}`
    : esc(t('Scale “{name}”: 1 point = 1 shadow spell power. Show the weights and why.', { name: d.profileLabel }))}</summary>
      <table><tbody>${d.scale.map((r) => `<tr><td>${esc(r.label)}</td><td class="num">${num(r.weight)}</td>
        <td class="small muted">${esc(r.note)}</td></tr>`).join('')}</tbody></table></details>`;
  const healRanks = eng?.healOptions?.length ? `<details class="scale"><summary>${esc(t('Heal ranks: what each rank sustains — the model heals with the top row'))}</summary>
      <table><thead><tr>${th([t('Spell')],
    [t('HPS in the fight'), 'num', esc(t('What the whole rotation sustains over the fight when this rank fills it — the number the rank choice uses. Mana, Renew and the fight length are all in here.'))],
    [t('Healing/mana'), 'num', esc(t('Healing one point of mana buys. Downranking trades cast speed for this efficiency.'))],
    [t('Healing/cast'), 'num'], [t('Mana'), 'num'], [t('Cast (s)'), 'num'])}</tr></thead><tbody>
      ${eng.healOptions.map((o) => `<tr><td>${esc(o.spell)}${o.chosen ? ` <span class="badge new">${esc(t('the model heals with this'))}</span>` : ''}${o.bonusLossPct ? ` <span class="badge warn"${hintAttr(t('You have outgrown this rank: the game gives it only part of your healing bonus. The numbers in this row already have that loss.'))}>${esc(t('{pct}% less bonus', { pct: num(o.bonusLossPct) }))}</span>` : ''}</td>
        <td class="num"><b>${num(o.hpsSustained, 1)}</b></td><td class="num">${num(o.healPerMana, 2)}</td>
        <td class="num">${num(o.healPerCast)}</td><td class="num">${num(o.manaPerCast)}</td><td class="num">${num(o.castSec, 1)}</td></tr>`).join('')}</tbody></table></details>` : '';
  const cov = eng?.coverage;
  const covBadge = { used: 'ok', sheet: 'ok', unmodeled: 'warn' };
  const covRows = (rows) => {
    const quiet = rows.filter((e) => e.status === 'none');
    return `<table><tbody>${rows.filter((e) => e.status !== 'none').map((e) => `<tr>
        <td><span class="badge ${covBadge[e.status] || ''}">${esc(e.statusLabel)}</span></td><td>${esc(e.label)}</td>
        <td class="small muted">${esc(e.text)}${e.unverified ? ` <span class="badge warn"${hintAttr(e.note)}>${esc(t('assumption'))}</span>` : ''}</td></tr>`).join('')}
      ${quiet.length ? `<tr><td><span class="badge">${esc(quiet[0].statusLabel)}</span></td>
        <td colspan="2" class="small muted">${esc(quiet.map((e) => e.label).join(', '))}</td></tr>` : ''}</tbody></table>`;
  };
  const check = eng?.tooltipCheck;
  const tipCheck = check ? `<h3>${esc(t('Checked against the game'))} ${help(esc(t('The spell texts in your spellbook hold the numbers of your character: spell power, talents and the loss of an outgrown rank are in them. For every rank of the spells above, the model computes the same numbers; here is how many agree. With WoW Tools Forever 1.12 or newer the mana cost and the cast time of every rank are compared as well. A rank that differs means the model misses something for it.')))}</h3>
      <p class="small ${check.off.length ? '' : 'muted'}">${esc(check.text)}</p>
      ${check.off.length ? `<table><tbody>${check.off.map((o) => `<tr><td><span class="badge warn">${esc(t('differs'))}</span></td>
        <td>${esc(o.label)}</td><td class="small muted">${esc(o.model)}</td><td class="small">${esc(o.text)}</td></tr>`).join('')}</tbody></table>` : ''}` : '';
  const covCount = (status) => (cov ? [...cov.spells, ...cov.talents, ...(cov.setBonuses || [])].filter((e) => e.status === status).length : 0);
  const coverage = cov ? `<details class="scale coverage"><summary>${esc(t('What the model does with your spells and talents: {used} used, {open} not modeled, {n} rotations compared',
    { used: covCount('used'), open: covCount('unmodeled'), n: cov.candidates }))}</summary>
      ${cov.exactTalents ? '' : `<div class="notice" style="margin-top:8px">${esc(t('The talent tables of the game could not be loaded: talent values are estimates.'))}</div>`}
      <h3>${esc(t('Spells'))}</h3>${covRows(cov.spells)}<h3>${esc(t('Talents'))}</h3>${covRows(cov.talents)}${cov.setBonuses?.length ? `<h3>${esc(t('Set bonuses'))}</h3>${covRows(cov.setBonuses)}` : ''}${tipCheck}</details>` : '';
  return `${scale}${eng?.downrank?.text ? `<div class="notice" style="margin-top:10px">${esc(eng.downrank.text)}</div>` : ''}${eng?.consumables ? `<div class="notice" style="margin-top:10px">${esc(eng.consumables.text)}</div>` : ''}${eng?.buffs ? `<p class="small muted" style="margin:8px 0 0">${esc(eng.buffs.text)}</p>` : ''}${healRanks}${coverage}`;
}

function buildFightControls(o, prefix) {
  return o.engine === 'off'
    ? `<label>${esc(t('Scale'))}<select id="${prefix}-profile">${selectOption('', t('automatic'), o.profile)}${selectOption('leveling', t('Leveling'), o.profile)}${selectOption('dungeon', t('Dungeon/raid'), o.profile)}</select></label>`
    : `<label><span class="label">${esc(t('Fight length (s)'))} ${help(esc(t('The combat model computes DPS over a fight of this length. A longer fight makes mana, regeneration and Life Tap matter more.')))}</span>
        <input type="number" id="${prefix}-fight" min="10" max="600" step="10" value="${o.fight}"></label>
      <label>${esc(t('Enemy level'))}<select id="${prefix}-target">${selectOption('', t('automatic (+2/+3)'), o.target)}${[0, 1, 2, 3].map((n) => selectOption(String(n), `+${n}`, o.target)).join('')}</select></label>
      <label id="${prefix}-metric" style="display:none"></label>
      <label id="${prefix}-kills" style="display:none"></label>
      <label class="check"><input type="checkbox" id="${prefix}-cons" ${o.consumables ? 'checked' : ''}> <span class="label">${esc(t('with consumables'))} ${help(esc(t('Rates the gear with the best set of consumables active (the page “Consumables” shows it). A flask’s spell power makes hit and crit worth more, a potion’s mana makes intellect worth less.')))}</span></label>`;
}

function populateDynamicEngineControls(eng, o, prefix, rerenderFn) {
  const bind = (id, field, parse) => { const el = $(id); if (el) el.addEventListener('change', (e) => { o[field] = parse(e.target.value); rerenderFn(); }); };
  if (eng?.metrics?.length > 1) {
    const slot = $(`#${prefix}-metric`);
    if (slot) {
      const names = { dps: t('DPS (damage)'), hps: t('HPS (healing)'), ttoom: t('Mana lasts (s)') };
      slot.innerHTML = `${esc(t('Metric'))}<select id="${prefix}-metric-sel">${selectOption('', t('automatic'), o.metric)}${eng.metrics.map((m) => selectOption(m, names[m] || m, o.metric)).join('')}</select>`;
      slot.style.display = '';
      bind(`#${prefix}-metric-sel`, 'metric', (v) => v);
    }
  }
  if (eng?.killBuff) {
    const slot = $(`#${prefix}-kills`);
    if (slot) {
      slot.innerHTML = `<span class="label">${esc(t('Kills per minute'))} ${help(esc(t('Your killing blows per minute, for talents that work after a kill (Spirit Tap). Automatic: one per fight for a damage rotation against normal enemies, none for a healer or against a boss.')))}</span>
        <select id="${prefix}-kills-sel">${selectOption('', t('automatic'), o.kills)}${[0, 1, 2, 3, 4, 6].map((n) => selectOption(String(n), String(n), o.kills)).join('')}</select>`;
      slot.style.display = '';
      bind(`#${prefix}-kills-sel`, 'kills', (v) => v);
    }
  }
}

export async function renderUpgrades() {
  if (!state.status) await loadStatus();
  const o = state.upgrades;
  const activeTab = o.tab === 'bis' ? 'bis' : 'ah';
  setTabs([
    { key: 'ah', label: t('AH upgrades') },
    { key: 'bis', label: t('BiS list'), meta: t('Top {n}', { n: o.bisTop || 10 }) },
  ], activeTab, (key) => {
    o.tab = key;
    saveView();
    renderUpgrades();
  });
  if (activeTab === 'bis') {
    await renderBiSTab(o);
  } else {
    await renderAHTab(o);
  }
}

async function renderAHTab(o) {
  const fightControls = buildFightControls(o, 'u');
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Budget (gold)'))}<input type="number" id="u-max" class="wide" min="0" step="0.5" value="${esc(o.maxPrice)}" placeholder="${esc(t('your own gold'))}"></label>
      <label><span class="label">${esc(t('Rating'))} ${help(esc(t('The combat model simulates your rotation over the fight; when it cannot run, the weight scale answers instead. Monte-Carlo plays the rotation with real rolls: slower, adds the spread and replays the biggest gains. The weight scale rates stats with fixed weights.')))}</span>
        <select id="u-engine">${selectOption('auto', t('Combat model'), o.engine)}${selectOption('mc', t('Combat model (Monte-Carlo)'), o.engine)}${selectOption('off', t('Weight scale'), o.engine)}</select></label>${fightControls}
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
  $('#u-cons')?.addEventListener('change', (e) => { o.consumables = e.target.checked; renderUpgrades(); });
  const withConsumables = o.consumables && o.engine !== 'off';
  const key = `upgrades:${state.char}:${o.maxPrice}:${o.preview}:${o.top}:${o.profile}:${o.sort}:${o.engine}:${o.fight}:${o.target}:${o.metric}:${o.kills}:${withConsumables}`;
  const d = await cached(key, () => api.bridge('upgrades', {
    char: state.char, maxPrice: o.maxPrice ? Number(o.maxPrice) : 0, preview: o.preview, top: o.top,
    profile: o.profile, sort: o.sort, engine: o.engine === 'mc' ? 'auto' : o.engine,
    method: o.engine === 'mc' ? 'montecarlo' : '', fightSeconds: Number(o.fight) || 60,
    targetDelta: o.target === '' ? -1 : Number(o.target), metric: o.metric,
    killsPerMinute: o.kills === '' ? -1 : Number(o.kills), consumables: withConsumables,
  }));
  const body = $('#u-body');
  if (!body || state.upgrades.tab === 'bis') return;
  if (d.error) {
    body.innerHTML = `${errorBox(d.error)}<div class="notice">${esc(t('Scan the auction house with Auctionator and close it: the addon then reads new items by itself. Or request an item export on the “Connection” page.'))}</div>`;
    return;
  }
  const eng = d.engine;
  const unit = eng ? ({ dps: 'DPS', hps: 'HPS', ttoom: 's' })[eng.metric] || 'DPS' : '';
  const perGoldLabel = eng ? t('{unit}/gold', { unit }) : t('Points/gold');
  populateDynamicEngineControls(eng, o, 'u', renderUpgrades);

  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => c.gain)));
  const itemCell = (c, compare) => `<div class="item-cell">${itemIcon(c.id, c.quality)}<div class="item-body">
          <div class="item-head"><span class="${quality(c.quality)}"${tipAttr(c.tip, compare)}>${esc(c.name)}</span>
            ${c.isNew ? `<span class="badge forever">${esc(t('NEW in Forever'))}</span>` : ''}
            ${c.soon ? `<span class="badge warn">${esc(t('from level {n}', { n: c.reqLevel }))}</span>` : ''}
            ${c.twoHand ? `<span class="badge">${esc(t('Two-hand, also replaces the off hand'))}</span>` : ''}
            ${setBadges(c)}${enchBadge(c)}</div>
          <div class="small muted">${esc(c.stats)}</div>
          ${c.effects.map((text) => `<div class="effects">${esc(text)}</div>`).join('')}</div></div>`;
  const activeSlot = d.slots.some((s) => s.slotName === o.slot) ? o.slot : '';
  const visibleSlots = activeSlot ? d.slots.filter((s) => s.slotName === activeSlot) : d.slots;
  const slotPills = d.slots.length > 1 ? `<div class="slot-pills">
      <button type="button" class="pill-btn${activeSlot === '' ? ' active' : ''}" data-u-slot="">${esc(t('All slots'))} <span class="count">${d.slots.length}</span></button>
      ${d.slots.map((s) => `<button type="button" class="pill-btn${activeSlot === s.slotName ? ' active' : ''}" data-u-slot="${esc(s.slotName)}">${esc(s.slotName)} <span class="count">${s.candidates.length}</span></button>`).join('')}
    </div>` : '';
  const slots = visibleSlots.map((s) => {
    const cur = s.current;
    const rows = s.candidates.map((c) => `<tr>
        <td><div class="gain"><b>${signed(c.gain)}</b><div class="bar"><span style="width:${Math.round((c.gain / maxGain) * 100)}%"></span></div></div></td>
        <td class="num muted">${pctOf(c)}</td>
        <td class="num">${priceCell(c)}</td>
        <td class="num muted">${perGoldCell(c)}</td>
        <td>${itemCell(c, cur?.tip)}</td></tr>`).join('');
    return `<div class="card section slot-card">
      <div><div class="slot-name">${esc(s.slotName)}</div>
        <div class="slot-current">
          <div class="small muted">${esc(t('Equipped'))}</div>
          <div class="small" style="margin-top:3px"><span class="item">${itemIcon(cur?.id, cur?.quality)}<span class="${quality(cur?.quality)}"${tipAttr(cur?.tip)}>${esc(cur?.name || '–')}</span></span></div>
          <div class="small muted" style="margin-top:2px">${esc(cur?.stats || '')}</div>
          ${cur?.enchant ? `<div class="small good" style="margin-top:2px">${esc(cur.enchant.name)}</div>` : ''}
        </div></div>
      <table><thead><tr>${th([t('Gain')], [t('vs. item'), 'num'], [t('Price'), 'num'], [perGoldLabel, 'num'], [t('Item')])}</tr></thead><tbody>${rows}</tbody></table></div>`;
  }).join('');
  const pk = d.package;
  const packageHtml = pk.items.length ? `<div class="section"><h2 class="heading">${t('Shopping list: best package for {money}', { money: money(d.budget) })}
      ${help(esc(t('Best combination across all slots that fits the budget (rings as a pair, main hand + off hand against two-hand, no items above your level). Sorted by gain per gold: buy from top to bottom and stop where it gets too expensive for you. Gear you already have comes first and costs nothing.')))}</h2><div class="card">
      <div class="row" style="gap:36px;align-items:flex-start;margin-bottom:10px">
        <div><h2>${esc(t('Gain'))}</h2><div class="big good">${signed(pk.gain)}${pk.gainPct === null ? ''
          : ` <span class="small muted">(${esc(eng ? t('+{pct} {unit}', { pct: percent(pk.gainPct), unit }) : t('+{pct} gear value', { pct: percent(pk.gainPct) }))})</span>`}</div></div>
        <div><h2>${esc(t('Cost'))}</h2><div class="big">${money(pk.cost)}</div></div>
        <div><h2>${esc(t('Purchases'))}</h2><div class="big">${pk.items.filter((c) => c.price).length}</div></div>
        ${pk.items.some((c) => !c.price) ? `<div><h2>${esc(t('Already yours'))}</h2><div class="big">${pk.items.filter((c) => !c.price).length}</div></div>` : ''}<span class="spacer"></span></div>
      <table><thead><tr>${th([t('Slot')], [t('Gain'), 'num'], [t('Price'), 'num'], [perGoldLabel, 'num'], [t('Item')], [t('replaces')],
        [t('up to here'), 'num', esc(t('Cost and gain in total if you buy from the top down to this row.'))])}</tr></thead><tbody>
      ${pk.items.map((c) => `<tr><td class="muted">${esc(c.slotName)}</td>
        <td class="num"><b class="${c.gain < 0 ? 'warn-text' : 'good'}">${signed(c.gain)}</b>${c.gain < 0 ? `<div class="small muted"${hintAttr(t('Only pays together with its paired main hand or ring further down in the list (replaces a two-hander on its own).'))}>${esc(t('paired swap'))}</div>` : ''}</td>
        <td class="num">${priceCell(c)}</td><td class="num muted">${perGoldCell(c)}</td>
        <td>${itemCell(c, c.compareTip)}</td><td class="small muted">${esc(c.replaces.join(' + ') || '–')}</td>
        <td class="num small">${money(c.cumCost)}<div class="muted">${signed(c.cumGain)}</div></td></tr>`).join('')}</tbody></table>
      </div></div>` : '';
  const review = d.review.length ? `<div class="section"><h2>${esc(t('With a special effect – judge for yourself'))}</h2><div class="card"><table>
      <thead><tr>${th([t('Slot')], [t('Price'), 'num'], [t('Item')], [t('Effect')])}</tr></thead><tbody>
      ${d.review.map((r) => `<tr><td class="muted">${esc(r.slot)}</td><td class="num">${priceCell(r)}</td>
        <td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}"${tipAttr(r.tip)}>${esc(r.name)}</span></span> ${r.isNew ? `<span class="badge new">${esc(t('NEW'))}</span>` : ''}</td>
        <td class="small">${esc(r.effects.join(' / '))}</td></tr>`).join('')}</tbody></table></div></div>` : '';
  const unknown = d.owned?.unknown || [];
  const unknownNote = unknown.length ? `<div class="notice" style="margin-top:10px"${hintAttr(unknown.map((u) => `${u.name} (${u.where})`).join(', '))}>${esc(t('{n} items you own are only known by name and cannot be rated: open the bank once with WoW Tools Forever 1.12 or newer, then /reload.', { n: unknown.length }))}</div>` : '';
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(t('Budget'))}</h2><div class="big">${money(d.budget)}</div></div>
      <div><h2>${esc(eng ? (eng.metric === 'ttoom' ? t('Mana lasts (s)') : t('{unit} now', { unit })) : t('Gear value'))} ${help(esc(eng
    ? t('The metric of the simulated fight with the gear you wear now. Every gain below is the change of this number.')
    : t('Everything you wear, rated as one set in points of the scale. Hit counts only up to the cap, so the value of an item depends on the rest of the gear.')))}</h2>
        <div class="big">${num(d.setValue)}</div>${eng?.mc
    ? `<div class="small muted">${esc(t('spread ±{sd}, worst tenth {p10}', { sd: num(eng.mc.stdDev, 1), p10: num(eng.mc.p10, 1) }))}</div>` : ''}</div>
      <div><h2>${esc(t('AH scan'))}</h2><div class="big">${esc(d.scanDate)}</div></div>
      <div><h2>${esc(t('Item export'))} ${help(esc(t('Items of the last AH scans whose stats the WoW Tools Forever addon has read in the game. Only these can be compared.')))}</h2>
        <div class="big">${num(d.exportCount)}</div><div class="small muted">${esc(ago(d.exportUpdated))}</div></div>
      <span class="spacer"></span></div>
      ${unknownNote}${renderModelDetails(d, eng)}</div>
    ${packageHtml}
    ${slots ? `<h2 class="section heading">${esc(t('All slots'))} ${help(slotTerms(eng, unit))}</h2>${slotPills}${slots}` : `<div class="notice section">${esc(t('No upgrades within this budget.'))}</div>`}${review}`;
}

async function renderBiSTab(o) {
  const fightControls = buildFightControls(o, 'b');
  const lvlMode = o.bisLevel === '60' ? '60' : 'current';
  const topN = Math.min(25, Math.max(1, Number(o.bisTop) || 10));
  const sourcesStr = o.bisSources || 'quest,dungeon,raid,crafted';
  const previewCtrl = lvlMode === 'current'
    ? `<label><span class="label">${esc(t('Preview (levels)'))} ${help(esc(t('Also lists items you can equip within this many levels. They are marked “from level …”.')))}</span>
        <input type="number" id="b-pre" min="0" max="10" value="${o.preview}"></label>`
    : '';
  content.innerHTML = `<div class="controls">
      <label><span class="label">${esc(t('Level'))} ${help(esc(t('Switch between items usable around your character’s current level or full Level 60 endgame BiS.'))) }</span>
        <select id="b-lvl">${selectOption('current', t('Current level'), lvlMode)}${selectOption('60', t('Level 60'), lvlMode)}</select></label>${previewCtrl}
      <label><span class="label">${esc(t('Rating'))} ${help(esc(t('The combat model simulates your rotation over the fight; when it cannot run, the weight scale answers instead. Monte-Carlo plays the rotation with real rolls: slower, adds the spread and replays the biggest gains. The weight scale rates stats with fixed weights.')))}</span>
        <select id="b-engine">${selectOption('auto', t('Combat model'), o.engine)}${selectOption('mc', t('Combat model (Monte-Carlo)'), o.engine)}${selectOption('off', t('Weight scale'), o.engine)}</select></label>${fightControls}
      <label>${esc(t('Items per slot'))}<input type="number" id="b-top" min="1" max="25" value="${topN}"></label>
    </div><div id="b-body">${loading(t('Ranking BiS items for your character …'))}</div>`;

  const bind = (id, field, parse) => { const el = $(id); if (el) el.addEventListener('change', (e) => { o[field] = parse(e.target.value); saveView(); renderUpgrades(); }); };
  bind('#b-lvl', 'bisLevel', (v) => v);
  bind('#b-pre', 'preview', (v) => Math.min(10, Math.max(0, Number(v) || 0)));
  bind('#b-engine', 'engine', (v) => v);
  bind('#b-profile', 'profile', (v) => v);
  bind('#b-fight', 'fight', (v) => Math.min(600, Math.max(10, Number(v) || 60)));
  bind('#b-target', 'target', (v) => v);
  bind('#b-top', 'bisTop', (v) => Math.min(25, Math.max(1, Number(v) || 10)));
  $('#b-cons')?.addEventListener('change', (e) => { o.consumables = e.target.checked; saveView(); renderUpgrades(); });

  const withConsumables = o.consumables && o.engine !== 'off';
  const key = `bis:${state.char}:${lvlMode}:${o.preview}:${topN}:${sourcesStr}:${o.profile}:${o.engine}:${o.fight}:${o.target}:${o.metric}:${o.kills}:${withConsumables}`;
  const d = await cached(key, () => api.bridge('bis', {
    char: state.char,
    levelMode: lvlMode,
    preview: o.preview,
    top: topN,
    sources: sourcesStr,
    profile: o.profile,
    engine: o.engine === 'mc' ? 'auto' : o.engine,
    method: o.engine === 'mc' ? 'montecarlo' : '',
    fightSeconds: Number(o.fight) || 60,
    targetDelta: o.target === '' ? -1 : Number(o.target),
    metric: o.metric,
    killsPerMinute: o.kills === '' ? -1 : Number(o.kills),
    consumables: withConsumables,
  }));
  const body = $('#b-body');
  if (!body || state.upgrades.tab !== 'bis') return;
  if (d.error) {
    body.innerHTML = errorBox(d.error);
    return;
  }

  const eng = d.engine;
  const unit = eng ? ({ dps: 'DPS', hps: 'HPS', ttoom: 's' })[eng.metric] || 'DPS' : '';
  populateDynamicEngineControls(eng, o, 'b', renderUpgrades);

  const activeSet = new Set(d.sources || []);
  const srcCounts = d.sourceCounts || {};
  const sourcePills = `<div class="bis-filters">
      <div class="bis-filter-group">
        <span class="small muted">${esc(t('Sources:'))}</span>
        ${BIS_SOURCE_DEFS.map((def) => {
          const on = activeSet.has(def.key);
          const cnt = srcCounts[def.key] ?? 0;
          return `<button type="button" class="pill-btn src-pill src-${def.key}${on ? ' active' : ''}" data-bis-src="${def.key}">${esc(t(def.label))} <span class="count">${cnt}</span></button>`;
        }).join('')}
      </div>
      <div class="bis-presets">
        <button type="button" class="pill-btn preset-btn" data-bis-preset="quest,dungeon,raid,crafted">${esc(t('Core'))}</button>
        <button type="button" class="pill-btn preset-btn" data-bis-preset="quest,dungeon,crafted">${esc(t('Pre-Raid'))}</button>
        <button type="button" class="pill-btn preset-btn" data-bis-preset="${BIS_SOURCE_DEFS.map((x) => x.key).join(',')}">${esc(t('All'))}</button>
      </div>
    </div>`;

  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => Math.abs(c.gain))));

  const sourceCell = (c) => {
    const lines = (c.sources || []).slice(0, 3).map((s) => {
      const done = s.questDone
        ? ` <span class="badge warn"${hintAttr(t('You have already completed this quest on this character.'))}>${esc(t('quest done'))}</span>` : '';
      const craft = s.canCraft
        ? ` <span class="badge ok">${esc(t('you can craft it'))}</span>` : '';
      return `<div class="bis-src-line"><span class="badge src-badge src-${esc(s.kind)}">${esc(s.kindLabel)}</span> <span class="small">${esc(s.label)}</span>${done}${craft}</div>`;
    }).join('');
    const more = (c.sources || []).length > 3
      ? `<div class="small muted">${esc(t('+{n} more sources', { n: c.sources.length - 3 }))}</div>` : '';
    return lines + more;
  };

  const bisItemCell = (c, compare) => `<div class="item-cell">${itemIcon(c.id, c.quality)}<div class="item-body">
      <div class="item-head"><span class="${quality(c.quality)}"${tipAttr(c.tip, compare)}>${esc(c.name)}</span>
        ${c.equipped ? `<span class="badge ok">${esc(t('equipped'))}</span>` : (c.owned ? `<span class="badge ok">${esc(c.where || t('owned'))}</span>` : '')}
        ${c.isNew ? `<span class="badge forever">${esc(t('NEW in Forever'))}</span>` : ''}
        ${c.soon ? `<span class="badge warn">${esc(t('from level {n}', { n: c.reqLevel }))}</span>` : ''}
        ${c.unique ? `<span class="badge">${esc(t('Unique'))}</span>` : ''}
        ${c.bind === 'bop' ? `<span class="badge">${esc(t('BoP'))}</span>` : ''}
        ${c.price ? `<span class="badge"${hintAttr(t('Lowest buyout in the last AH scan.'))}>${t('AH')} ${money(c.price)}</span>` : ''}
        ${setBadges(c)}${enchBadge(c)}</div>
      <div class="small muted">${esc(c.stats)}${c.ilvl ? ` · iLvl ${esc(c.ilvl)}` : ''}</div>
      ${(c.effects || []).map((text) => `<div class="effects">${esc(text)}</div>`).join('')}</div></div>`;

  const gainCell = (c) => {
    const neg = c.gain < -0.05;
    const zero = Math.abs(c.gain) <= 0.05;
    const width = Math.min(100, Math.round((Math.max(0, c.gain) / maxGain) * 100));
    const cls = neg ? 'neg' : (zero ? 'zero' : '');
    const txt = zero ? num(0, 1) : signed(c.gain);
    return `<div class="gain"><b class="${cls}">${txt}</b><div class="bar"><span style="width:${width}%"></span></div></div>`;
  };

  const activeSlot = d.slots.some((s) => s.slotName === o.bisSlot) ? o.bisSlot : '';
  const visibleSlots = activeSlot ? d.slots.filter((s) => s.slotName === activeSlot) : d.slots;
  const slotPills = d.slots.length > 1 ? `<div class="slot-pills">
      <button type="button" class="pill-btn${activeSlot === '' ? ' active' : ''}" data-bis-slot="">${esc(t('All slots'))} <span class="count">${d.slots.length}</span></button>
      ${d.slots.map((s) => `<button type="button" class="pill-btn${activeSlot === s.slotName ? ' active' : ''}" data-bis-slot="${esc(s.slotName)}">${esc(s.slotName)} <span class="count">${s.candidates.length}</span></button>`).join('')}
    </div>` : '';

  const renderEquippedBox = (cur, label) => {
    if (!cur) return '';
    return `<div class="slot-current">
        <div class="small muted">${esc(label)}</div>
        <div class="small" style="margin-top:3px"><span class="item">${itemIcon(cur.id, cur.quality)}<span class="${quality(cur.quality)}"${tipAttr(cur.tip)}>${esc(cur.name || '–')}</span></span></div>
        <div class="small muted" style="margin-top:2px">${esc(cur.stats || '')}</div>
        ${cur.enchant ? `<div class="small good" style="margin-top:2px">${esc(cur.enchant.name)}</div>` : ''}
      </div>`;
  };

  const slotsHtml = visibleSlots.map((s) => {
    const cur = s.current;
    const cur2 = s.currentSecond;
    const rows = s.candidates.map((c) => `<tr${c.equipped ? ' class="bis-row-eq"' : ''}>
        <td class="num rank-num${c.rank === 1 ? ' rank-1' : ''}">#${c.rank}</td>
        <td>${gainCell(c)}</td>
        <td class="num muted">${pctOf(c)}</td>
        <td>${bisItemCell(c, cur?.tip)}</td>
        <td>${sourceCell(c)}</td></tr>`).join('');
    const specialsHtml = s.specials?.length ? `<details class="scale bis-specials"><summary>${esc(t('Special effects / procs not in Top {n} ({count})', { n: d.top, count: s.specials.length }))}</summary>
        <table><thead><tr>${th([t('Gain')], [t('Item')], [t('Source')])}</tr></thead><tbody>
        ${s.specials.map((sp) => `<tr>
          <td class="num muted">${signed(sp.gain)}</td>
          <td>${bisItemCell(sp, cur?.tip)}</td>
          <td>${sourceCell(sp)}</td></tr>`).join('')}</tbody></table></details>` : '';
    return `<div class="card section slot-card">
      <div><div class="slot-name">${esc(s.slotName)}</div>
        ${cur ? renderEquippedBox(cur, cur2 ? t('Equipped (1)') : t('Equipped')) : `<div class="slot-current"><div class="small muted">${esc(t('Equipped'))}</div><div class="small muted" style="margin-top:3px">–</div></div>`}
        ${cur2 ? renderEquippedBox(cur2, t('Equipped (2)')) : ''}
        <div class="small muted" style="margin-top:6px">${esc(t('{shown} of {total} items', { shown: s.candidates.length, total: s.totalCandidates }))}</div>
      </div>
      <div>
        <table><thead><tr>${th([t('#'), 'num'], [t('Gain')], [t('vs. item'), 'num'], [t('Item')], [t('Source')])}</tr></thead><tbody>${rows}</tbody></table>
        ${specialsHtml}
      </div></div>`;
  }).join('');

  const levelSummary = d.levelMode === '60'
    ? t('Level 60')
    : (d.targetLevel > d.level ? t('Level {lvl} (up to {max})', { lvl: d.level, max: d.targetLevel }) : t('Level {lvl}', { lvl: d.level }));
  const dataSrcText = (d.dataSources || []).join(' · ');

  const pk = d.package;
  const bisPackageHtml = pk?.items?.length ? `<div class="section"><h2 class="heading">${esc(t('Optimal BiS set for your gear'))}
      ${help(esc(t('Best combination across all slots within the active source filters and level range (rings and trinkets as pairs, main hand + off hand against two-hand, set bonuses and hit cap scored together). Already completed quests are excluded unless you own the item. Sorted by biggest gain, gear you already own comes first.')))}</h2><div class="card">
      <div class="row" style="gap:36px;align-items:flex-start;margin-bottom:10px">
        <div><h2>${esc(t('Gain'))}</h2><div class="big good">${signed(pk.gain)}${pk.gainPct === null || pk.gainPct === undefined ? ''
          : ` <span class="small muted">(${esc(eng ? t('+{pct} {unit}', { pct: percent(pk.gainPct), unit }) : t('+{pct} gear value', { pct: percent(pk.gainPct) }))})</span>`}</div></div>
        <div><h2>${esc(eng ? (eng.metric === 'ttoom' ? t('Mana with BiS (s)') : t('{unit} with BiS', { unit })) : t('BiS gear value'))}</h2>
          <div class="big">${num(pk.targetValue, 1)}</div>
          <div class="small muted">${esc(t('from {now}', { now: num(d.setValue, 1) }))}</div></div>
        <div><h2>${esc(t('Upgrades'))}</h2><div class="big">${pk.items.length}</div></div>
        ${pk.ownedCount ? `<div><h2>${esc(t('Already yours'))}</h2><div class="big">${pk.ownedCount}</div></div>` : ''}<span class="spacer"></span></div>
      <table><thead><tr>${th([t('Slot')], [t('Gain'), 'num'], [t('Item')], [t('Source')], [t('replaces')],
        [t('up to here'), 'num', esc(t('Total gain and resulting value if you equip from the top down to this row.'))])}</tr></thead><tbody>
      ${pk.items.map((c) => `<tr><td class="muted">${esc(c.slotName)}</td>
        <td class="num"><b class="${c.gain < 0 ? 'warn-text' : 'good'}">${signed(c.gain)}</b>${c.gain < 0 ? `<div class="small muted"${hintAttr(t('Only pays together with its paired main hand or ring further down in the list (replaces a two-hander on its own).'))}>${esc(t('paired swap'))}</div>` : ''}</td>
        <td>${bisItemCell(c, c.compareTip)}</td>
        <td>${sourceCell(c)}</td>
        <td class="small muted">${esc((c.replaces || []).join(' + ') || '–')}</td>
        <td class="num small"><span class="good">${signed(c.cumGain)}</span><div class="muted">${num(c.cumValue, 1)}${unit ? ` ${esc(unit)}` : ''}</div></td></tr>`).join('')}</tbody></table>
      </div></div>` : (d.slots.length ? `<div class="notice section">${esc(t('Your equipped gear is already optimal for the selected source filters and level range.'))}</div>` : '');

  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(eng ? (eng.metric === 'ttoom' ? t('Mana lasts (s)') : t('{unit} now', { unit })) : t('Gear value'))} ${help(esc(eng
    ? t('The metric of the simulated fight with the gear you wear now. Every gain below is the change of this number.')
    : t('Everything you wear, rated as one set in points of the scale. Hit counts only up to the cap, so the value of an item depends on the rest of the gear.')))}</h2>
        <div class="big">${num(d.setValue)}</div>${eng?.mc
    ? `<div class="small muted">${esc(t('spread ±{sd}, worst tenth {p10}', { sd: num(eng.mc.stdDev, 1), p10: num(eng.mc.p10, 1) }))}</div>` : ''}</div>
      <div><h2>${esc(t('Level range'))}</h2><div class="big">${esc(levelSummary)}</div>
        <div class="small muted">${esc(t('Top {n} per slot', { n: d.top }))}</div></div>
      <div><h2>${esc(t('Catalog'))} ${help(esc(t('Combines the built-in Forever source catalog with DB2 tables and locally installed ForeverMyBis / AtlasLoot addons when present.')))}</h2>
        <div class="big">${num(d.catalogItems)}</div>
        <div class="small muted">${esc(dataSrcText)}</div></div>
      <span class="spacer"></span></div>
      ${renderModelDetails(d, eng)}</div>
    <div class="section">${sourcePills}</div>
    ${bisPackageHtml}
    <h2 class="section heading">${esc(t('BiS list per slot'))} ${help(bisTerms(eng, unit))}</h2>
    ${slotPills}
    ${slotsHtml || `<div class="notice section">${esc(t('No items match the selected source filters and level range.'))}</div>`}`;
}

content.addEventListener('click', (e) => {
  if (state.page !== 'upgrades') return;
  const uSlot = e.target.closest('[data-u-slot]');
  if (uSlot) {
    state.upgrades.slot = uSlot.dataset.uSlot;
    saveView();
    renderUpgrades();
    return;
  }
  const bSlot = e.target.closest('[data-bis-slot]');
  if (bSlot) {
    state.upgrades.bisSlot = bSlot.dataset.bisSlot;
    saveView();
    renderUpgrades();
    return;
  }
  const srcBtn = e.target.closest('[data-bis-src]');
  if (srcBtn) {
    const key = srcBtn.dataset.bisSrc;
    const cur = (state.upgrades.bisSources || 'quest,dungeon,raid,crafted').split(',').filter(Boolean);
    const set = new Set(cur);
    if (set.has(key)) {
      if (set.size <= 1) return; // keep at least one category active
      set.delete(key);
    } else {
      set.add(key);
    }
    state.upgrades.bisSources = BIS_SOURCE_DEFS.map((d) => d.key).filter((k) => set.has(k)).join(',');
    saveView();
    renderUpgrades();
    return;
  }
  const presetBtn = e.target.closest('[data-bis-preset]');
  if (presetBtn) {
    state.upgrades.bisSources = presetBtn.dataset.bisPreset;
    saveView();
    renderUpgrades();
  }
});
