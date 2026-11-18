// Language of the texts the main process produces: error messages of addon.js and releases.js and of the
// bridge call. The English text is the key, German lives in locales/de.js; a text without a translation
// stays English. The page has its own copy of this for its texts (renderer/i18n.js).
// No Electron, so addon.js and releases.js stay testable with plain Node.
const fs = require('node:fs');
const path = require('node:path');

const LANGUAGES = ['en', 'de'];
const catalogs = { de: require('./locales/de') };
let language = 'en';

function setLanguage(lang) {
  language = LANGUAGES.includes(lang) ? lang : 'en';
  return language;
}

// `text` in the selected language, with {name} placeholders filled in from `params`.
function t(text, params) {
  const found = catalogs[language]?.[text] ?? text;
  return params ? found.replace(/\{(\w+)\}/g, (all, key) => (key in params ? String(params[key]) : all)) : found;
}

// Text locale of the game client, e.g. "deDE", from WTF/Config.wtf. null if it cannot be read.
function clientLocale(wowDir) {
  try {
    const config = fs.readFileSync(path.join(wowDir, 'WTF', 'Config.wtf'), 'utf8');
    return (config.match(/^SET\s+textLocale\s+"(\w+)"/m) || [])[1] || null;
  } catch {
    return null;
  }
}

const languageOf = (locale) => (String(locale || '').toLowerCase().startsWith('de') ? 'de' : 'en');

module.exports = { LANGUAGES, setLanguage, language: () => language, t, clientLocale, languageOf };
