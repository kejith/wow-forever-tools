// The section "Damage" of the Character page: the rotation that deals the most damage and the one for a long
// fight, every damage spell the character knows with what one cast does and costs, the rotations of the other
// fights and what a point of each stat adds. From the combat engine through scripts/damage.py; only asked for
// when the combat engine has a damage metric for the class.
import { DASH as dash, esc, help, hintAttr, num, percent, terms, th } from '../core.js';
import { t } from '../i18n.js';
import { clock, stepsTable } from './rotations.js';

// The two rotations the section opens with; the other fights follow as a table.
const TOP = ['max', 'endurance'];

// What one cast does without a crit, as a tooltip says it: the hit, and below it the ticks.
function hit(r) {
  const parts = [];
  if (r.max) parts.push(r.min === r.max ? num(r.min) : `${num(r.min)}–${num(r.max)}`);
  if (r.ticks) parts.push(t('{ticks} × {tick} over {seconds} s', { ticks: num(r.ticks), tick: num(r.perTick, 0), seconds: num(r.durationSec) }));
  return parts.map((part, i) => `<div>${i ? '+ ' : ''}${esc(part)}</div>`).join('');
}

const best = (text, is, why) => (is ? `<b class="heal-best"${hintAttr(why)}>${text}</b>` : text);

function spellRow(r, first) {
  const limit = r.limit === 'kept' ? t('kept up') : r.limit === 'cooldown' ? t('per cooldown') : '';
  const dps = `${best(num(r.dps, 1), r.best.includes('dps'), t('The most damage per second among the spells you can cast back to back on one enemy.'))}
      ${limit ? `<div class="small muted">${esc(limit)}</div><div class="small muted nowrap">${esc(t('{n} while casting', { n: num(r.perCastSec, 1) }))}</div>` : ''}`;
  const alone = !r.spammable ? dash
    : r.spamSeconds === null ? `<span class="good">${esc(t('lasts'))}</span>`
      : `<span${hintAttr(t('{n} damage until the mana is gone', { n: num(r.spamDamage) }))}>${esc(clock(r.spamSeconds))}<div class="small muted">${num(r.spamDamage)}</div></span>`;
  // The two rotations of the section by name, the other fights as a count with their names on hover.
  const other = r.uses.filter((u) => !TOP.includes(u.key));
  const uses = [...r.uses.filter((u) => TOP.includes(u.key)).map((u) => `<span class="badge ok">${esc(u.title)}</span>`),
    other.length ? `<span class="badge"${hintAttr(other.map((u) => u.title).join(', '))}>${esc(other.length === 1 ? other[0].title : t('+{n} fights', { n: other.length }))}</span>` : ''].join(' ');
  const about = [t('from level {level}', { level: r.level }), r.schoolLabel,
    r.cooldownSec ? t('cooldown {time}', { time: clock(r.cooldownSec) }) : '', r.area ? t('hits every enemy') : '', r.leech ? t('returns {n} health', { n: num(r.leech) }) : '',
    r.selfDamage ? t('costs you {n} health', { n: num(r.selfDamage) }) : ''].filter(Boolean).join(' · ');
  return `<tr${first ? ' class="heal-chain"' : ''}>
      <td>${esc(r.label)}
        <div class="small muted">${esc(about)}</div>
        ${uses ? `<div class="heal-uses">${uses}</div>` : ''}</td>
      <td class="nowrap">${hit(r)}${r.critPct ? `<div class="small muted">${esc(t('{pct} crit', { pct: percent(num(r.critPct, 1)) }))}</div>` : ''}</td>
      <td class="num"><b>${num(r.perCast, 0)}</b></td>
      <td class="num">${num(r.mana)}</td>
      <td class="num">${r.castSec ? esc(t('{n} s', { n: num(r.castSec, 1) })) : esc(t('instant'))}</td>
      <td class="num">${dps}</td>
      <td class="num">${best(num(r.dpm, 2), r.best.includes('dpm'), t('The most damage per mana of all your spells.'))}</td>
      <td class="num">${esc(percent(num(r.coefPct, 0)))}<div class="small muted">+${num(r.bonus, 0)}</div></td>
      <td class="num">${alone}</td></tr>`;
}

function rows(list) {
  let chain = null;
  return list.map((r) => {
    const first = chain !== null && r.baseId !== chain;
    chain = r.baseId;
    return spellRow(r, first);
  }).join('');
}

