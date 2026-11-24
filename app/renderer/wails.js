// window.wow: the small, fixed API the page uses, on top of the Wails bindings (the exported methods of App in
// app/app.go, window.go.main.App.*) and the Wails events. Loaded before app.js; the page has no other way out.
// getSettings() also tells the page its language: { language: auto|en|de, activeLanguage: en|de, clientLocale }.
(() => {
  // Wails checks the number of arguments, so every call passes all of them.
  const call = (name, ...args) => window.go.main.App[name](...args);
  const on = (event) => (fn) => window.runtime.EventsOn(event, fn);
  window.wow = {
    bridge: (command, opts) => call('Bridge', command, opts ?? null),
    addonInfo: (key, opts) => call('AddonInfo', key, opts ?? null),
    installAddon: (key) => call('InstallAddon', key),
    gphRequest: (req) => call('GphRequest', req ?? {}),
    gphClearRequest: () => call('GphClearRequest'),
    createRequest: (action, opts) => call('CreateRequest', action, opts ?? null),
    // What the WoWToolsForever addon shows in the game: gameData() is the last sync (or waits for the running one),
    // sendGameData() computes and writes now, onGameData(fn) hears every finished sync.
    gameData: () => call('GameData'),
    sendGameData: () => call('SendGameData'),
    onGameData: on('game-data'),
    // The app's own update: updateState() is what the updater knows, onUpdate(fn) hears every change (also the
    // progress of a download); the three steps answer with the state they reached.
    updateState: () => call('UpdateState'),
    checkUpdate: () => call('CheckUpdate'),
    downloadUpdate: () => call('DownloadUpdate'),
    installUpdate: () => call('InstallUpdate'),
    openReleaseNotes: () => call('OpenReleaseNotes'),
    onUpdate: on('app-update'),
    icons: (itemIds) => call('Icons', Array.isArray(itemIds) ? itemIds : []),
    wowRunning: () => call('WowRunning'),
    openPath: (p) => call('OpenPath', String(p ?? '')),
    getSettings: () => call('GetSettings'),
    setSettings: (patch) => call('SetSettings', patch ?? {}),
    onDataChanged: on('data-changed'),
  };
})();
