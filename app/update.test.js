// Checks the app's own update against a local stand-in for the GitHub API and a made-up installer; nothing
// is started (the installer goes to a recorder): node update.test.js
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const { EventEmitter } = require('node:events');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const i18n = require('./i18n');
const update = require('./update');

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'wow-update-test-'));
const sha256 = (data) => crypto.createHash('sha256').update(data).digest('hex');
const installer = (version) => Buffer.concat([Buffer.from(`MZ installer ${version} `), Buffer.alloc(300 * 1024, version.length)]);

// Stand-in for api.github.com: `published` is the latest release of the app, null = no release.
//   { tag, exe, name?, sums?, digest?, send? }: `sums` replaces the text of SHA256SUMS.txt (null = no such
//   asset), `send` what the download does: 'other' (same size, other bytes), 'cut' (connection drops half
//   way), 'stall' (half of it, then silence), 'long' (more bytes than listed).
let published = null;
const hits = [];
const server = http.createServer((req, res) => {
  hits.push(req.url);
  const rel = published;
  const asset = req.url.match(/^\/repos\/kejith\/wow-forever-tools\/releases\/assets\/(\d+)$/);
  if (!rel || (!asset && req.url !== '/repos/kejith/wow-forever-tools/releases/latest')) {
    res.writeHead(404, { 'Content-Type': 'application/json' });
    res.end('{"message":"Not Found"}');
    return;
  }
  const name = rel.name || `WoW-Forever-Tools-Setup-${rel.tag.slice(1)}.exe`;
  const sums = rel.sums === undefined ? `${sha256(rel.exe)}  ${name}\n` : rel.sums;
  if (!asset) {
    const assets = [{ id: 21, name, size: rel.exe.length, digest: rel.digest, browser_download_url: 'https://example.invalid/never-used.exe' }];
    if (sums !== null) assets.push({ id: 22, name: 'SHA256SUMS.txt', size: Buffer.byteLength(sums) });
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ tag_name: rel.tag, html_url: 'https://example.invalid/never-used', published_at: '2026-11-23T10:00:00Z', assets }));
  } else if (req.headers.accept !== 'application/octet-stream') {
    res.writeHead(404); res.end();
  } else if (asset[1] === '22' && sums !== null) {
    res.writeHead(200, { 'Content-Type': 'application/octet-stream' });
    res.end(sums);
  } else if (asset[1] === '21') {
    const half = rel.exe.subarray(0, rel.exe.length >> 1);
    if (rel.send === 'cut') {
      res.writeHead(200, { 'Content-Length': rel.exe.length });
      res.write(half, () => res.destroy());
    } else if (rel.send === 'stall') {
      res.writeHead(200, { 'Content-Length': rel.exe.length });
      res.write(half);
    } else {
      res.writeHead(200, { 'Content-Type': 'application/octet-stream' });
      res.end(rel.send === 'other' ? Buffer.alloc(rel.exe.length, 7) : rel.send === 'long' ? Buffer.concat([rel.exe, rel.exe]) : rel.exe);
    }
  } else {
    res.writeHead(404); res.end();
  }
});

const release = (version, extra = {}) => ({ tag: `v${version}`, exe: installer(version), ...extra });
// What an update folder holds besides the remembered answer of the last check.
const leftovers = (dir) => (fs.existsSync(dir) ? fs.readdirSync(dir) : []).filter((name) => name !== 'update.json');
const assetHits = (from) => hits.slice(from).filter((url) => url.endsWith('/assets/21')).length;

