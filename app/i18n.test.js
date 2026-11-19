// Checks the German catalogs against the source: node i18n.test.js (part of npm test).
// Every text handed to t() or mark(), and every data-i18n text of index.html, needs a German entry with the
// same placeholders and the same HTML tags, and a catalog must not hold entries nothing uses any more.
// Two catalogs: locales/de.js for the main process, renderer/locales/de.js for the page.
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const i18n = require('./i18n');

const CALL = /\b(?:t|mark)\(\s*(?:'((?:[^'\\\n]|\\.)*)'|"((?:[^"\\\n]|\\.)*)")/g;
const ATTRIBUTE = /\bdata-i18n(?:-title|-help)?="([^"]*)"/g;

// Texts of one file as Map text -> "file:line" of the first use.
function textsIn(file, into) {
  const source = fs.readFileSync(file, 'utf8');
  const pattern = file.endsWith('.html') ? ATTRIBUTE : CALL;
  for (const m of source.matchAll(pattern)) {
    const raw = m[1] ?? m[2];
    assert.ok(!/\\[^'"\\]/.test(raw), `${file}: escape sequence in ${raw}`);
    const text = raw.replace(/\\(.)/g, '$1');
    const line = source.slice(0, m.index).split('\n').length;
    if (!into.has(text)) into.set(text, `${path.relative(__dirname, file)}:${line}`);
  }
  return into;
}

const scripts = (dir) => fs.readdirSync(dir).filter((f) => f.endsWith('.js') && !f.endsWith('.test.js')).map((f) => path.join(dir, f));
const sorted = (text, pattern) => (text.match(pattern) || []).sort().join(' ');

function checkCatalog(name, used, catalog, atLeast) {
  assert.ok(used.size >= atLeast, `${name}: only ${used.size} texts found, the scan is broken`);
  const missing = [...used].filter(([text]) => !Object.hasOwn(catalog, text)).map(([text, where]) => `  ${where}: ${JSON.stringify(text)}`);
  assert.deepStrictEqual(missing, [], `${name}: no German text for\n${missing.join('\n')}`);
  const unused = Object.keys(catalog).filter((text) => !used.has(text));
  assert.deepStrictEqual(unused, [], `${name}: entries nothing uses\n  ${unused.join('\n  ')}`);
  for (const [text, german] of Object.entries(catalog)) {
    assert.ok(typeof german === 'string' && german.trim(), `${name}: empty entry for ${text}`);
    assert.strictEqual(sorted(german, /\{\w+\}/g), sorted(text, /\{\w+\}/g), `${name}: placeholders differ in ${JSON.stringify(text)}`);
    assert.strictEqual(sorted(german, /<\/?\w+>/g), sorted(text, /<\/?\w+>/g), `${name}: HTML tags differ in ${JSON.stringify(text)}`);
  }
  return used.size;
}

(async () => {
  // --- Main process ---
  const mainUsed = new Map();
  for (const file of scripts(__dirname)) textsIn(file, mainUsed);
  const mainCount = checkCatalog('locales/de.js', mainUsed, require('./locales/de'), 25);

  assert.strictEqual(i18n.t('Unknown action: {action}', { action: 'x' }), 'Unknown action: x');
  assert.strictEqual(i18n.setLanguage('de'), 'de');
  assert.strictEqual(i18n.t('Unknown action: {action}', { action: 'x' }), 'Unbekannte Aktion: x');
  assert.strictEqual(i18n.t('a text nobody translated'), 'a text nobody translated');
  assert.strictEqual(i18n.setLanguage('fr'), 'en'); // unknown languages fall back to English
  assert.strictEqual(i18n.languageOf('deDE'), 'de');
  assert.strictEqual(i18n.languageOf('de-AT'), 'de');
  assert.strictEqual(i18n.languageOf('enUS'), 'en');
  assert.strictEqual(i18n.languageOf(null), 'en');

  // The client's language comes from WTF/Config.wtf.
  const wow = fs.mkdtempSync(path.join(require('node:os').tmpdir(), 'wow-i18n-'));
  assert.strictEqual(i18n.clientLocale(wow), null);
  fs.mkdirSync(path.join(wow, 'WTF'));
  fs.writeFileSync(path.join(wow, 'WTF', 'Config.wtf'), 'SET portal "EU"\r\nSET textLocale "deDE"\r\nSET audioLocale "enUS"\r\n');
  assert.strictEqual(i18n.clientLocale(wow), 'deDE');
  fs.rmSync(wow, { recursive: true, force: true });

  // --- Page ---
  const renderer = path.join(__dirname, 'renderer');
  const pageUsed = textsIn(path.join(renderer, 'index.html'), new Map());
  for (const file of [...scripts(renderer), ...scripts(path.join(renderer, 'pages'))]) textsIn(file, pageUsed);
  const page = await import('./renderer/i18n.js');
  const pageCount = checkCatalog('renderer/locales/de.js', pageUsed, (await import('./renderer/locales/de.js')).default, 250);

  assert.strictEqual(page.t('{n} days old', { n: 3 }), '3 days old');
  assert.strictEqual(page.locale(), 'en-US');
  assert.strictEqual(page.setLanguage('de'), 'de');
  assert.strictEqual(page.t('{n} days old', { n: 3 }), '3 Tage alt');
  assert.strictEqual(page.locale(), 'de-DE');
  assert.strictEqual(page.setLanguage('xx'), 'en');

  // No German left in the code: umlauts only belong into the catalogs (and into test data).
  const german = /[äöüÄÖÜß]/;
  for (const file of [...scripts(__dirname), ...scripts(renderer), ...scripts(path.join(renderer, 'pages')), path.join(renderer, 'index.html')]) {
    const lines = fs.readFileSync(file, 'utf8').split('\n');
    const hit = lines.findIndex((line) => german.test(line));
    assert.strictEqual(hit, -1, `${path.relative(__dirname, file)}:${hit + 1}: German text outside the catalog: ${lines[hit]}`);
  }

  console.log(`i18n.test.js: ok (${mainCount} + ${pageCount} texts)`);
})().catch((err) => {
  console.error(err.message);
  process.exitCode = 1;
});
