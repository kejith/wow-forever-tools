// Checks install/update/request handling against a scratch WoW folder, and the download of addon releases
// against a local stand-in for the GitHub API: node addon.test.js
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const zlib = require('node:zlib');
const addon = require('./addon');
const i18n = require('./i18n');
const releases = require('./releases');

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'wowmcp-test-'));
const wow = path.join(scratch, 'wow');
const target = addon.installedAddonDir(wow);

// ---------- helpers ----------

function writeTree(dir, files) {
  for (const [name, text] of Object.entries(files)) {
    fs.mkdirSync(path.dirname(path.join(dir, name)), { recursive: true });
    fs.writeFileSync(path.join(dir, name), text);
  }
  return dir;
}

const stub = (global) => `-- Example: ${global} = { id = 1, action = "items" }\n${global} = nil\n`;
const wowmcpFiles = (version) => ({
  'WoWMCP.toc': `## Interface: 16001\n## Version: ${version}\n## SavedVariables: WoWMCP_Account\n\nAppRequest.lua\nWoWMCP.lua\n`,
  'AppRequest.lua': stub('WoWMCP_AppRequest'),
  'WoWMCP.lua': `-- WoWMCP ${version}\n`,
});
const gphFiles = (version) => ({
  'GoldPerHour.toc': `## Interface: 16001\n## Version: ${version}\n\nAppRequest.lua\nGoldPerHour.lua\n`,
  'AppRequest.lua': stub('GoldPerHour_AppRequest'),
  'GoldPerHour.lua': `-- GoldPerHour ${version}\n`,
});

// A zip with what real ones contain: a folder entry, stored and deflated files, an archive comment.
function zip(entries, comment = '') {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const e of entries) {
    const name = Buffer.from(e.name);
    const data = Buffer.from(e.data || '');
    const method = e.name.endsWith('/') || e.store ? 0 : 8;
    const packed = method === 8 ? zlib.deflateRawSync(data) : data;
    const crc = e.crc ?? releases.crc32(data);
    const lh = Buffer.alloc(30);
    lh.writeUInt32LE(0x04034b50, 0); lh.writeUInt16LE(20, 4); lh.writeUInt16LE(method, 8); lh.writeUInt32LE(crc, 14);
    lh.writeUInt32LE(packed.length, 18); lh.writeUInt32LE(data.length, 22); lh.writeUInt16LE(name.length, 26);
    locals.push(lh, name, packed);
    const ch = Buffer.alloc(46);
    ch.writeUInt32LE(0x02014b50, 0); ch.writeUInt16LE(20, 4); ch.writeUInt16LE(20, 6); ch.writeUInt16LE(method, 10);
    ch.writeUInt32LE(crc, 16); ch.writeUInt32LE(packed.length, 20); ch.writeUInt32LE(data.length, 24);
    ch.writeUInt16LE(name.length, 28); ch.writeUInt32LE(offset, 42);
    centrals.push(ch, name);
    offset += 30 + name.length + packed.length;
  }
  const cd = Buffer.concat(centrals);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(entries.length, 8); end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(cd.length, 12); end.writeUInt32LE(offset, 16); end.writeUInt16LE(Buffer.byteLength(comment), 20);
  return Buffer.concat([...locals, cd, end, Buffer.from(comment)]);
}

const addonZip = (name, files) => zip([{ name: `${name}/` },
  ...Object.entries(files).map(([f, data], i) => ({ name: `${name}/${f}`, data, store: i === 0 }))], 'a'.repeat(40));

