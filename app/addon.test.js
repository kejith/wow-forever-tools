// Checks install/update/request handling against a scratch WoW folder: node addon.test.js
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const addon = require('./addon');

const wow = fs.mkdtempSync(path.join(os.tmpdir(), 'wowmcp-test-'));
const target = addon.installedAddonDir(wow);

try {
  let info = addon.addonInfo(wow);
  assert.equal(info.installed, false);
  assert.throws(() => addon.writeRequest(wow, 'items'), /nicht installiert/);

  // Fresh install ships every file from the .toc.
  info = addon.installAddon(wow);
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
  info = addon.addonInfo(wow);
  assert.equal(info.upToDate, false);
  assert.equal(info.installedVersion, '1.0.0');
  assert.ok(info.changed.includes('WoWMCP.toc'));
  info = addon.installAddon(wow);
  assert.equal(info.upToDate, true);
  assert.equal(info.firstInstall, false);
  assert.ok(!fs.existsSync(path.join(target, 'ItemQueue.lua')));
  assert.deepEqual(addon.readRequest(wow), { id: r2.id, action: 'sync' });
  console.log('addon.test.js: ok');
} finally {
  fs.rmSync(wow, { recursive: true, force: true });
}
