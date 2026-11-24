// WoW Tools Forever, the desktop app: the page. This file wires the pages to the navigation, the character
// selection and the language; core.js holds what the pages share, pages/*.js one page each.
import { $, api, clearTabs, content, errorBox, esc, hydrateIcons, hydrateNavIcons, loadStatus, loading, pages, resetTips, restoreView, saveView, showUnit, state, updateLiveState } from './core.js';
import { mark, setLanguage, t } from './i18n.js';
import { renderConnection, showUpdate } from './pages/connection.js';
import { renderCharacter } from './pages/character.js';
import { renderWrits } from './pages/writs.js';
import { renderUpgrades } from './pages/upgrades.js';
import { renderRecipes } from './pages/recipes.js';
import { renderConsumables } from './pages/consumables.js';
import { renderRotations } from './pages/rotations.js';
import { renderGoldPerHour } from './pages/goldperhour.js';
import { renderPriceHistory } from './pages/pricehistory.js';

Object.assign(pages, {
  connection: { title: mark('Connection'), render: renderConnection, accountWide: true },
  character: { title: mark('Character'), render: renderCharacter },
  writs: { title: mark('Writs and crates'), render: renderWrits },
  upgrades: { title: mark('AH upgrades'), render: renderUpgrades },
  rotations: { title: mark('Rotations'), render: renderRotations },
  recipes: { title: mark('Professions'), render: renderRecipes },
  consumables: { title: mark('Consumables'), render: renderConsumables },
  pricehistory: { title: mark('Price history'), render: renderPriceHistory, accountWide: true },
  goldperhour: { title: mark('Gold per hour'), render: renderGoldPerHour, accountWide: true },
});

// ---------- Language ----------
// The main process decides the language (setting, else the game client, else the system). The fixed texts
// of index.html carry their English text in data-i18n and are filled in here.

function applyLanguage(settings) {
  document.documentElement.lang = setLanguage(settings.activeLanguage);
  for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);
  for (const el of document.querySelectorAll('[data-i18n-title]')) el.title = t(el.dataset.i18nTitle);
  for (const el of document.querySelectorAll('[data-i18n-help]')) {
    el.dataset.help = esc(t(el.dataset.i18nHelp));
    el.setAttribute('aria-label', t('Explanation'));
  }
  $('#lang-select').value = settings.language;
  $('#app-version').textContent = settings.version ? `v${settings.version}` : '';
}

$('#lang-select').addEventListener('change', async (e) => {
  applyLanguage(await api.setSettings({ language: e.target.value }));
  state.cache.clear();
  content.innerHTML = loading(t('Reloading data …'));
  await loadStatus();
  showPage(state.page);
});

// ---------- Navigation ----------

async function showPage(page) {
  window.__idle = false;
  state.page = page;
  resetTips();
  clearTabs();
  document.querySelectorAll('#nav button').forEach((b) => b.classList.toggle('active', b.dataset.page === page));
  $('#page-title').textContent = t(pages[page].title);
  const wide = !!pages[page].accountWide;
  $('#page-scope').hidden = !wide;
  $('#unit').classList.toggle('unit-muted', wide);
  content.scrollTop = 0;
  saveView();
  try {
    await pages[page].render();
    await hydrateIcons();
  } catch (err) {
    content.innerHTML = errorBox(err.message || String(err));
  }
  window.__idle = true;
}
window.__showPage = showPage;

$('#nav').addEventListener('click', (e) => {
  const button = e.target.closest('button[data-page]');
  if (button) showPage(button.dataset.page);
});
$('#char-select').addEventListener('change', (e) => {
  state.char = e.target.value;
  showUnit();
  showPage(state.page);
});
// The options of the pages change in their own handlers, which run before the event arrives here.
document.addEventListener('change', saveView);
$('#refresh').addEventListener('click', async () => {
  state.cache.clear();
  content.innerHTML = loading(t('Reloading data …'));
  await loadStatus();
  showPage(state.page);
});

api.onDataChanged(async () => {
  // WoW wrote SavedVariables (/reload or logout): reload everything that depends on them. The client's
  // language may have changed too.
  applyLanguage(await api.getSettings());
  state.cache.clear();
  state.gph.notice = null;
  await loadStatus();
  showPage(state.page);
});

// The main process computed the lists for the game again (at the start, after a /reload, after "Send now").
api.onGameData((result) => {
  state.ingame = result;
  if (state.page === 'connection' && window.__idle) showPage('connection');
});

// The app's own update: the main process reports every step (found, the progress of the download, ready).
api.onUpdate((update) => {
  state.update = update;
  showUpdate();
});
// The note in the sidebar: a downloaded update is installed from here, anything else is on the Connection page.
$('#update-pill').addEventListener('click', () => {
  if (state.update?.phase === 'ready' && state.update.canInstall) api.installUpdate();
  else showPage('connection');
});

// ---------- Start ----------

const settings = await api.getSettings();
applyLanguage(settings);
if (!settings.capture) restoreView(); // screenshots always show the pages as a new user sees them
await loadStatus();
// Screenshots wait for the lists; otherwise the card fills in when the sync is done.
if (settings.capture && settings.captureChar) {
  // --char=<name> picks the character of the screenshots (the start of its name is enough).
  const wanted = settings.captureChar.toLowerCase();
  const found = (state.status?.characters || []).find((c) => !c.error && (c.name || c.key).toLowerCase().startsWith(wanted));
  if (found) { state.char = found.key; $('#char-select').value = found.key; showUnit(); }
}
if (settings.capture) await hydrateNavIcons();
else hydrateNavIcons();
if (settings.capture) state.ingame = await api.gameData();
else api.gameData().then((result) => { state.ingame ||= result; });
api.updateState().then((update) => {
  state.update ||= update;
  showUpdate();
});
showPage(pages[state.page] ? state.page : 'connection');
// Game process and snapshot ages change without file writes; refresh the light status every 30 s.
setInterval(async () => {
  state.game = await api.wowRunning();
  updateLiveState();
}, 30000);
