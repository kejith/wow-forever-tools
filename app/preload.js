// Exposes a small, fixed API to the renderer; the page itself has no Node access.
// getSettings() also tells the page its language: { language: auto|en|de, activeLanguage: en|de, clientLocale }.
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('wow', {
  bridge: (command, opts) => ipcRenderer.invoke('bridge', command, opts),
  addonInfo: (key, opts) => ipcRenderer.invoke('addon:info', key, opts),
  installAddon: (key) => ipcRenderer.invoke('addon:install', key),
  gphRequest: (req) => ipcRenderer.invoke('gph:request', req),
  gphClearRequest: () => ipcRenderer.invoke('gph:clear'),
  createRequest: (action, opts) => ipcRenderer.invoke('request:create', action, opts),
  // What the WoWMCP addon shows in the game: gameData() is the last sync (or waits for the running one),
  // sendGameData() computes and writes now, onGameData(fn) hears every finished sync.
  gameData: () => ipcRenderer.invoke('gamedata:get'),
  sendGameData: () => ipcRenderer.invoke('gamedata:send'),
  onGameData: (fn) => ipcRenderer.on('game-data', (_e, result) => fn(result)),
  // The app's own update: updateState() is what the updater knows, onUpdate(fn) hears every change (also the
  // progress of a download); the three steps answer with the state they reached.
  updateState: () => ipcRenderer.invoke('update:get'),
  checkUpdate: () => ipcRenderer.invoke('update:check'),
  downloadUpdate: () => ipcRenderer.invoke('update:download'),
  installUpdate: () => ipcRenderer.invoke('update:install'),
  openReleaseNotes: () => ipcRenderer.invoke('update:notes'),
  onUpdate: (fn) => ipcRenderer.on('app-update', (_e, state) => fn(state)),
  icons: (itemIds) => ipcRenderer.invoke('icons', itemIds),
  wowRunning: () => ipcRenderer.invoke('wow:running'),
  openPath: (p) => ipcRenderer.invoke('open:path', p),
  getSettings: () => ipcRenderer.invoke('settings:get'),
  setSettings: (patch) => ipcRenderer.invoke('settings:set', patch),
  onDataChanged: (fn) => ipcRenderer.on('data-changed', (_e, info) => fn(info)),
});
