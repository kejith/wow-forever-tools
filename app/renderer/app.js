// WoW Forever Tools, the desktop app: the page. This file wires the pages to the navigation, the character
// selection and the language; core.js holds what the pages share, pages/*.js one page each.
import { $, api, content, errorBox, hydrateIcons, loadStatus, loading, pages, resetTips, state, updateLiveState } from './core.js';
import { mark, setLanguage, t } from './i18n.js';
import { renderConnection } from './pages/connection.js';
import { renderCharacter } from './pages/character.js';
import { renderWrits } from './pages/writs.js';
import { renderUpgrades } from './pages/upgrades.js';
import { renderRecipes } from './pages/recipes.js';
import { renderGoldPerHour } from './pages/goldperhour.js';

Object.assign(pages, {
  connection: { title: mark('Connection'), render: renderConnection },
  character: { title: mark('Character'), render: renderCharacter },
  writs: { title: mark("Craftsman's writs"), render: renderWrits },
  upgrades: { title: mark('AH upgrades'), render: renderUpgrades },
  recipes: { title: mark('Professions and recipes'), render: renderRecipes },
  goldperhour: { title: mark('Gold per hour'), render: renderGoldPerHour },
});

// ---------- Language ----------
// The main process decides the language (setting, else the game client, else the system). The fixed texts
// of index.html carry their English text in data-i18n and are filled in here.

function applyLanguage(settings) {
  document.documentElement.lang = setLanguage(settings.activeLanguage);
  for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);
  for (const el of document.querySelectorAll('[data-i18n-title]')) el.title = t(el.dataset.i18nTitle);
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
  document.querySelectorAll('#nav button').forEach((b) => b.classList.toggle('active', b.dataset.page === page));
  $('#page-title').textContent = t(pages[page].title);
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
  showPage(state.page);
});
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

// ---------- Start ----------

applyLanguage(await api.getSettings());
await loadStatus();
showPage('connection');
// Game process and snapshot ages change without file writes; refresh the light status every 30 s.
setInterval(async () => {
  state.game = await api.wowRunning();
  updateLiveState();
}, 30000);