function spellTable(d) {
  const head = th(
    [t('Spell'), '', `${esc(t('Every damage spell you know in its highest rank, with your talents, your spell power and the enemy’s level in the numbers. A lower rank is listed here only if it is the best per mana; the others are below the table.'))}
      <p>${esc(t('The labels below a spell are the fights whose rotation casts it. A row is one cast alone: what your other spells add to it shows in the rotations.'))}</p>`],
    [t('One cast'), '', esc(t('What one cast does without a crit, as the tooltip shows it: the range of a direct hit, the ticks of a spell over time or of a channel.'))],
    [t('Average'), 'num', esc(t('Damage of one cast on average: misses and crits weighed in, all ticks counted.'))],
    [t('Mana'), 'num', esc(t('Mana cost with your talents.'))],
    [t('Cast time'), 'num', esc(t('An instant spell still takes the global cooldown of 1.5 seconds.'))],
    ['DPS', 'num', `${esc(t('Damage per second on one enemy when you use the spell as much as you can: cast back to back, kept on the enemy for a spell over time, or cast whenever its cooldown is ready.'))}
      <p>${esc(t('“while casting”: the average divided by the time the cast takes. That is what a second of casting buys, and why the spells over time come first in a rotation.'))}</p>`],
    [t('Per mana'), 'num', esc(t('Damage per point of mana: the number that decides how long your mana lasts.'))],
    [t('Coefficient'), 'num', esc(t('What a point of spell power adds to one cast, in percent: with talents, misses and crits in it. Below: what your spell power adds to the average now.'))],
    [t('Alone'), 'num', esc(t('Casting only this spell without a pause: how long a full mana bar lasts with the regeneration while casting, and below it the damage until then. “lasts”: the regeneration pays for it.'))],
  );
  const shown = d.rows.filter((r) => r.top || r.best.length);
  const lower = d.rows.filter((r) => !r.top && !r.best.length);
  const wand = d.stats.wandDps ? `<tr class="heal-chain"><td>${esc(t('Wand'))}<div class="small muted">${esc(t('what you do when the mana is gone'))}</div></td>
      <td></td><td class="num">${dash}</td><td class="num">0</td><td class="num">${dash}</td><td class="num">${num(d.stats.wandDps, 1)}</td>
      <td class="num">${dash}</td><td class="num">${dash}</td>
      <td class="num"><span class="good">${esc(t('lasts'))}</span></td></tr>` : '';
  const table = (body) => `<table class="sticky compact heal-table"><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
  return `${table(rows(shown) + wand)}
    ${lower.length ? `<details class="scale dmg-lower"><summary>${esc(t('Lower ranks ({n})', { n: lower.length }))}</summary>${table(rows(lower))}</details>` : ''}`;
}

function statTiles(d) {
  const s = d.stats;
  const tile = (label, value, sub = '') => `<div class="heal-tile"><div class="small muted">${esc(label)}</div>
      <div class="big">${value}</div>${sub ? `<div class="small muted">${esc(sub)}</div>` : ''}</div>`;
  return `<div class="heal-tiles">
      ${s.schools.map((x) => tile(t('Spell power: {school}', { school: x.label }), num(x.power), t('{pct} crit on the character sheet', { pct: percent(num(x.critPct, 1)) }))).join('')}
      ${tile(t('Spells that miss'), esc(percent(num(s.missPct, 1))), t('enemy {delta} levels above you', { delta: s.targetDelta }))}
      ${tile(t('Mana'), num(s.manaMax), t('{casting} per 5 sec. while casting, {idle} after 5 s without a cast', { casting: num(s.castingPer5, 1), idle: num(s.idlePer5, 1) }))}
      ${s.tapMana ? tile(t('Mana for health'), num(s.tapMana), t('one cast, for {health} of your {max} health', { health: num(s.tapHealth), max: num(s.healthMax) })) : ''}
      ${s.wandDps ? tile(t('Wand'), `${num(s.wandDps, 1)} <span class="small muted">DPS</span>`, t('costs no mana')) : ''}
    </div>`;
}

// One of the two rotations the section opens with, as a card like those of the page "Rotations".
function topCard(s, unit) {
  const mana = !s.freeMana ? ''
    : s.oomSeconds === null ? `<span class="badge ok">${esc(t('Mana lasts'))}</span>`
      : `<span class="badge warn">${esc(t('Mana lasts {time}', { time: clock(s.oomSeconds) }))}</span>`;
  const skipped = s.skipped.map((k) => `<div class="small muted">${esc(t('Left out'))}: <span class="rot-name">${esc(k.label)}</span>
      <span class="warn-text">−${num(k.loss, 1)} ${esc(unit)}</span></div>`).join('');
  return `<div class="card rot-card" data-key="${esc(s.key)}">
      <div class="rot-head"><h3>${esc(s.title)} ${mana}</h3><div class="big">${num(s.value, 1)} <span class="small muted">${esc(unit)}</span></div></div>
      <div class="small muted">${esc(s.about)}</div>
      <p class="rot-verdict">${esc(s.verdict)}</p>
      ${stepsTable({ ...s, unit }, false)}
      ${skipped ? `<div class="rot-skipped">${skipped}</div>` : ''}</div>`;
}

function fightTable(d, list) {
  const body = list.map((s) => `<tr>
      <td><b>${esc(s.title)}</b><div class="small muted">${esc(s.about)}</div></td>
      <td class="num">${num(s.value, 1)}</td>
      <td>${s.steps.map((st) => `<div class="heal-step">${esc(st.label)}<div class="small muted">${esc(st.rule)}</div></div>`).join('')}</td></tr>`).join('');
  return `<table class="compact dmg-fights"><thead><tr><th>${esc(t('Fight'))}</th><th class="num">${esc(d.unit)}</th><th>${esc(t('What to cast'))}</th></tr></thead>
      <tbody>${body}</tbody></table>`;
}

function weightTable(d) {
  const w = d.weights;
  const cell = (row, name) => `<td class="num">${num(row[name], 2)}</td>
      <td class="num muted">${row[`${name}Power`] === null ? dash : num(row[`${name}Power`], 2)}</td>`;
  const body = w.rows.map((row) => `<tr><td>${esc(row.label)}</td>${cell(row, 'short')}${cell(row, 'long')}</tr>`).join('');
  const fight = (seconds) => esc(t('Fight of {seconds} s', { seconds: num(seconds) }));
  return `<table class="compact heal-weights"><thead>
      <tr><th></th><th class="num" colspan="2">${fight(w.seconds)}</th><th class="num" colspan="2">${fight(w.longSeconds)}</th></tr>
      <tr><th>${esc(t('One point of'))}</th><th class="num">${esc(d.unit)}</th><th class="num">${esc(t('= spell power'))}</th>
        <th class="num">${esc(d.unit)}</th><th class="num">${esc(t('= spell power'))}</th></tr></thead><tbody>${body}</tbody></table>`;
}

// The whole section from the answer of the bridge command "damage".
export function damageSection(d) {
  const top = TOP.map((key) => d.rotations.find((s) => s.key === key)).filter(Boolean);
  const others = d.rotations.filter((s) => !TOP.includes(s.key));
  const rotationTerms = terms([
    [t('Maximum damage'), t('The rotation that deals the most damage in the fight your gear is rated for when mana is no limit. The badge says how long your mana pays for it.')],
    [t('Sustained damage'), t('The rotation for a fight that never ends: it deals the most over 10 minutes, with everything that brings mana back in it.')],
    [t('Per min'), t('Casts per minute over the fight.')],
    [t('Time'), t('Share of the fight you spend casting the spell.')],
    ['DPS', t('Damage per second of the whole fight that comes from the spell.')],
    [t('Left out'), t('A spell you know that the best rotation does not cast, with the DPS you would lose by casting it anyway.')],
  ], t('Every fight is played cast by cast with each rotation you could cast; the best one is shown. The page “Rotations” has the same fights with their start cast by cast.'));
  const weightTerms = terms([
    ['DPS', t('Damage per second a point of the stat adds to the fight your gear is rated for.')],
    [t('= spell power'), t('How many points of spell power add the same.')],
  ], t('Hit counts only until no spell misses any more. A stat that gives mana or health counts only where the mana runs out.'));
  const fightHelp = `${esc(t('The best rotation for the other fights of the rotation guide. Against several enemies the damage is that on all of them together.'))}
      <p>${esc(t('Your gear is rated by a faster estimate of the normal fight: {value} {unit} over {seconds} s. It leaves out the start of the fight, in which the spells over time go up one after the other.', { value: num(d.rated.value, 1), unit: d.unit, seconds: num(d.rated.seconds) }))}</p>`;
  return `<h2 class="heading">${esc(t('Damage'))} ${help(d.notes.map((n) => `<p>${esc(n)}</p>`).join(''))}
      <span class="spacer"></span><span class="small muted">${esc(t('combat model {version}', { version: d.engine.version }))}</span></h2>
    ${d.engine.spellbookSource === 'assumed' ? `<div class="notice small">${t('No spellbook in the export: every trainer rank up to your level is assumed. Update the WoWMCP addon and type <code>/reload</code>, then the page knows your spells.')}</div>` : ''}
    ${statTiles(d)}
    <h3 class="dmg-sub">${esc(t('Which rotation'))} ${help(rotationTerms)}</h3>
    <div class="dmg-top">${top.map((s) => topCard(s, d.unit)).join('')}</div>
    <div class="card heal-card dmg-spells">${spellTable(d)}</div>
    <div class="heal-grid dmg-grid">
      <div class="card"><h2 class="heading">${esc(t('The other fights'))} ${help(fightHelp)}</h2>${fightTable(d, others)}</div>
      <div class="card"><h2 class="heading">${esc(t('What a point adds'))} ${help(weightTerms)}</h2>${weightTable(d)}</div>
    </div>`;
}