// Stand-in for api.github.com: `published[repo]` = { tag, zip } is the latest release, missing = no release.
const published = {};
const hits = [];
const server = http.createServer((req, res) => {
  hits.push(req.url);
  const latest = req.url.match(/^\/repos\/(kejith\/\w+)\/releases\/latest$/);
  const asset = req.url.match(/^\/repos\/(kejith\/\w+)\/releases\/assets\/(\d+)$/);
  const rel = published[(latest || asset || [])[1]];
  if (!rel) { res.writeHead(404, { 'Content-Type': 'application/json' }); res.end('{"message":"Not Found"}'); return; }
  if (latest) {
    const name = latest[1].split('/')[1];
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ tag_name: rel.tag, html_url: `https://github.com/${latest[1]}/releases/tag/${rel.tag}`,
      published_at: '2026-11-17T20:00:00Z',
      assets: [{ id: 7, name: 'notes.txt', size: 1 }, { id: rel.id, name: `${name}-${rel.tag}.zip`, size: rel.zip.length,
        browser_download_url: 'https://example.invalid/never-used.zip' }] }));
  } else if (Number(asset[2]) === rel.id && req.headers.accept === 'application/octet-stream') {
    res.writeHead(200, { 'Content-Type': 'application/octet-stream' });
    res.end(rel.zip);
  } else {
    res.writeHead(404); res.end();
  }
});

