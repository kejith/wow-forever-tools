// The section "Healing" of the Character page: every heal and shield rank the character knows with what one
// cast gives and costs, which rank the rotation guide casts when, the regeneration and what a point of each
// stat adds. From the combat engine through scripts/healing.py; only asked for when the class heals.
import { DASH as dash, esc, help, hintAttr, num, percent, terms, th } from '../core.js';
import { t } from '../i18n.js';
import { clock } from './rotations.js';

// What one cast does without a crit, as a tooltip says it.
function hit(r) {
  const parts = [];
  if (r.absorb) parts.push(t('absorbs {n}', { n: num(r.absorb) }));
  if (r.max) parts.push(r.min === r.max ? num(r.min) : `${num(r.min)}–${num(r.max)}`);
  if (r.ticks) parts.push(t('{ticks} × {tick} over {seconds} s', { ticks: num(r.ticks), tick: num(r.perTick, 0), seconds: num(r.durationSec) }));
  return parts.join(' + ');
}

const best = (text, is, why) => (is ? `<b class="heal-best"${hintAttr(why)}>${text}</b>` : text);

function rankRow(r, first) {
  const hps = r.hpsOnTarget !== null
    ? `${num(r.hpsOnTarget, 1)}<div class="small muted">${esc(t('on the target'))}</div>`
    : best(num(r.hps, 1), r.best.includes('hps'), t('The most healing per second among the heals you can cast back to back.'));
  const alone = r.kind === 'hot' || r.kind === 'shield' ? dash
    : r.spamSeconds === null ? `<span class="good">${esc(t('lasts'))}</span>`
      : `${esc(clock(r.spamSeconds))}<div class="small muted">${esc(t('{n} healed', { n: num(r.spamHealing) }))}</div>`;
  const uses = r.uses.map((u) => `<span class="badge${u.key === 'sustained' ? ' ok' : ''}">${esc(u.title)}</span>`).join(' ');
  return `<tr${first ? ' class="heal-chain"' : ''}>
      <td><span class="nowrap">${esc(r.label)}</span>
        <div class="small muted nowrap">${esc(t('from level {level}', { level: r.level }))}${r.bonusLossPct ? ` <span class="warn-text"${hintAttr(t('Your level is far above this rank: it gets {pct}% less of your healing bonus.', { pct: r.bonusLossPct }))}>· ${esc(t('outgrown'))}</span>` : ''}</div>
        ${uses ? `<div class="heal-uses">${uses}</div>` : ''}</td>
      <td class="nowrap">${esc(hit(r))}${r.critPct ? `<div class="small muted">${esc(t('{pct} crit', { pct: percent(num(r.critPct, 1)) }))}</div>` : ''}</td>
      <td class="num"><b>${num(r.perCast, 0)}</b></td>
      <td class="num">${num(r.mana)}</td>
      <td class="num">${r.castSec ? esc(t('{n} s', { n: num(r.castSec, 1) })) : esc(t('instant'))}</td>
      <td class="num">${hps}</td>
      <td class="num">${best(num(r.hpm, 2), r.best.includes('hpm'), t('The most healing per mana of all your heals.'))}</td>
      <td class="num">${esc(percent(num(r.coefPct, 0)))}<div class="small muted">${esc(t('+{n} now', { n: num(r.bonus, 0) }))}</div></td>
      <td class="num">${alone}</td></tr>`;
}

function rankTable(d) {
  const head = th(
    [t('Spell'), '', `${esc(t('Every heal and shield rank you know, with your talents and your healing bonus in the numbers. “outgrown”: your level is far above the rank, so it gets less of the healing bonus.'))}
      <p>${esc(t('The labels below a rank are the situations in which the rotation guide casts it (see “Which rank when” below). “Sustained” is the fight your gear is rated for.'))}</p>`],
    [t('One cast'), '', esc(t('What one cast does without a crit, as the tooltip shows it: the range of a direct heal, the ticks of a heal over time, what a shield absorbs.'))],
    [t('Average'), 'num', esc(t('Healing of one cast on average: crits weighed in, all ticks counted.'))],
    [t('Mana'), 'num', esc(t('Mana cost with your talents.'))],
    [t('Cast time'), 'num', esc(t('An instant spell still takes the global cooldown of 1.5 seconds.'))],
    ['HPS', 'num', esc(t('Healing per second of casting: the average divided by the cast time (the global cooldown for an instant). A heal over time and a shield fit on a target only once, so theirs is the healing per second on that target while they run.'))],
    [t('Per mana'), 'num', esc(t('Healing per point of mana: the number that decides how long your mana lasts.'))],
    [t('Coefficient'), 'num', esc(t('What a point of healing bonus adds to one cast of this rank, in percent: with talents, crits and the loss of an outgrown rank in it. Below: what your healing bonus adds to the average now.'))],
    [t('Nothing else'), 'num', esc(t('Casting only this rank without a pause: how long a full mana bar lasts with the regeneration while casting, and what it heals until then. “lasts”: the regeneration pays for it.'))],
  );
  let chain = null;
  const rows = d.rows.map((r) => {
    const first = chain !== null && r.baseId !== chain;
    chain = r.baseId;
    return rankRow(r, first);
  }).join('');
  return `<table class="sticky compact heal-table"><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table>`;
}

