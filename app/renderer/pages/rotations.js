// Page "Rotations": what the selected character casts in which situation, from the combat engine through
// scripts/rotations.py. A damage dealer gets the best rotation for several kinds of fights, a healer the
// cheapest spells for several levels of incoming damage. The sentences come from the script; the page lays
// them out and explains the columns once per guide.
import { $, DASH as dash, api, cached, content, errorBox, esc, help, loadStatus, loading, num, percent, selectOption, state, terms } from '../core.js';
import { t } from '../i18n.js';

// A time in a fight: "45 s" or "2:24 min".
export const clock = (seconds) => (seconds < 60 ? t('{n} s', { n: Math.round(seconds) })
  : t('{m}:{s} min', { m: Math.floor(seconds / 60), s: String(Math.floor(seconds % 60)).padStart(2, '0') }));

const share = (pct) => (pct >= 0.5 ? percent(num(pct, 0)) : dash);

// The cards repeat, so their columns are explained once, behind the "?" at the guide's heading.
const guideTerms = (g) => (g.metric === 'hps' ? terms([
  [t('Per min'), t('Casts per minute at this level of damage.')],
  [t('Time'), t('Share of the time you spend casting the spell. What is left is a pause, in which mana regenerates.')],
  ['HPS', t('Healing per second the spell supplies at this pace.')],
  [t('Mana/s'), t('Mana per second the spell costs at this pace.')],
  [t('Mana lasts'), t('How long your full mana bar pays for this healing. “Mana lasts” without a time: the regeneration covers it.')],
], [g.about, ...g.notes].join(' ')) : terms([
  [t('Per min'), t('Casts per minute over the fight.')],
  [t('Time'), t('Share of the fight you spend casting the spell.')],
  ['DPS', t('Damage per second of the whole fight that comes from the spell.')],
  [t('Left out'), t('A spell you know that the best rotation does not cast, with the DPS you would lose by casting it anyway.')],
  [t('Cast by cast'), t('The start of the played fight: every action with your mana and health right before it.')],
], g.about));

function stepsTable(s, healer) {
  const rows = s.steps.map((st, i) => `<tr>
      <td class="muted num">${i + 1}</td>
      <td>${esc(st.label)}<div class="small muted">${esc(st.rule)}</div></td>
      <td class="num">${st.perMin >= 0.1 ? num(st.perMin, 1) : dash}</td>
      <td class="num">${share(st.sharePct)}</td>
      <td class="num">${st.value ? `<b>${num(st.value, 1)}</b>` : dash}</td>
      ${healer ? `<td class="num">${st.manaPerSec ? num(st.manaPerSec, 1) : dash}</td>` : ''}</tr>`).join('');
  return `<table class="rot-steps"><thead><tr><th></th><th>${esc(t('Spell'))}</th><th class="num">${esc(t('Per min'))}</th>
      <th class="num">${esc(t('Time'))}</th><th class="num">${esc(s.unit)}</th>${healer ? `<th class="num">${esc(t('Mana/s'))}</th>` : ''}</tr></thead>
    <tbody>${rows}</tbody></table>`;
}

const meter = (pct, cls) => `<div class="rot-meter ${cls}"><span style="width:${Math.max(0, Math.min(100, pct))}%"></span></div>`;

function timeline(s) {
  if (!s.timeline.length) return '';
  const rows = s.timeline.map((e) => `<tr><td class="num muted">${esc(clock(e.t))}</td>
      <td>${esc(e.label)}${e.n > 1 ? ` <span class="muted">×${e.n}</span>` : ''}</td>
      <td class="num">${esc(percent(e.manaPct))}</td><td>${meter(e.manaPct, 'mana')}</td>
      <td class="num">${esc(percent(e.hpPct))}</td><td>${meter(e.hpPct, 'health')}</td></tr>`).join('');
  return `<details class="scale rot-timeline"><summary>${esc(t('Cast by cast: how the fight starts'))}</summary>
      <table><thead><tr><th class="num">${esc(t('At'))}</th><th>${esc(t('Action'))}</th><th colspan="2">${esc(t('Mana'))}</th>
        <th colspan="2">${esc(t('Health'))}</th></tr></thead><tbody>${rows}</tbody></table>
      ${s.more ? `<p class="small muted">${esc(t('… and {n} more actions', { n: s.more }))}</p>` : ''}</details>`;
}