async function main() {
  // ---------- versions and zip reading ----------
  assert.equal(releases.compareVersions('1.10.0', '1.9.3'), 1);
  assert.equal(releases.compareVersions('v0.3.0', '0.3'), 0);
  assert.equal(releases.compareVersions('0.2.0', '0.3.0-beta'), -1);
  const files = releases.readZip(addonZip('WoWMCP', wowmcpFiles('1.5.0')));
  assert.deepEqual(files.map((f) => f.name), ['WoWMCP/WoWMCP.toc', 'WoWMCP/AppRequest.lua', 'WoWMCP/WoWMCP.lua']);
  assert.equal(files[2].data.toString(), '-- WoWMCP 1.5.0\n');
  assert.throws(() => releases.readZip(zip([{ name: 'WoWMCP/x.lua', data: 'x', crc: 1 }])), /checksum does not match/);
  assert.throws(() => releases.readZip(Buffer.from('<html>not a zip</html>')), /not a zip file/);
  // Nothing may land outside the addon's folder.
  for (const name of ['../evil.lua', 'WoWMCP/../../evil.lua', 'Other/x.lua', '/abs.lua', 'WoWMCP/a\\..\\b.lua']) {
    const out = path.join(scratch, 'extract', 'WoWMCP');
    assert.throws(() => releases.extractAddon(zip([{ name: 'WoWMCP/ok.lua', data: 'ok' }, { name, data: 'x' }].reverse()), 'WoWMCP', out),
      /Unexpected entry in the zip/, name);
    assert.ok(!fs.existsSync(path.join(scratch, 'evil.lua')) && !fs.existsSync(path.join(scratch, 'extract', 'evil.lua')));
  }

  // ---------- local checkout as source ----------
  const src = writeTree(path.join(scratch, 'src', 'WoWMCP'), wowmcpFiles('1.5.0'));
  const gphSrc = writeTree(path.join(scratch, 'src', 'GoldPerHour'), gphFiles('0.3.0'));
  const local = { sources: { wowmcp: src, goldperhour: gphSrc } };

  let info = await addon.addonInfo(wow, 'wowmcp', local);
  assert.equal(info.installed, false);
  assert.equal(info.source.kind, 'local');
  assert.equal(info.sourceVersion, '1.5.0');
  assert.throws(() => addon.writeRequest(wow, 'items'), /WoWMCP is not installed/);

  // Fresh install ships every file from the .toc.
  info = await addon.installAddon(wow, 'wowmcp', local);
  assert.equal(info.installed, true);
  assert.equal(info.upToDate, true);
  assert.equal(info.firstInstall, true);
  assert.ok(fs.existsSync(path.join(target, 'AppRequest.lua')));
  // The shipped stub (with an example in a comment) is no pending request.
  assert.equal(addon.readRequest(wow), null);

  // A request is written as Lua the addon can read, ids grow even within one second.
  const r1 = addon.writeRequest(wow, 'items', { full: true });
  const r2 = addon.writeRequest(wow, 'sync');
  assert.ok(r2.id > r1.id);
  const text = fs.readFileSync(path.join(target, 'AppRequest.lua'), 'utf8');
  assert.match(text, /WoWMCP_AppRequest = \{ id = \d+, action = "sync" \}/);
  assert.deepEqual(addon.readRequest(wow), { id: r2.id, action: 'sync' });

  // Updating an install from 1.0 (old .toc, ItemQueue.lua) keeps the pending request and removes the old file.
  fs.writeFileSync(path.join(target, 'WoWMCP.toc'), '## Interface: 16001\n## Version: 1.0.0\n## SavedVariablesPerCharacter: WoWMCP_Char\n\nItemQueue.lua\nWoWMCP.lua\n');
  fs.writeFileSync(path.join(target, 'ItemQueue.lua'), 'WoWMCP_ItemQueue = {}\n');
  info = await addon.addonInfo(wow, 'wowmcp', local);
  assert.equal(info.upToDate, false);
  assert.equal(info.installedVersion, '1.0.0');
  assert.ok(info.changed.includes('WoWMCP.toc'));
  info = await addon.installAddon(wow, 'wowmcp', local);
  assert.equal(info.upToDate, true);
  assert.equal(info.firstInstall, false);
  assert.ok(!fs.existsSync(path.join(target, 'ItemQueue.lua')));
  assert.deepEqual(addon.readRequest(wow), { id: r2.id, action: 'sync' });

  // A change in the checkout without a new version number still counts as an update.
  fs.appendFileSync(path.join(src, 'WoWMCP.lua'), '-- edited\n');
  info = await addon.addonInfo(wow, 'wowmcp', local);
  assert.deepEqual([info.upToDate, info.ahead, info.changed], [false, false, ['WoWMCP.lua']]);
  await addon.installAddon(wow, 'wowmcp', local);

  // Since 1.7 the addon has AppData.lua, the lists the app's bridge writes. An update ships the empty stub,
  // later updates keep what the app wrote, and the file never counts as a difference to the source.
  const withLists = (version) => ({
    ...wowmcpFiles(version),
    'WoWMCP.toc': `## Interface: 16001\n## Version: ${version}\n\nAppRequest.lua\nAppData.lua\nWoWMCP.lua\n`,
    'AppData.lua': stub('WoWMCP_AppData'),
  });
  const lists = { sources: { wowmcp: writeTree(path.join(scratch, 'src', 'lists'), withLists('1.7.0')) } };
  info = await addon.installAddon(wow, 'wowmcp', lists);
  assert.deepEqual([info.installedVersion, info.upToDate], ['1.7.0', true]);
  assert.match(fs.readFileSync(path.join(target, 'AppData.lua'), 'utf8'), /^WoWMCP_AppData = nil$/m);
  const sent = 'WoWMCP_AppData = { version = 1, generated = 1, hash = "abc", chars = {} }\n';
  fs.writeFileSync(path.join(target, 'AppData.lua'), sent);
  info = await addon.addonInfo(wow, 'wowmcp', lists);
  assert.deepEqual([info.upToDate, info.changed], [true, []]);
  writeTree(lists.sources.wowmcp, withLists('1.7.1'));
  info = await addon.installAddon(wow, 'wowmcp', lists);
  assert.deepEqual([info.installedVersion, info.upToDate], ['1.7.1', true]);
  assert.equal(fs.readFileSync(path.join(target, 'AppData.lua'), 'utf8'), sent);
  assert.deepEqual(addon.readRequest(wow), { id: r2.id, action: 'sync' });
  fs.rmSync(path.join(target, 'AppData.lua'));
  await addon.installAddon(wow, 'wowmcp', local);

  // A .toc must not send the installer outside the addon folder.
  const evil = writeTree(path.join(scratch, 'src', 'evil'), { 'WoWMCP.toc': '## Version: 9\n\n..\\..\\evil.lua\n', 'evil.lua': 'x' });
  await assert.rejects(addon.installAddon(wow, 'wowmcp', { sources: { wowmcp: evil } }), /Path not allowed in WoWMCP\.toc/);
  info = await addon.addonInfo(wow, 'wowmcp', { sources: { wowmcp: path.join(scratch, 'nowhere') } });
  assert.match(info.sourceError, /Local source without WoWMCP\.toc/);
  assert.equal(info.upToDate, true); // nothing to compare against, the install is left alone

  // GoldPerHour installs next to WoWMCP into its own folder and has its own requests.
  const gph = addon.installedAddonDir(wow, 'goldperhour');
  assert.equal((await addon.addonInfo(wow, 'goldperhour', local)).installed, false);
  assert.throws(() => addon.writeGphRequest(wow, { action: 'start' }), /GoldPerHour is not installed/);
  info = await addon.installAddon(wow, 'goldperhour', local);
  assert.equal(info.installed, true);
  assert.equal(info.upToDate, true);
  assert.equal(info.name, 'GoldPerHour');
  assert.ok(fs.existsSync(path.join(gph, 'GoldPerHour.lua')));
  assert.equal(addon.readRequest(wow, 'goldperhour'), null);
  assert.deepEqual(addon.readRequest(wow), { id: r2.id, action: 'sync' }); // WoWMCP's request untouched

  const g1 = addon.writeGphRequest(wow, {
    action: 'reset',
    settings: { scale: 9, tooltipItems: '4', priceSource: 'vendor"; os.exit() --', autoStart: true, shown: false,
      herbSpells: [2366, '1234', -1, 'x', 1234], evil: 'print(1)' },
  });
  const gText = fs.readFileSync(path.join(gph, 'AppRequest.lua'), 'utf8');
  assert.match(gText, new RegExp(`GoldPerHour_AppRequest = \\{ id = ${g1.id}, action = "reset", settings = \\{ shown = false, autoStart = true, scale = 2, tooltipItems = 4, herbSpells = \\{ 2366, 1234 \\} \\} \\}`));
  assert.doesNotMatch(gText, /os\.exit|evil|print/);
  assert.deepEqual(addon.readRequest(wow, 'goldperhour'), { id: g1.id, action: 'reset', settings: true });
  assert.throws(() => addon.writeGphRequest(wow, { action: 'explode' }), /Unknown action: explode/);
  assert.throws(() => addon.writeGphRequest(wow, {}), /Nothing to send/);
  const g2 = addon.writeGphRequest(wow, { settings: { priceSource: 'auctionator' } });
  assert.ok(g2.id > g1.id);
  assert.equal(g2.action, null);

  // An update keeps the request, withdrawing leaves an empty one.
  fs.writeFileSync(path.join(gph, 'GoldPerHour.toc'), '## Interface: 16001\n## Version: 0.2.0\n\nGoldPerHour.lua\n');
  assert.equal((await addon.addonInfo(wow, 'goldperhour', local)).installedVersion, '0.2.0');
  info = await addon.installAddon(wow, 'goldperhour', local);
  assert.equal(info.upToDate, true);
  assert.equal(addon.readRequest(wow, 'goldperhour').id, g2.id);
  addon.clearGphRequest(wow);
  assert.equal(addon.readRequest(wow, 'goldperhour'), null);
  assert.match(fs.readFileSync(path.join(gph, 'AppRequest.lua'), 'utf8'), /^GoldPerHour_AppRequest = nil$/m);

  // ---------- GitHub release as source ----------
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const cacheDir = path.join(scratch, 'cache');
  const gh = { cacheDir, api: `http://127.0.0.1:${server.address().port}` };
  const wow2 = path.join(scratch, 'wow2');
  const gph2 = addon.installedAddonDir(wow2, 'goldperhour');

  // No release yet and nothing cached: the reason is reported and nothing can be installed.
  info = await addon.addonInfo(wow2, 'goldperhour', gh);
  assert.match(info.sourceError, /kejith\/GoldPerHour has no release yet/);
  assert.equal(info.source, null);
  await assert.rejects(addon.installAddon(wow2, 'goldperhour', gh), /has no release yet/);

  // First release: downloaded through the asset id, unpacked into the cache, installed from there.
  published['kejith/GoldPerHour'] = { tag: 'v0.3.0', id: 11, zip: addonZip('GoldPerHour', gphFiles('0.3.0')) };
  info = await addon.addonInfo(wow2, 'goldperhour', gh);
  assert.deepEqual([info.source.kind, info.source.tag, info.sourceVersion, info.source.offline], ['release', 'v0.3.0', '0.3.0', false]);
  assert.equal(info.source.url, 'https://github.com/kejith/GoldPerHour/releases/tag/v0.3.0');
  assert.equal(info.sourceDir, path.join(cacheDir, 'GoldPerHour', 'v0.3.0'));
  info = await addon.installAddon(wow2, 'goldperhour', gh);
  assert.deepEqual([info.installed, info.upToDate, info.installedVersion, info.firstInstall], [true, true, '0.3.0', true]);
  assert.equal(fs.readFileSync(path.join(gph2, 'GoldPerHour.lua'), 'utf8'), '-- GoldPerHour 0.3.0\n');

  // Within maxAge the app does not ask GitHub again; `force` does and finds the next release.
  published['kejith/GoldPerHour'] = { tag: 'v0.4.0', id: 12, zip: addonZip('GoldPerHour', gphFiles('0.4.0')) };
  const before = hits.length;
  info = await addon.addonInfo(wow2, 'goldperhour', gh);
  assert.equal(hits.length, before);
  assert.equal(info.upToDate, true);
  info = await addon.addonInfo(wow2, 'goldperhour', { ...gh, force: true });
  assert.deepEqual([info.upToDate, info.sourceVersion, info.installedVersion], [false, '0.4.0', '0.3.0']);
  assert.ok(!fs.existsSync(path.join(cacheDir, 'GoldPerHour', 'v0.3.0'))); // old download removed
  addon.writeGphRequest(wow2, { action: 'start' });
  info = await addon.installAddon(wow2, 'goldperhour', gh);
  assert.deepEqual([info.upToDate, info.installedVersion, info.pendingRequest.action], [true, '0.4.0', 'start']);

  // A broken or hostile download changes nothing: the last good release stays the source.
  published['kejith/GoldPerHour'] = { tag: 'v0.5.0', id: 13, zip: zip([{ name: 'GoldPerHour/GoldPerHour.toc', data: '## Version: 0.5.0\n' }, { name: '../evil.lua', data: 'x' }]) };
  info = await addon.addonInfo(wow2, 'goldperhour', { ...gh, force: true });
  assert.deepEqual([info.sourceVersion, info.source.offline, info.upToDate], ['0.4.0', true, true]);
  assert.match(info.source.error, /Unexpected entry in the zip/);
  assert.ok(!fs.existsSync(path.join(cacheDir, 'evil.lua')) && !fs.existsSync(path.join(cacheDir, 'GoldPerHour', 'v0.5.0')));
  assert.deepEqual(fs.readdirSync(path.join(cacheDir, 'GoldPerHour')).sort(), ['release.json', 'v0.4.0']);

  // Installed from a checkout that is ahead of the release: not offered as an update.
  writeTree(gph2, gphFiles('0.6.0'));
  published['kejith/GoldPerHour'] = { tag: 'v0.4.0', id: 12, zip: addonZip('GoldPerHour', gphFiles('0.4.0')) };
  info = await addon.addonInfo(wow2, 'goldperhour', { ...gh, force: true });
  assert.deepEqual([info.ahead, info.upToDate, info.sourceVersion], [true, true, '0.4.0']);

  // Without network the cached release keeps working and says so.
  await new Promise((resolve) => server.close(resolve));
  info = await addon.addonInfo(wow2, 'goldperhour', { ...gh, force: true });
  assert.deepEqual([info.source.offline, info.sourceVersion], [true, '0.4.0']);
  assert.match(info.source.error, /GitHub cannot be reached/);
  info = await addon.installAddon(wow2, 'goldperhour', { ...gh, force: true });
  assert.deepEqual([info.installedVersion, info.upToDate], ['0.4.0', true]);
  info = await addon.addonInfo(wow2, 'wowmcp', gh);
  assert.match(info.sourceError, /Release of kejith\/WoWMCP not loaded: GitHub cannot be reached/);

  // The same messages in German when the app runs in German.
  i18n.setLanguage('de');
  info = await addon.addonInfo(wow2, 'wowmcp', gh);
  assert.match(info.sourceError, /Release von kejith\/WoWMCP nicht geladen: GitHub nicht erreichbar/);
  assert.throws(() => addon.writeGphRequest(wow, { action: 'explode' }), /Unbekannte Aktion: explode/);
  i18n.setLanguage('en');

  console.log('addon.test.js: ok');
}

main().catch((err) => { console.error(err); process.exitCode = 1; }).finally(() => {
  server.close();
  fs.rmSync(scratch, { recursive: true, force: true });
});
