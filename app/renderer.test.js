// Checks the page's German catalog against its source: node renderer.test.js (from app/, no dependencies).
// Every text handed to t() or mark(), and every data-i18n text of index.html, needs an entry in
// renderer/locales/de.js with the same placeholders and the same HTML tags, and the catalog must not hold
// entries nothing uses any more. The app's own texts (the Go code) have their test in i18n_test.go.
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

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

const scripts = (dir) => fs.readdirSync(dir).filter((f) => f.endsWith('.js')).map((f) => path.join(dir, f));
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
  const renderer = path.join(__dirname, 'renderer');
  const files = [...scripts(renderer), ...scripts(path.join(renderer, 'pages'))];
  const used = textsIn(path.join(renderer, 'index.html'), new Map());
  for (const file of files) textsIn(file, used);
  const page = await import('./renderer/i18n.js');
  const count = checkCatalog('renderer/locales/de.js', used, (await import('./renderer/locales/de.js')).default, 250);

  assert.strictEqual(page.t('{n} days old', { n: 3 }), '3 days old');
  assert.strictEqual(page.locale(), 'en-US');
  assert.strictEqual(page.setLanguage('de'), 'de');
  assert.strictEqual(page.t('{n} days old', { n: 3 }), '3 Tage alt');
  assert.strictEqual(page.locale(), 'de-DE');
  assert.strictEqual(page.setLanguage('xx'), 'en');

  // No German left in the code: umlauts only belong into the catalog (and into test data).
  const german = /[äöüÄÖÜß]/;
  for (const file of [...files, path.join(renderer, 'index.html')]) {
    const lines = fs.readFileSync(file, 'utf8').split('\n');
    const hit = lines.findIndex((line) => german.test(line));
    assert.strictEqual(hit, -1, `${path.relative(__dirname, file)}:${hit + 1}: German text outside the catalog: ${lines[hit]}`);
  }

  // window.wow: every method the pages call exists in wails.js, and every method wails.js calls is an exported
  // method of App (Wails binds exactly those).
  const shim = fs.readFileSync(path.join(renderer, 'wails.js'), 'utf8');
  const offered = new Set([...shim.matchAll(/^ {4}(\w+):/gm)].map((m) => m[1]));
  const called = new Set(files.flatMap((f) => [...fs.readFileSync(f, 'utf8').matchAll(/\bapi\.(\w+)\(/g)].map((m) => m[1])));
  for (const name of called) assert.ok(offered.has(name), `window.wow.${name} is called but wails.js has no such method`);
  const go = fs.readdirSync(__dirname).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
    .map((f) => fs.readFileSync(path.join(__dirname, f), 'utf8')).join('\n');
  for (const [, method] of shim.matchAll(/call\('(\w+)'/g)) {
    assert.ok(new RegExp(`^func \\(a \\*App\\) ${method}\\(`, 'm').test(go), `wails.js calls App.${method}, which the Go code does not have`);
  }

  console.log(`renderer.test.js: ok (${count} texts, ${called.size} API methods)`);
})().catch((err) => {
  console.error(err.message);
  process.exitCode = 1;
});