async function main() {
  // ---------- small parts ----------
  const hex = 'ab'.repeat(32);
  assert.equal(update.parseSums(`${'cd'.repeat(32)}  other.exe\n${hex.toUpperCase()} *WoW-Forever-Tools-Setup-1.13.0.exe\r\n`, 'WoW-Forever-Tools-Setup-1.13.0.exe'), hex);
  assert.equal(update.parseSums(`${hex}  WoW-Forever-Tools-Setup-1.13.0.exe.blockmap\n`, 'WoW-Forever-Tools-Setup-1.13.0.exe'), null);
  assert.equal(update.parseSums('<html>', 'x.exe'), null);
  assert.deepEqual(update.installerArgs(true), ['--updated', '/S', '--force-run']);
  assert.deepEqual(update.installerArgs(false), ['--updated', '/S']);
  assert.equal(update.releaseUrl('1.13.0'), 'https://github.com/kejith/wow-forever-tools/releases/tag/v1.13.0');

  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const api = `http://127.0.0.1:${server.address().port}`;
  let auto = false;
  const started = [];
  // One updater per "start of the app"; `states` are the phases it reported.
  const start = (dir, current, extra = {}) => {
    const states = [];
    const updater = update.createUpdater({
      dir: path.join(scratch, dir), current, api, automatic: () => auto, onState: (s) => states.push(s),
      launchFn: async (file, opts) => { started.push({ file, ...opts }); }, ...extra,
    });
    return { updater, states, dir: path.join(scratch, dir), phases: () => states.map((s) => s.phase).filter((p, i, all) => p !== all[i - 1]) };
  };

  // ---------- nothing to do ----------
  let app = start('a', '1.12.0');
  assert.deepEqual([app.updater.state().phase, app.updater.state().canInstall, app.updater.state().automatic], ['idle', true, false]);
  let s = await app.updater.check();
  assert.deepEqual([s.phase, s.latest, s.checkedAt], ['idle', null, null]);
  assert.match(s.error, /kejith\/wow-forever-tools has no release yet/);

  // The running version is the newest: no checksum is fetched, and within the hour GitHub is not asked again.
  published = release('1.12.0');
  let before = hits.length;
  s = await app.updater.check();
  assert.deepEqual([s.phase, s.latest.version, s.error, hits.length - before], ['current', '1.12.0', null, 1]);
  assert.ok(Date.now() - s.checkedAt < 5000);
  published = release('1.13.0');
  s = await app.updater.check();
  assert.deepEqual([s.phase, hits.length - before], ['current', 1]);

  // ---------- an update, step by step (automatic off) ----------
  before = hits.length;
  s = await app.updater.check(true);
  assert.deepEqual([s.phase, s.latest.version, s.latest.size, assetHits(before)], ['available', '1.13.0', published.exe.length, 0]);
  assert.equal(s.latest.url, 'https://github.com/kejith/wow-forever-tools/releases/tag/v1.13.0'); // built, not taken from the answer
  assert.equal(s.latest.publishedAt, '2026-11-23T10:00:00Z');
  assert.equal(app.updater.installsOnQuit(), false);
  assert.equal((await app.updater.install()).started, false); // nothing downloaded yet

  s = await app.updater.download();
  const exe = path.join(app.dir, 'WoW-Forever-Tools-Setup-1.13.0.exe');
  assert.deepEqual([s.phase, s.progress, s.error], ['ready', null, null]);
  assert.ok(fs.readFileSync(exe).equals(published.exe));
  assert.deepEqual(fs.readdirSync(app.dir).sort(), ['WoW-Forever-Tools-Setup-1.13.0.exe', 'update.json']);
  assert.deepEqual(app.phases(), ['checking', 'idle', 'checking', 'current', 'checking', 'current', 'checking', 'available', 'downloading', 'ready']);
  const loading = app.states.filter((x) => x.phase === 'downloading');
  assert.deepEqual(loading[0].progress, { received: 0, total: published.exe.length });
  assert.ok(loading.length >= 2 && loading.at(-1).progress.received > 0);
  assert.equal(app.updater.installsOnQuit(), false); // the user turned the automatic off

  // "Restart and update": the installer is started for an update, silently, and starts the app again.
  s = await app.updater.install();
  assert.deepEqual([s.started, s.phase], [true, 'installing']);
  assert.deepEqual(started, [{ file: exe, relaunch: true }]);
  assert.equal((await app.updater.check(true)).phase, 'installing'); // nothing else runs while it installs

  // ---------- the next start finds the download; with the automatic it goes in when the app closes ----------
  auto = true;
  before = hits.length;
  app = start('a', '1.12.0');
  s = await app.updater.check();
  assert.deepEqual([s.phase, s.automatic, hits.length - before], ['ready', true, 0]);
  assert.equal(app.updater.installsOnQuit(), true);
  s = await app.updater.install({ relaunch: false });
  assert.deepEqual([s.started, started.at(-1)], [true, { file: exe, relaunch: false }]);

  // A file that changed while it waited is not started.
  app = start('a', '1.12.0', { maxAge: Infinity });
  await app.updater.check();
  fs.writeFileSync(exe, Buffer.alloc(published.exe.length, 1));
  s = await app.updater.install();
  assert.deepEqual([s.started, s.phase, started.length], [false, 'available', 2]);
  assert.match(s.error, /does not match its checksum/);
  assert.ok(!fs.existsSync(exe));

  // ---------- automatic: found, downloaded, ready in one go ----------
  app = start('b', '1.12.0');
  s = await app.updater.check();
  assert.deepEqual([s.phase, s.error], ['ready', null]);
  assert.deepEqual(app.phases(), ['checking', 'available', 'downloading', 'ready']);

  // A newer release replaces the download that waited.
  published = release('1.14.0');
  s = await app.updater.check(true);
  assert.deepEqual([s.phase, s.latest.version], ['ready', '1.14.0']);
  assert.deepEqual(fs.readdirSync(app.dir).sort(), ['WoW-Forever-Tools-Setup-1.14.0.exe', 'update.json']);

  // After the update the new version tidies up and says so.
  fs.writeFileSync(path.join(app.dir, 'WoW-Forever-Tools-Setup-1.15.0.exe.part'), 'half');
  app = start('b', '1.14.0', { updated: true });
  assert.deepEqual(fs.readdirSync(app.dir), ['update.json']);
  s = await app.updater.check();
  assert.deepEqual([s.phase, s.updated, s.latest.version], ['current', true, '1.14.0']);

  // A file that cannot be removed yet (the installer that still runs right after the update; here a folder
  // of that name) does not keep the rest from going, and the next check tries again.
  const locked = path.join(app.dir, 'WoW-Forever-Tools-Setup-1.13.0.exe');
  fs.mkdirSync(path.join(locked, 'in-use'), { recursive: true });
  fs.writeFileSync(path.join(app.dir, 'WoW-Forever-Tools-Setup-1.14.0.exe'), 'done');
  app = start('b', '1.14.0');
  assert.deepEqual(fs.readdirSync(app.dir).sort(), ['WoW-Forever-Tools-Setup-1.13.0.exe', 'update.json']);
  fs.rmSync(locked, { recursive: true });
  fs.writeFileSync(locked, 'released');
  await app.updater.check();
  assert.deepEqual(fs.readdirSync(app.dir), ['update.json']);

  // Turning the automatic on fetches an update that was only shown.
  auto = false;
  published = release('1.15.0');
  s = await app.updater.check(true);
  assert.equal(s.phase, 'available');
  assert.equal((await app.updater.settingsChanged()).phase, 'available');
  auto = true;
  assert.equal((await app.updater.settingsChanged()).phase, 'ready');

  // ---------- started from source, or no Windows: it tells, it does not install ----------
  app = start('c', '1.12.0', { reason: 'source' });
  before = hits.length;
  s = await app.updater.check();
  assert.deepEqual([s.phase, s.canInstall, s.reason, s.latest.version, assetHits(before)], ['available', false, 'source', '1.15.0', 0]);
  assert.equal((await app.updater.download()).phase, 'available');
  assert.equal((await app.updater.install()).started, false);
  assert.equal(app.updater.installsOnQuit(), false);
  // A checkout ahead of the release is current.
  assert.equal((await start('c', '1.16.0', { reason: 'source' }).updater.check()).phase, 'current');

  // ---------- releases the app does not take ----------
  const refused = async (dir, rel, pattern, phase = 'idle') => {
    published = rel;
    const run = start(dir, '1.12.0');
    const state = await run.updater.check();
    assert.match(state.error || '', pattern, dir);
    assert.equal(state.phase, phase, dir);
    assert.deepEqual(leftovers(run.dir), [], dir);
    return run;
  };
  await refused('r1', release('1.13.0', { tag: 'nightly' }), /Unexpected release name: nightly/);
  await refused('r2', release('1.13.0', { name: 'evil.exe' }), /Release v1\.13\.0 has no installer/);
  await refused('r3', release('1.13.0', { sums: null }), /Release v1\.13\.0 has no checksum for WoW-Forever-Tools-Setup-1\.13\.0\.exe/);
  await refused('r4', release('1.13.0', { sums: `${hex}  another-file.exe\n` }), /has no checksum for/);
  await refused('r5', release('1.13.0', { digest: `sha256:${hex}` }), /The checksums of release v1\.13\.0 do not agree/);
  // GitHub's digest and the release's own list agree: fine.
  published = release('1.13.0');
  published.digest = `sha256:${sha256(published.exe)}`;
  assert.equal((await start('r6', '1.12.0').updater.check()).phase, 'ready');

  // ---------- downloads that go wrong leave nothing behind and can be tried again ----------
  app = await refused('d1', release('1.13.0', { send: 'other' }), /does not match its checksum/, 'available');
  app = await refused('d2', release('1.13.0', { send: 'long' }), /does not match its checksum/, 'available');
  app = await refused('d3', release('1.13.0', { send: 'cut' }), /The download of the update stopped/, 'available');
  published = release('1.13.0', { send: 'stall' });
  app = start('d4', '1.12.0', { idleMs: 300 });
  s = await app.updater.check();
  assert.match(s.error, /The download of the update stopped \(no data for 0 s\)/);
  assert.deepEqual(fs.readdirSync(app.dir), ['update.json']);
  published = release('1.13.0');
  s = await app.updater.download();
  assert.deepEqual([s.phase, s.error], ['ready', null]);

  // ---------- the real launcher, with a recorder in place of the process ----------
  const spawned = [];
  const fakeSpawn = (fail) => (file, args, options) => {
    const child = new EventEmitter();
    child.unref = () => { child.unrefed = true; };
    spawned.push({ file, args, options, child });
    setImmediate(() => (fail ? child.emit('error', Object.assign(new Error('spawn EACCES'), { code: 'EACCES' })) : child.emit('spawn')));
    return child;
  };
  await update.launch('C:\\x\\setup.exe', { relaunch: true, spawnFn: fakeSpawn(false) });
  assert.deepEqual([spawned[0].file, spawned[0].args, spawned[0].options, spawned[0].child.unrefed],
    ['C:\\x\\setup.exe', ['--updated', '/S', '--force-run'], { detached: true, stdio: 'ignore' }, true]);
  await assert.rejects(update.launch('C:\\x\\setup.exe', { spawnFn: fakeSpawn(true) }), /The installer could not be started \(EACCES\)/);
  // An installer that does not start leaves the update ready for another try.
  app = start('d4', '1.12.0', { launchFn: (file, opts) => update.launch(file, { ...opts, spawnFn: fakeSpawn(true) }) });
  await app.updater.check();
  s = await app.updater.install();
  assert.deepEqual([s.started, s.phase], [false, 'ready']);
  assert.match(s.error, /could not be started/);

  // ---------- without network the last answer counts, and the reason is told ----------
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
  s = await app.updater.check(true);
  assert.deepEqual([s.phase, s.latest.version], ['ready', '1.13.0']);
  assert.match(s.error, /GitHub cannot be reached/);
  i18n.setLanguage('de');
  s = await start('e', '1.12.0').updater.check();
  assert.deepEqual([s.phase, s.latest], ['idle', null]);
  assert.match(s.error, /GitHub nicht erreichbar/);
  i18n.setLanguage('en');

  console.log('update.test.js: ok');
}

main().catch((err) => { console.error(err); process.exitCode = 1; }).finally(() => {
  server.closeAllConnections();
  server.close();
  fs.rmSync(scratch, { recursive: true, force: true });
});