function statTiles(d) {
  const s = d.stats;
  const tile = (label, value, sub = '') => `<div class="heal-tile"><div class="small muted">${esc(label)}</div>
      <div class="big">${value}</div>${sub ? `<div class="small muted">${esc(sub)}</div>` : ''}</div>`;
  return `<div class="heal-tiles">
      ${tile(t('Healing bonus'), num(s.healing))}
      ${tile(t('Spell crit'), esc(percent(num(s.critPct, 1))))}
      ${tile(t('Mana'), num(s.manaMax))}
      ${tile(t('Mana per 5 sec. while casting'), num(s.castingPer5, 1), s.castingRegenPct ? t('gear {gear} + {pct} of the spirit regeneration', { gear: num(s.gearMp5), pct: percent(num(s.castingRegenPct, 0)) }) : t('gear {gear}', { gear: num(s.gearMp5) }))}
      ${tile(t('Mana per 5 sec. after 5 s without a cast'), num(s.idlePer5, 1), t('gear {gear} + spirit {spirit}', { gear: num(s.gearMp5), spirit: num(s.spiritPer5, 1) }))}
    </div>`;
}

function whenTable(d) {
  const lasts = (seconds) => (seconds === null ? `<span class="badge ok">${esc(t('Mana lasts'))}</span>`
    : `<span class="badge warn">${esc(t('Mana lasts {time}', { time: clock(seconds) }))}</span>`);
  const rows = d.recommended.map((s) => `<tr>
      <td><b>${esc(s.title)}</b><div class="small muted">${esc(s.about)}</div></td>
      <td class="num">${num(s.value, 1)}</td>
      <td>${lasts(s.oomSeconds)}</td>
      <td>${s.steps.map((st) => `<div class="heal-step">${esc(st.label)}<div class="small muted">${esc(st.rule)}</div></div>`).join('')}</td></tr>`).join('');
  const sus = d.sustained;
  const sustained = `<tr>
      <td><b>${esc(t('Sustained'))}</b><div class="small muted">${esc(t('As much as your mana pays for over {seconds} seconds: the number your gear is rated by.', { seconds: num(sus.seconds) }))}</div></td>
      <td class="num">${num(sus.value, 1)}</td>
      <td><span class="badge">${esc(t('{value} HPS over {seconds} s', { value: num(sus.longValue, 1), seconds: num(sus.longSeconds) }))}</span></td>
      <td>${sus.steps.map((st) => `<div class="heal-step">${esc(st.label)}<div class="small muted">${esc(t('{value} HPS', { value: num(st.value, 1) }))}</div></div>`).join('')}</td></tr>`;
  return `<table class="compact heal-when"><thead><tr><th>${esc(t('Situation'))}</th><th class="num">HPS</th><th></th><th>${esc(t('What to cast'))}</th></tr></thead>
      <tbody>${rows}${sustained}</tbody></table>`;
}

function weightTable(d) {
  const w = d.weights;
  const cell = (row, name) => `<td class="num">${num(row[name], 2)}</td>
      <td class="num muted">${row[`${name}Bonus`] === null ? dash : num(row[`${name}Bonus`], 2)}</td>`;
  const rows = w.rows.map((row) => `<tr><td>${esc(row.label)}</td>${cell(row, 'short')}${cell(row, 'long')}</tr>`).join('');
  const fight = (seconds) => esc(t('Fight of {seconds} s', { seconds: num(seconds) }));
  return `<table class="compact heal-weights"><thead>
      <tr><th></th><th class="num" colspan="2">${fight(w.seconds)}</th><th class="num" colspan="2">${fight(w.longSeconds)}</th></tr>
      <tr><th>${esc(t('One point of'))}</th><th class="num">HPS</th><th class="num">${esc(t('= healing bonus'))}</th>
        <th class="num">HPS</th><th class="num">${esc(t('= healing bonus'))}</th></tr></thead><tbody>${rows}</tbody></table>`;
}

// The whole section from the answer of the bridge command "healing".
export function healingSection(d) {
  const weightTerms = terms([
    ['HPS', t('Healing per second a point of the stat adds to the sustained healing: as much as your mana pays for over the fight.')],
    [t('= healing bonus'), t('How many points of healing bonus add the same.')],
  ], t('A short fight is limited by the mana bar, so intellect counts; a long one by the regeneration, so spirit and mana per 5 sec. count.'));
  return `<h2 class="heading">${esc(t('Healing'))} ${help(d.notes.map((n) => `<p>${esc(n)}</p>`).join(''))}
      <span class="spacer"></span><span class="small muted">${esc(t('combat model {version}', { version: d.engine.version }))}</span></h2>
    ${d.engine.spellbookSource === 'assumed' ? `<div class="notice small">${t('No spellbook in the export: every trainer rank up to your level is assumed. Update the WoWMCP addon and type <code>/reload</code>, then the page knows your spells.')}</div>` : ''}
    ${statTiles(d)}
    <div class="card heal-card">${rankTable(d)}</div>
    <div class="heal-grid">
      <div class="card"><h2 class="heading">${esc(t('Which rank when'))} ${help(esc(t('For each level of incoming damage the spells that supply that much healing per second for the least mana, and how long your mana lasts. The page “Rotations” shows the same plans with their numbers.')))}</h2>${whenTable(d)}</div>
      <div class="card"><h2 class="heading">${esc(t('What a point adds'))} ${help(weightTerms)}</h2>${weightTable(d)}</div>
    </div>`;
}
