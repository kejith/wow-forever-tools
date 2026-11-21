// Page "Character": the character window as in the game (gear left and right, weapons below, stats in the
// middle), then professions and talents, and for a class that heals the heal table (pages/healing.js).
import { $, DASH, ago, api, cached, content, errorBox, esc, help, hintAttr, itemIcon, loadStatus, loading, money, num, percent, quality, state, tipAttr } from '../core.js';
import { mark, t } from '../i18n.js';
import { consumablesSection, loadConsumables } from './consumables.js';
import { healingSection } from './healing.js';

// Slot IDs and order of the in-game character window (Classic layout).
const PAPERDOLL = {
  left: [[1, mark('Head')], [2, mark('Neck')], [3, mark('Shoulders')], [15, mark('Back')], [5, mark('Chest')], [4, mark('Shirt')],
    [19, mark('Tabard')], [9, mark('Wrists')]],
  right: [[10, mark('Hands')], [6, mark('Waist')], [7, mark('Legs')], [8, mark('Feet')], [11, mark('Finger')], [12, mark('Finger')],
    [13, mark('Trinket')], [14, mark('Trinket')]],
  bottom: [[16, mark('Main Hand')], [17, mark('Off Hand')], [18, mark('Ranged')]],
};
const SCHOOLS = [['arcane', mark('Arcane')], ['fire', mark('Fire')], ['nature', mark('Nature')], ['frost', mark('Frost')], ['shadow', mark('Shadow')]];

// Points per talent tree, e.g. "Affliction 37 / Demonology 1 / Destruction 0 · 1 unspent".
function talentLine(tree) {
  const groups = Array.isArray(tree.groups) ? tree.groups.map((g) => [g.name, g.spent]) : Object.entries(tree.tabs || {});
  const points = groups.length ? groups.map(([name, spent]) => `${esc(name)} ${esc(spent)}`).join(' / ')
    : `${esc(tree.label || '')} ${esc(tree.spent ?? '')}`.trim();
  return points + (tree.unspent ? ` <span class="muted">· ${esc(t('{n} unspent', { n: tree.unspent }))}</span>` : '');
}

