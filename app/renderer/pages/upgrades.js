// Page "AH upgrades": gear from the auction house against the equipped gear, rated by scripts/upgrades.py.
import { $, ago, api, cached, content, errorBox, esc, help, hintAttr, itemIcon, loadStatus, loading, money, num, percent, quality, selectOption, state, terms, th, tipAttr } from '../core.js';
import { t } from '../i18n.js';

// What the columns of the cards per slot mean; the cards repeat, so the explanation sits at the heading once.
// With the combat engine the gains are in the metric's unit (DPS, HPS, seconds), with the weight scale points.
const slotTerms = (engine, unit) => terms([
  [t('Gain'), engine ? t('Change of your {unit} in the simulated fight when you swap the item in.', { unit })
    : t('Change of the gear value when you swap the item in, in points of the scale.')],
  [t('vs. item'), engine ? t('The gain relative to the {unit} the replaced item contributes.', { unit })
    : t('The gain relative to the points of the item it replaces.')],
  [t('Price'), t('Lowest buyout at the last scan, not a live offer. Gear you already have costs nothing: the row says where it is (your bags, your bank, or unbound on another character).')],
  [engine ? t('{unit}/gold', { unit }) : t('Points/gold'), t('Gain per gold spent.')],
], t('Auctionator cannot tell items with random stats (“of Shadow Wrath”) apart, so they are missing here.'));

