// Language of the page. The English text is the key: t('Open folder') returns it unchanged in English and
// the entry of locales/de.js in German; a text without a translation stays English. {name} placeholders are
// filled in from the second argument. Texts that pass through a table first are wrapped in mark(), so the
// catalog test (npm test) finds them, and translated with t() where they are shown.
// No DOM in here, so Node can load the module in the tests.
import de from './locales/de.js';

const CATALOGS = { de };
const LOCALES = { en: 'en-US', de: 'de-DE' };
let current = 'en';

export function setLanguage(lang) {
  current = Object.hasOwn(LOCALES, lang) ? lang : 'en';
  return current;
}

export const language = () => current;

// BCP 47 tag for number and date formatting.
export const locale = () => LOCALES[current];

export function t(text, params) {
  const found = CATALOGS[current]?.[text] ?? text;
  return params ? found.replace(/\{(\w+)\}/g, (all, key) => (key in params ? String(params[key]) : all)) : found;
}

export const mark = (text) => text;
