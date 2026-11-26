// Page "Character": the character window as in the game (gear left and right, weapons below, stats in the
// middle), with tabs for Damage, Healing and Consumables so each view stays focused.
import { $, DASH, ago, api, cached, content, errorBox, esc, help, hintAttr, itemIcon, loadStatus, loading, money, num, percent, quality, saveView, setTabs, state, tipAttr } from '../core.js';
import { mark, t } from '../i18n.js';
import { consumablesSection, loadConsumables } from './consumables.js';
import { damageSection } from './damage.js';
import { enchantsSection, loadEnchants } from './enchants.js';
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
const DAMAGE_SCHOOLS = {
  holy: mark('Holy damage'), fire: mark('Fire damage'), nature: mark('Nature damage'),
  frost: mark('Frost damage'), shadow: mark('Shadow damage'), arcane: mark('Arcane damage'),
};

const RELIC_CLASSES = new Set(['DRUID', 'SHAMAN', 'PALADIN']);
const WAND_CLASSES = new Set(['WARLOCK', 'PRIEST', 'MAGE']);

function rangedSlotLabel(classFile) {
  if (RELIC_CLASSES.has(classFile)) return mark('Relic');
  if (WAND_CLASSES.has(classFile)) return mark('Wand');
  return mark('Ranged');
}

function resourceLabel(powerType) {
  if (powerType === 'RAGE') return t('Rage');
  if (powerType === 'ENERGY') return t('Energy');
  return t('Mana');
}

function formatSpeed(mh, oh) {
  if (!mh) return '–';
  return oh ? `${num(mh, 2)} / ${num(oh, 2)} s` : t('{n} s', { n: num(mh, 2) });
}

// Points per talent tree, e.g. "Affliction 37 / Demonology 1 / Destruction 0 · 1 unspent".
function talentLine(tree) {
  const groups = Array.isArray(tree.groups) ? tree.groups.map((g) => [g.name, g.spent]) : Object.entries(tree.tabs || {});
  const points = groups.length ? groups.map(([name, spent]) => `${esc(name)} ${esc(spent)}`).join(' / ')
    : `${esc(tree.label || '')} ${esc(tree.spent ?? '')}`.trim();
  return points + (tree.unspent ? ` <span class="muted">· ${esc(t('{n} unspent', { n: tree.unspent }))}</span>` : '');
}

// Picks the school with the highest spell power so non-warlocks do not show Shadow damage.
function topSpellSchool(st) {
  const dmg = st.spellDamage || {};
  let bestKey = 'shadow', bestVal = -1;
  for (const [k, v] of Object.entries(dmg)) {
    if (DAMAGE_SCHOOLS[k] && typeof v === 'number' && v > bestVal) { bestKey = k; bestVal = v; }
  }
  const vals = Object.values(dmg).filter((v) => typeof v === 'number');
  const uniform = vals.length > 1 && vals.every((v) => v === vals[0]);
  return {
    key: bestKey,
    label: uniform ? t('Spell damage') : t(DAMAGE_SCHOOLS[bestKey] || mark('Spell damage')),
    value: bestVal >= 0 ? bestVal : dmg.shadow,
    crit: st.spellCrit?.[bestKey] ?? st.spellCrit?.shadow,
  };
}