export async function renderUpgrades() {
  if (!state.status) await loadStatus();
  const o = state.upgrades;
  const fightControls = o.engine === 'off' ? '' : `
      <label><span class="label">${esc(t('Fight length (s)'))} ${help(esc(t('The combat model computes DPS over a fight of this length. A longer fight makes mana, regeneration and Life Tap matter more.')))}</span>
        <input type="number" id="u-fight" min="10" max="600" step="10" value="${o.fight}"></label>
      <label>${esc(t('Enemy level'))}<select id="u-target">${selectOption('', t('from the scale (+2/+3)'), o.target)}${[0, 1, 2, 3].map((n) => selectOption(String(n), `+${n}`, o.target)).join('')}</select></label>
      <label id="u-metric" style="display:none"></label>
      <label id="u-kills" style="display:none"></label>`;
  content.innerHTML = `<div class="controls">
      <label>${esc(t('Budget (gold)'))}<input type="number" id="u-max" class="wide" min="0" step="0.5" value="${esc(o.maxPrice)}" placeholder="${esc(t('your own gold'))}"></label>
      <label><span class="label">${esc(t('Rating'))} ${help(esc(t('The combat model simulates your rotation over the fight; when it cannot run, the weight scale answers instead. Monte-Carlo plays the rotation with real rolls: slower, adds the spread and replays the biggest gains. The weight scale rates stats with fixed weights.')))}</span>
        <select id="u-engine">${selectOption('auto', t('Combat model'), o.engine)}${selectOption('mc', t('Combat model (Monte-Carlo)'), o.engine)}${selectOption('off', t('Weight scale'), o.engine)}</select></label>
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
  const key = `upgrades:${state.char}:${o.maxPrice}:${o.preview}:${o.top}:${o.profile}:${o.sort}:${o.engine}:${o.fight}:${o.target}:${o.metric}:${o.kills}`;
  const d = await cached(key, () => api.bridge('upgrades', {
    char: state.char, maxPrice: o.maxPrice ? Number(o.maxPrice) : 0, preview: o.preview, top: o.top,
    profile: o.profile, sort: o.sort, engine: o.engine === 'mc' ? 'auto' : o.engine,
    method: o.engine === 'mc' ? 'montecarlo' : '', fightSeconds: Number(o.fight) || 60,
    targetDelta: o.target === '' ? -1 : Number(o.target), metric: o.metric,
    killsPerMinute: o.kills === '' ? -1 : Number(o.kills),
  }));
  const body = $('#u-body');
  if (!body) return;
  if (d.error) {
    body.innerHTML = `${errorBox(d.error)}<div class="notice">${esc(t('Scan the auction house with Auctionator and close it: the addon then reads new items by itself. Or request an item export on the “Connection” page.'))}</div>`;
    return;
  }
  const eng = d.engine; // set when the combat engine rated the items (gains are in the metric's unit)
  const unit = eng ? ({ dps: 'DPS', hps: 'HPS', ttoom: 's' })[eng.metric] || 'DPS' : '';
  const perGoldLabel = eng ? t('{unit}/gold', { unit }) : t('Points/gold');
  // A class with several metrics gets the select (its options come from the engine's answer).
  if (eng?.metrics?.length > 1) {
    const slot = $('#u-metric');
    if (slot) {
      const names = { dps: t('DPS (damage)'), hps: t('HPS (healing)'), ttoom: t('Mana lasts (s)') };
      slot.innerHTML = `${esc(t('Metric'))}<select id="u-metric-sel">${selectOption('', t('automatic'), o.metric)}${eng.metrics.map((m) => selectOption(m, names[m] || m, o.metric)).join('')}</select>`;
      slot.style.display = '';
      bind('#u-metric-sel', 'metric', (v) => v);
    }
  }
  // A talent that works after killing blows (Spirit Tap): how many of them the fight has.
  if (eng?.killBuff) {
    const slot = $('#u-kills');
    if (slot) {
      slot.innerHTML = `<span class="label">${esc(t('Kills per minute'))} ${help(esc(t('Your killing blows per minute, for talents that work after a kill (Spirit Tap). Automatic: one per fight for a damage rotation against normal enemies, none for a healer or against a boss.')))}</span>
        <select id="u-kills-sel">${selectOption('', t('automatic'), o.kills)}${[0, 1, 2, 3, 4, 6].map((n) => selectOption(String(n), String(n), o.kills)).join('')}</select>`;
      slot.style.display = '';
      bind('#u-kills-sel', 'kills', (v) => v);
    }
  }
  const maxGain = Math.max(1, ...d.slots.flatMap((s) => s.candidates.map((c) => c.gain)));
  const signed = (n) => `${n < 0 ? '−' : '+'}${num(Math.abs(n), 1)}`;
  const pctOf = (c) => (c.gainPct === null ? esc(t('new')) : `${c.gainPct < 0 ? '−' : '+'}${percent(Math.abs(c.gainPct))}`);
  // Gear the account already has: where it is instead of a price.
  const price = (c) => (c.price ? money(c.price) : `<span class="badge ok">${esc(c.where || t('owned'))}</span>`);
  const perGold = (c) => (c.price ? num(c.perGold, 1) : '–');
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
        <td class="num">${price(c)}</td>
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
        <td class="num"><b class="${c.gain < 0 ? '' : 'good'}">${signed(c.gain)}</b></td>
        <td class="num">${price(c)}</td><td class="num muted">${perGold(c)}</td>
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
      ${d.review.map((r) => `<tr><td class="muted">${esc(r.slot)}</td><td class="num">${price(r)}</td>
        <td><span class="item">${itemIcon(r.id, r.quality)}<span class="${quality(r.quality)}"${tipAttr(r.tip)}>${esc(r.name)}</span></span> ${r.isNew ? `<span class="badge new">${esc(t('NEW'))}</span>` : ''}</td>
        <td class="small">${esc(r.effects.join(' / '))}</td></tr>`).join('')}</tbody></table></div></div>` : '';
  const healRanks = eng?.healOptions?.length ? `<details class="scale"><summary>${esc(t('Heal ranks: what each rank sustains — the model heals with the top row'))}</summary>
      <table><thead><tr>${th([t('Spell')],
    [t('HPS in the fight'), 'num', esc(t('What the whole rotation sustains over the fight when this rank fills it — the number the rank choice uses. Mana, Renew and the fight length are all in here.'))],
    [t('Healing/mana'), 'num', esc(t('Healing one point of mana buys. Downranking trades cast speed for this efficiency.'))],
    [t('Healing/cast'), 'num'], [t('Mana'), 'num'], [t('Cast (s)'), 'num'])}</tr></thead><tbody>
      ${eng.healOptions.map((o) => `<tr><td>${esc(o.spell)}${o.chosen ? ` <span class="badge new">${esc(t('the model heals with this'))}</span>` : ''}${o.bonusLossPct ? ` <span class="badge warn"${hintAttr(t('You have outgrown this rank: the game gives it only part of your healing bonus. The numbers in this row already have that loss.'))}>${esc(t('{pct}% less bonus', { pct: num(o.bonusLossPct) }))}</span>` : ''}</td>
        <td class="num"><b>${num(o.hpsSustained, 1)}</b></td><td class="num">${num(o.healPerMana, 2)}</td>
        <td class="num">${num(o.healPerCast)}</td><td class="num">${num(o.manaPerCast)}</td><td class="num">${num(o.castSec, 1)}</td></tr>`).join('')}</tbody></table></details>` : '';
  // What the model did with every known spell and talent, so that nothing is left out silently.
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
  // The model's numbers per spell rank next to the game's own spell texts.
  const check = eng?.tooltipCheck;
  const tipCheck = check ? `<h3>${esc(t('Checked against the game'))} ${help(esc(t('The spell texts in your spellbook hold the numbers of your character: spell power, talents and the loss of an outgrown rank are in them. For every rank of the spells above, the model computes the same numbers; here is how many agree. With WoWMCP 1.12 or newer the mana cost and the cast time of every rank are compared as well. A rank that differs means the model misses something for it.')))}</h3>
      <p class="small ${check.off.length ? '' : 'muted'}">${esc(check.text)}</p>
      ${check.off.length ? `<table><tbody>${check.off.map((o) => `<tr><td><span class="badge warn">${esc(t('differs'))}</span></td>
        <td>${esc(o.label)}</td><td class="small muted">${esc(o.model)}</td><td class="small">${esc(o.text)}</td></tr>`).join('')}</tbody></table>` : ''}` : '';
  const covCount = (status) => (cov ? [...cov.spells, ...cov.talents].filter((e) => e.status === status).length : 0);
  const coverage = cov ? `<details class="scale coverage"><summary>${esc(t('What the model does with your spells and talents: {used} used, {open} not modeled, {n} rotations compared',
    { used: covCount('used'), open: covCount('unmodeled'), n: cov.candidates }))}</summary>
      ${cov.exactTalents ? '' : `<div class="notice" style="margin-top:8px">${esc(t('The talent tables of the game could not be loaded: talent values are estimates.'))}</div>`}
      <h3>${esc(t('Spells'))}</h3>${covRows(cov.spells)}<h3>${esc(t('Talents'))}</h3>${covRows(cov.talents)}${tipCheck}</details>` : '';
  // Owned gear an older addon exported by name only cannot be rated.
  const unknown = d.owned?.unknown || [];
  const unknownNote = unknown.length ? `<div class="notice" style="margin-top:10px"${hintAttr(unknown.map((u) => `${u.name} (${u.where})`).join(', '))}>${esc(t('{n} items you own are only known by name and cannot be rated: open the bank once with WoWMCP 1.12 or newer, then /reload.', { n: unknown.length }))}</div>` : '';
  body.innerHTML = `<div class="card"><div class="row" style="gap:36px;align-items:flex-start">
      <div><h2>${esc(t('Budget'))}</h2><div class="big">${money(d.budget)}</div></div>
      <div><h2>${esc(eng ? (eng.metric === 'ttoom' ? t('Mana lasts (s)') : t('{unit} now', { unit })) : t('Gear value'))} ${help(esc(eng
    ? t('The metric of the simulated fight with the gear you wear now. Every gain below is the change of this number.')
    : t('Everything you wear, rated as one set in points of the scale. Hit counts only up to the cap, so the value of an item depends on the rest of the gear.')))}</h2>
        <div class="big">${num(d.setValue)}</div>${eng?.mc
    ? `<div class="small muted">${esc(t('spread ±{sd}, worst tenth {p10}', { sd: num(eng.mc.stdDev, 1), p10: num(eng.mc.p10, 1) }))}</div>` : ''}</div>
      <div><h2>${esc(t('AH scan'))}</h2><div class="big">${esc(d.scanDate)}</div></div>
      <div><h2>${esc(t('Item export'))} ${help(esc(t('Items of the last AH scans whose stats the WoWMCP addon has read in the game. Only these can be compared.')))}</h2>
        <div class="big">${num(d.exportCount)}</div><div class="small muted">${esc(ago(d.exportUpdated))}</div></div>
      <span class="spacer"></span></div>
      ${scale}${eng?.downrank?.text ? `<div class="notice" style="margin-top:10px">${esc(eng.downrank.text)}</div>` : ''}${eng?.buffs ? `<p class="small muted" style="margin:8px 0 0">${esc(eng.buffs.text)}</p>` : ''}${unknownNote}${healRanks}${coverage}</div>
    ${packageHtml}
    ${slots ? `<h2 class="section heading">${esc(t('All slots'))} ${help(slotTerms(eng, unit))}</h2>${slots}` : `<div class="notice section">${esc(t('No upgrades within this budget.'))}</div>`}${review}`;
}
