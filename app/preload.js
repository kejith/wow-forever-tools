// Exposes a small, fixed API to the renderer; the page itself has no Node access.
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('wow', {
  bridge: (command, opts) => ipcRenderer.invoke('bridge', command, opts),
  addonInfo: () => ipcRenderer.invoke('addon:info'),
  installAddon: () => ipcRenderer.invoke('addon:install'),
  createRequest: (action, opts) => ipcRenderer.invoke('request:create', action, opts),
  icons: (itemIds) => ipcRenderer.invoke('icons', itemIds),
  wowRunning: () => ipcRenderer.invoke('wow:running'),
  openPath: (p) => ipcRenderer.invoke('open:path', p),
  getSettings: () => ipcRenderer.invoke('settings:get'),
  setSettings: (patch) => ipcRenderer.invoke('settings:set', patch),
  onDataChanged: (fn) => ipcRenderer.on('data-changed', (_e, info) => fn(info)),
});