function activateCharTab(key) {
  state.character.tab = key;
  for (const panel of content.querySelectorAll('[data-char-panel]')) panel.hidden = panel.dataset.charPanel !== key;
  content.scrollTop = 0;
  saveView();
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
        <div class="pd-meta"><span class="pd-label">${esc(t(label))}</span> · ${meta}</div>
        ${e.enchant ? `<div class="pd-ench">${esc(e.enchant.name)}</div>` : ''}</div></div>`;
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
  const school = topSpellSchool(st);
  const talents = [...(d.talents || []), ...(d.legacy || [])].map(talentLine).filter(Boolean).join('<br>');
  const buffs = (c.buffs || []).filter((b) => b?.name);
  const weBuffs = (d.weaponEnchants || []).map((w) => ({ name: w.name }));
  const allBuffs = [...weBuffs, ...buffs];
  const buffsBox = allBuffs.length ? `<div class="pd-stats"><h4>${esc(t('Active buffs'))}</h4>
    <div class="pd-buffs">${allBuffs.map((b) => `<span class="pd-buff">${esc(b.name)}</span>`).join('')}</div></div>` : '';
  const sets = d.sets || [];
  const setsBox = sets.length ? `<div class="pd-stats"><h4>${esc(t('Item sets'))}</h4>
    <div class="pd-buffs">${sets.map((s) => {
      const active = (s.bonuses || []).filter((b) => b.active).map((b) => `${b.threshold}`);
      const detail = active.length ? ` · ${active.join(', ')}` : (s.skillMet === false ? ` · ${t('skill requirement not met')}` : '');
      const tip = (s.bonuses || []).map((b) => `(${b.threshold}) ${b.name || ''}${b.active ? ' ✓' : ''}`).join('\n');
      return `<span class="pd-buff"${tip ? hintAttr(tip) : ''}>${esc(s.name)} (${s.equipped}/${s.total}${esc(detail)})</span>`;
    }).join('')}</div></div>` : '';
  const role = d.role || 'caster';
  const combatBox = role === 'melee' || role === 'tank'
    ? statBox(t('Melee'), [
      [t('Attack power'), value(st.attackPower)],
      [t('Melee crit'), pct(st.meleeCrit)],
      [t('Hit'), st.meleeHit !== undefined ? percent(value(st.meleeHit)) : '–'],
      [t('Weapon speed'), formatSpeed(st.meleeSpeed, st.offhandSpeed)],
    ])
    : role === 'ranged'
      ? statBox(t('Ranged'), [
        [t('Ranged attack power'), value(st.rangedAttackPower)],
        [t('Ranged crit'), pct(st.rangedCrit)],
        [t('Hit'), st.meleeHit !== undefined ? percent(value(st.meleeHit)) : '–'],
        [t('Weapon speed'), formatSpeed(st.rangedSpeed)],
      ])
      : statBox(t('Spells'), [
        [school.label, value(school.value)],
        [t('Healing'), value(st.healing)],
        [t('Spell crit'), pct(school.crit)],
        [t('Spell hit'), st.spellHit !== undefined ? percent(value(st.spellHit)) : '–'],
      ]);
  const defenseRows = [
    [t('Health'), value(st.healthMax)],
    [resourceLabel(st.powerType), value(st.powerMax)],
    [t('Armor'), value(st.armor)],
    [t('Dodge'), pct(st.dodge)],
  ];
  if (role === 'tank' || st.defense || st.parry > 0 || st.block > 0) {
    if (st.defense) defenseRows.push([t('Defense skill'), value(st.defense)]);
    if (st.parry > 0) defenseRows.push([t('Parry'), pct(st.parry)]);
    if (st.block > 0) defenseRows.push([t('Block'), pct(st.block)]);
  }
  const bottomSlots = [[16, mark('Main Hand')], [17, mark('Off Hand')], [18, rangedSlotLabel(c.classFile)]];
  const center = `<div class="pd-center">
      <div class="pd-res">${resist}</div>
      <div class="pd-grid">
        ${statBox(t('Attributes'), [[t('Strength'), value(st.strength)], [t('Agility'), value(st.agility)], [t('Stamina'), value(st.stamina)],
          [t('Intellect'), value(st.intellect)], [t('Spirit'), value(st.spirit)]])}
        ${combatBox}
        ${statBox(t('Defense'), defenseRows)}
        ${statBox(t('Gear'), [[t('Gear value'), value(d.setValue, 1)], [t('Avg. iLvl'), ilvls.length ? value(ilvls.reduce((a, b) => a + b, 0) / ilvls.length, 1) : '–'],
          [t('Lowest durability'), lowest === null ? '–' : `<span class="${lowest < 0.3 ? 'dur-low' : ''}">${percent(Math.round(lowest * 100))}</span>`],
          [t('Scale'), esc(d.profileLabel)]],
        esc(t('pts = points: what the gear would lack without the item, on the scale “{scale}” of the Upgrades page ({weights}). {total} in total.',
          { scale: d.profileLabel, weights: d.weights, total: num(d.setValue) })))}
      </div>
      ${(buffsBox || setsBox) ? `<div class="pd-extra">${setsBox}${buffsBox}</div>` : ''}</div>`;

  const profs = d.professions.map((p) => `<div style="margin-bottom:8px">
      <div class="row small"><span>${esc(p.name)}</span><span class="spacer"></span><span class="muted">${p.rank}/${p.max}</span></div>
      <div class="bar"><span style="width:${Math.round((p.rank / p.max) * 100)}%"></span></div></div>`).join('');
  const xp = t('Experience {xp} / {max} ({pct})', { xp: num(c.xp || 0), max: num(c.xpMax || 0), pct: percent(xpPct) })
    + (c.rested ? ` · ${t('rested {n}', { n: num(c.rested) })}` : '');

  // Real panel tabs docked above the scroll area.
  const tabDefs = [{ key: 'gear', label: t('Gear') },
    ...(d.metric === 'dps' ? [[d.deals, 'dmg', t('Damage')], [d.heals, 'heal', t('Healing')]]
      : [[d.heals, 'heal', t('Healing')], [d.deals, 'dmg', t('Damage')]]).filter(([on]) => on).map(([, key, label]) => ({ key, label })),
    { key: 'ench', label: t('Enchants') },
    { key: 'cons', label: t('Consumables') }];
  const activeTab = tabDefs.some((x) => x.key === state.character.tab) ? state.character.tab : 'gear';
  state.character.tab = activeTab;
  if (state.page === 'character') setTabs(tabDefs, activeTab, activateCharTab);
  const heal = d.heals ? `<div class="tab-panel" data-char-panel="heal" id="c-heal"${activeTab === 'heal' ? '' : ' hidden'}>${loading(t('Computing every heal rank …'))}</div>` : '';
  const dmg = d.deals ? `<div class="tab-panel" data-char-panel="dmg" id="c-dmg"${activeTab === 'dmg' ? '' : ' hidden'}>${loading(t('Playing the fights and computing every damage spell …'))}</div>` : '';
  content.innerHTML = `<div class="tab-panel" data-char-panel="gear" id="c-gear"${activeTab === 'gear' ? '' : ' hidden'}>
      <div class="paperdoll">
        <div class="pd-head">
          <div><div class="pd-title">${esc(c.name)} <span>${esc(c.surname || '')}</span></div>
            <div class="pd-sub">${esc(t('Level {level}', { level: c.level }))} ${esc(c.race)} ${d.specName ? `${esc(d.specName)} ` : ''}${esc(c.class)}</div></div>
          <span class="spacer"></span>
          <div class="pd-money">${money(c.money)}<div class="small muted">${esc(c.zone || '')} · Snapshot ${esc(ago(d.updated))}</div></div>
        </div>
        <div class="pd-body">
          <div class="pd-col">${column(PAPERDOLL.left, 'left')}</div>
          ${center}
          <div class="pd-col">${column(PAPERDOLL.right, 'right')}</div>
          <div class="pd-weapons">${column(bottomSlots, 'bottom')}</div>
        </div>
        <div class="pd-xp"><div class="bar"><span style="width:${xpPct}%"></span></div>
          <div class="pd-xp-text">${esc(xp)}</div></div>
      </div>
      <div class="grid section" id="c-prof"><div class="card"><h2>${esc(t('Professions'))}</h2>${profs || DASH}</div>
        <div class="card"><h2>${esc(t('Talents'))}</h2>${talents || DASH}</div></div>
    </div>
    ${(d.metric === 'dps' ? [dmg, heal] : [heal, dmg]).join('')}
    <div class="tab-panel" data-char-panel="ench" id="c-ench"${activeTab === 'ench' ? '' : ' hidden'}>${loading(t('Rating permanent enchants per slot …'))}</div>
    <div class="tab-panel" data-char-panel="cons" id="c-cons"${activeTab === 'cons' ? '' : ' hidden'}>${loading(t('Rating the consumables for a fight …'))}</div>`;
  // The heal table, the damage analysis, the enchants and the consumables take a few engine runs each: the page stands,
  // the sections follow.
  const char = state.char;
  const fill = async (selector, load, html) => {
    const r = await load();
    const box = $(selector);
    if (box && state.char === char && state.page === 'character') box.innerHTML = r.error ? errorBox(r.error) : html(r);
  };
  await Promise.all([
    d.heals ? fill('#c-heal', () => cached(`healing:${char}`, () => api.bridge('healing', { char })), healingSection) : null,
    d.deals ? fill('#c-dmg', () => cached(`damage:${char}`, () => api.bridge('damage', { char })), damageSection) : null,
    fill('#c-ench', () => loadEnchants(char), enchantsSection),
    fill('#c-cons', () => loadConsumables(char), consumablesSection),
  ]);
}