function card(s, healer) {
  // For a healer the answer is how long the mana pays for it.
  const mana = !healer ? ''
    : s.oomSeconds == null ? `<span class="badge ok">${esc(t('Mana lasts'))}</span>`
      : `<span class="badge warn">${esc(t('Mana lasts {time}', { time: clock(s.oomSeconds) }))}</span>`;
  const skipped = s.skipped.map((k) => `<div class="small muted">${esc(t('Left out'))}: <span class="rot-name">${esc(k.label)}</span>
      <span class="warn-text">−${num(k.loss, 1)} ${esc(s.unit)}</span>${k.why ? ` · ${esc(k.why)}` : ''}</div>`).join('');
  return `<div class="card rot-card" data-key="${esc(s.key)}">
      <div class="rot-head"><h3>${esc(s.title)} ${mana}</h3><div class="big">${num(s.value, 1)} <span class="small muted">${esc(s.unit)}</span></div></div>
      <div class="small muted">${esc(s.about)}</div>
      <p class="rot-verdict">${esc(s.verdict)}</p>
      ${stepsTable(s, healer)}
      ${skipped ? `<div class="rot-skipped">${skipped}</div>` : ''}${timeline(s)}</div>`;
}

export async function renderRotations() {
  if (!state.status) await loadStatus();
  if (!state.char) { content.innerHTML = `<div class="notice">${esc(t('No character exported.'))}</div>`; return; }
  const o = state.rotations;
  content.innerHTML = `<div class="controls">
      <label><span class="label">${esc(t('Normal fight (s)'))} ${help(esc(t('Length of the fight called “Normal fight”: the same fight your gear is rated for on the upgrades page. The other fights have fixed lengths.')))}</span>
        <input type="number" id="r-fight" min="10" max="600" step="10" value="${o.fight}"></label>
      <label>${esc(t('Enemy level'))}<select id="r-target">${selectOption('', t('automatic'), o.target)}${[0, 1, 2, 3].map((n) => selectOption(String(n), `+${n}`, o.target)).join('')}</select></label>
      <label class="check"><input type="checkbox" id="r-cons" ${o.consumables ? 'checked' : ''}> <span class="label">${esc(t('with consumables'))} ${help(esc(t('Plays every fight with the best set of consumables for the normal fight active (the page “Consumables” shows it). What a potion gives per second is that of the normal fight in every fight.')))}</span></label>
      <span class="spacer"></span><div class="page-meta"><span id="r-meta"></span></div>
    </div><div id="r-body">${loading(t('Playing the fights with every rotation you could cast …'))}</div>`;
  $('#r-fight').addEventListener('change', (e) => { o.fight = Math.min(600, Math.max(10, Number(e.target.value) || 60)); renderRotations(); });
  $('#r-target').addEventListener('change', (e) => { o.target = e.target.value; renderRotations(); });
  $('#r-cons').addEventListener('change', (e) => { o.consumables = e.target.checked; renderRotations(); });
  const d = await cached(`rotations:${state.char}:${o.fight}:${o.target}:${o.consumables}`, () => api.bridge('rotations', {
    char: state.char, fightSeconds: Number(o.fight) || 60, targetDelta: o.target === '' ? -1 : Number(o.target),
    consumables: o.consumables,
  }));
  const body = $('#r-body');
  if (!body) return;
  if (d.error) { body.innerHTML = errorBox(d.error); return; }
  $('#r-meta').textContent = [t('Level {level}', { level: d.level }), t('combat model {version}', { version: d.engine.version })].join(' · ');
  body.innerHTML = `${d.engine.spellbookSource === 'assumed' ? `<div class="notice small">${t('No spellbook in the export: every trainer rank up to your level is assumed. Update the WoWMCP addon and type <code>/reload</code>, then the page knows your spells.')}</div>` : ''}
    ${d.guides.map((g, i) => `<div class="section rot-guide" id="r-${esc(g.metric)}"${i ? '' : ' style="margin-top:0"'}>
      <h2 class="heading">${esc(g.title)} ${help(guideTerms(g))}</h2>
      ${g.consumables ? `<div class="notice small">${esc(g.consumables.text)}</div>` : ''}
      <div class="rot-grid">${g.situations.map((s) => card(s, g.metric === 'hps')).join('')}</div></div>`).join('')}`;
}
