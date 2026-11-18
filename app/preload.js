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
  icons: (itemIds) => ipcRenderer.invoke('icons', itemIds),
  wowRunning: () => ipcRenderer.invoke('wow:running'),
  openPath: (p) => ipcRenderer.invoke('open:path', p),
  getSettings: () => ipcRenderer.invoke('settings:get'),
  setSettings: (patch) => ipcRenderer.invoke('settings:set', patch),
  onDataChanged: (fn) => ipcRenderer.on('data-changed', (_e, info) => fn(info)),
});