export async function renderCharacter() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = `<div class="notice">${esc(t('No character exported.'))}</div>`; return; }
  content.innerHTML = loading(t('Loading character …'));
  const d = await cached(`character:${state.char}`, () => api.bridge('character', { char: state.char }));
  if (d.error) { content.innerHTML = errorBox(d.error); return; }
  const c = d.character;
  const st = c.stats || {};
  const xpPct = c.xpMax ? Math.round((c.xp / c.xpMax) * 100) : 0;
  const value = (v, digits = 0) => (v === undefined || v === null ? '–' : num(v, digits));
  const pct = (v) => (v === undefined || v === null ? '–' : percent(num(v, 2)));

  // Paper doll in the layout of the in-game character window: left and right columns, weapons below.
  const bySlot = Object.fromEntries(d.equipment.map((e) => [e.slot, e]));
  const slot = ([id, label], side) => {
    const e = bySlot[id];
    if (!e) {
      return `<div class="pd-slot ${side} empty">${itemIcon(null, null, 'lg')}
        <div class="pd-text"><div class="pd-name">${esc(t(label))}</div><div class="pd-meta">${esc(t('empty'))}</div></div></div>`;
    }
    const dur = e.durability;
    const meta = [
      esc(t('{n} pts', { n: value(e.score, 1) })),
      e.ilvl ? `iLvl ${esc(e.ilvl)}` : '',
      dur ? `<span class="${dur[0] / dur[1] < 0.3 ? 'dur-low' : ''}">${dur[0]}/${dur[1]}</span>` : '',
    ].filter(Boolean).join(' · ');
    return `<div class="pd-slot ${side}"${tipAttr(e.tip)}>${itemIcon(e.id, e.quality, 'lg')}
      <div class="pd-text"><div class="pd-name ${quality(e.quality)}">${esc(e.name)}</div>
        <div class="pd-meta"><span class="pd-label">${esc(t(label))}</span> · ${meta}</div></div></div>`;
  };
  const column = (slots, side) => slots.map((s) => slot(s, side)).join('');

  const statBox = (title, rows, info = '') => `<div class="pd-stats"><h4>${esc(title)} ${help(info)}</h4>
    ${rows.map(([k, v]) => `<div class="pd-stat"><span>${esc(k)}</span><b>${v}</b></div>`).join('')}</div>`;
  const res = st.resistances || {};
  const resist = SCHOOLS.map(([k, label]) => `<span class="res res-${k}"${hintAttr(t('{school} resistance', { school: t(label) }))}>`
    + `${value(res[k] ?? 0)}<small>${esc(t(label))}</small></span>`).join('');
  const durs = d.equipment.filter((e) => e.durability).map((e) => e.durability[0] / e.durability[1]);
  const ilvls = d.equipment.map((e) => e.ilvl).filter(Boolean);
  const lowest = durs.length ? Math.min(...durs) : null;
  const center = `<div class="pd-center">
      <div class="pd-res">${resist}</div>
      <div class="pd-grid">
        ${statBox(t('Attributes'), [[t('Strength'), value(st.strength)], [t('Agility'), value(st.agility)], [t('Stamina'), value(st.stamina)],
          [t('Intellect'), value(st.intellect)], [t('Spirit'), value(st.spirit)]])}
        ${statBox(t('Spells'), [[t('Shadow damage'), value(st.spellDamage?.shadow)], [t('Healing'), value(st.healing)],
          [t('Spell crit'), pct(st.spellCrit?.shadow)], [t('Spell hit'), st.spellHit !== undefined ? percent(value(st.spellHit)) : '–']])}
        ${statBox(t('Defense'), [[t('Health'), value(st.healthMax)], [st.powerType === 'MANA' || !st.powerType ? t('Mana') : t('Energy'), value(st.powerMax)],
          [t('Armor'), value(st.armor)], [t('Dodge'), pct(st.dodge)]])}
        ${statBox(t('Gear'), [[t('Gear value'), value(d.setValue, 1)], [t('Avg. iLvl'), ilvls.length ? value(ilvls.reduce((a, b) => a + b, 0) / ilvls.length, 1) : '–'],
          [t('Lowest durability'), lowest === null ? '–' : `<span class="${lowest < 0.3 ? 'dur-low' : ''}">${percent(Math.round(lowest * 100))}</span>`],
          [t('Scale'), esc(d.profileLabel)]],
        esc(t('pts = points: what the gear would lack without the item, on the scale “{scale}” of the AH upgrades ({weights}). {total} in total.',
          { scale: d.profileLabel, weights: d.weights, total: num(d.setValue) })))}
      </div></div>`;

  const profs = d.professions.map((p) => `<div style="margin-bottom:8px">
      <div class="row small"><span>${esc(p.name)}</span><span class="spacer"></span><span class="muted">${p.rank}/${p.max}</span></div>
      <div class="bar"><span style="width:${Math.round((p.rank / p.max) * 100)}%"></span></div></div>`).join('');
  const talents = [...(d.talents || []), ...(d.legacy || [])].map(talentLine).filter(Boolean).join('<br>');
  const xp = t('Experience {xp} / {max} ({pct})', { xp: num(c.xp || 0), max: num(c.xpMax || 0), pct: percent(xpPct) })
    + (c.rested ? ` · ${t('rested {n}', { n: num(c.rested) })}` : '');
  content.innerHTML = `
    <div class="paperdoll">
      <div class="pd-head">
        <div><div class="pd-title">${esc(c.name)} <span>${esc(c.surname || '')}</span></div>
          <div class="pd-sub">${esc(t('Level {level}', { level: c.level }))} ${esc(c.race)} ${esc(c.class)}</div></div>
        <span class="spacer"></span>
        <div class="pd-money">${money(c.money)}<div class="small muted">${esc(c.zone || '')} · Snapshot ${esc(ago(d.updated))}</div></div>
      </div>
      <div class="pd-body">
        <div class="pd-col">${column(PAPERDOLL.left, 'left')}</div>
        ${center}
        <div class="pd-col">${column(PAPERDOLL.right, 'right')}</div>
        <div class="pd-weapons">${column(PAPERDOLL.bottom, 'bottom')}</div>
      </div>
      <div class="pd-xp"><div class="bar"><span style="width:${xpPct}%"></span></div>
        <div class="pd-xp-text">${esc(xp)}</div></div>
    </div>
    <div class="grid section"><div class="card"><h2>${esc(t('Professions'))}</h2>${profs || DASH}</div>
      <div class="card"><h2>${esc(t('Talents'))}</h2>${talents || DASH}</div></div>
    ${d.heals ? `<div class="section" id="c-heal">${loading(t('Computing every heal rank …'))}</div>` : ''}
    <div id="c-cons">${loading(t('Rating the consumables for a fight …'))}</div>`;
  // The heal table and the consumables take a few engine runs each: the page stands, the sections follow.
  const char = state.char;
  const fill = async (selector, load, html) => {
    const r = await load();
    const box = $(selector);
    if (box && state.char === char && state.page === 'character') box.innerHTML = r.error ? errorBox(r.error) : html(r);
  };
  await Promise.all([
    d.heals ? fill('#c-heal', () => cached(`healing:${char}`, () => api.bridge('healing', { char })), healingSection) : null,
    fill('#c-cons', () => loadConsumables(char), consumablesSection),
  ]);
}
