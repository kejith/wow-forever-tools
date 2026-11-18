// Latest GitHub release of an addon, downloaded once and kept in a cache folder. No Electron and no npm
// dependency: Node's fetch and zlib are enough for the small zips the addon repositories publish.
const fs = require('node:fs');
const path = require('node:path');
const zlib = require('node:zlib');
const { t } = require('./i18n');

const API = 'https://api.github.com';
const MAX_AGE_MS = 15 * 60 * 1000; // GitHub allows 60 anonymous API calls per hour and address
const MAX_ZIP_BYTES = 20 * 1024 * 1024;
const MAX_UNPACKED_BYTES = 50 * 1024 * 1024;
const SAFE_NAME = /^[\w][\w.-]*$/;

// ---------- zip ----------

const CRC_TABLE = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function crc32(buf) {
  let c = 0xffffffff;
  for (let i = 0; i < buf.length; i++) c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

// Files of a zip as [{ name, data }]. Handles what the release packager and ordinary zip tools write (stored or
// deflated, no zip64, no encryption) and checks size and CRC of every file.
function readZip(buf) {
  let eocd = -1;
  for (let i = buf.length - 22; i >= Math.max(0, buf.length - 22 - 0xffff); i--) {
    if (buf.readUInt32LE(i) === 0x06054b50) { eocd = i; break; }
  }
  if (eocd < 0) throw new Error(t('not a zip file'));
  const count = buf.readUInt16LE(eocd + 10);
  let pos = buf.readUInt32LE(eocd + 16);
  const files = [];
  let total = 0;
  for (let i = 0; i < count; i++) {
    if (pos + 46 > buf.length || buf.readUInt32LE(pos) !== 0x02014b50) throw new Error(t('zip directory is damaged'));
    const flags = buf.readUInt16LE(pos + 8);
    const method = buf.readUInt16LE(pos + 10);
    const crc = buf.readUInt32LE(pos + 16);
    const packed = buf.readUInt32LE(pos + 20);
    const size = buf.readUInt32LE(pos + 24);
    const nameLen = buf.readUInt16LE(pos + 28);
    const extraLen = buf.readUInt16LE(pos + 30);
    const commentLen = buf.readUInt16LE(pos + 32);
    const local = buf.readUInt32LE(pos + 42);
    const name = buf.toString('utf8', pos + 46, pos + 46 + nameLen);
    pos += 46 + nameLen + extraLen + commentLen;
    if (name.endsWith('/')) continue; // folder entry
    if (flags & 1) throw new Error(t('{name}: encrypted', { name }));
    total += size;
    if (total > MAX_UNPACKED_BYTES) throw new Error(t('zip is too large when unpacked'));
    if (local + 30 > buf.length || buf.readUInt32LE(local) !== 0x04034b50) throw new Error(t('{name}: entry is damaged', { name }));
    const start = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const raw = buf.subarray(start, start + packed);
    let data;
    if (method === 0) data = Buffer.from(raw);
    else if (method === 8) data = zlib.inflateRawSync(raw, { maxOutputLength: Math.max(size, 1) });
    else throw new Error(t('{name}: unknown compression {method}', { name, method }));
    if (data.length !== size || crc32(data) !== crc) throw new Error(t('{name}: checksum does not match', { name }));
    files.push({ name, data });
  }
  return files;
}

// Unpacks the folder `<name>/` of the zip into `target`. Anything outside that folder is an error, so a
// zip can never write next to the cache folder.
function extractAddon(buf, name, target) {
  const files = readZip(buf);
  const prefix = `${name}/`;
  if (!files.length) throw new Error(t('zip is empty'));
  for (const f of files) {
    const rel = f.name.startsWith(prefix) ? f.name.slice(prefix.length) : null;
    const parts = rel ? rel.split('/') : [];
    if (!rel || f.name.includes('\\') || parts.some((p) => !p || p === '.' || p === '..' || p.includes(':'))) {
      throw new Error(t('Unexpected entry in the zip: {name}', { name: f.name }));
    }
    const file = path.join(target, ...parts);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, f.data);
  }
}

// ---------- versions ----------

function tocVersion(dir, name) {
  try {
    const toc = fs.readFileSync(path.join(dir, `${name}.toc`), 'utf8');
    return (toc.match(/^## Version:\s*(.+)$/m) || [])[1]?.trim() || null;
  } catch {
    return null;
  }
}

// -1, 0 or 1 for "1.4.0" against "1.10"; a leading v and anything after the numbers is ignored.
function compareVersions(a, b) {
  const nums = (v) => (String(v || '').match(/\d+(\.\d+)*/) || [''])[0].split('.').map((n) => Number(n) || 0);
  const x = nums(a);
  const y = nums(b);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] || 0) - (y[i] || 0);
    if (d) return d < 0 ? -1 : 1;
  }
  return 0;
}

// ---------- GitHub ----------

async function request(fetchFn, url, accept) {
  let res;
  try {
    res = await fetchFn(url, {
      headers: { Accept: accept, 'User-Agent': 'wow-forever-tools', 'X-GitHub-Api-Version': '2022-11-28' },
      signal: AbortSignal.timeout(15000),
    });
  } catch (err) {
    throw new Error(t('GitHub cannot be reached ({reason})', { reason: err.cause?.code || err.name || err.message }));
  }
  if (res.status === 404) throw Object.assign(new Error(t('no release found')), { notFound: true });
  if (res.status === 403 || res.status === 429) throw new Error(t('GitHub rate limit reached, try again later'));
  if (!res.ok) throw new Error(t('GitHub answered with {status}', { status: res.status }));
  return res;
}

function readState(file) {
  try { return JSON.parse(fs.readFileSync(file, 'utf8')); } catch { return null; }
}

// The cached release as a source, or null when the cache is empty or incomplete.
function cachedSource(root, name, state, extra = {}) {
  if (!state || !SAFE_NAME.test(String(state.tag || ''))) return null;
  const dir = path.join(root, state.tag);
  if (!fs.existsSync(path.join(dir, `${name}.toc`))) return null;
  return { kind: 'release', dir, repo: state.repo, tag: state.tag, url: state.url, publishedAt: state.publishedAt,
    checkedAt: state.checkedAt, version: tocVersion(dir, name), offline: false, ...extra };
}

// Newest release of `repo` unpacked under `<cacheDir>/<name>/<tag>`. Asks GitHub at most every `maxAge` ms
// (`force` asks now). Without network the last download is returned with `offline` and the reason.
async function latestRelease({ repo, name, cacheDir, api = API, fetchFn = fetch, maxAge = MAX_AGE_MS, force = false }) {
  if (!cacheDir) throw new Error(t('No cache folder for addon releases.'));
  const root = path.join(cacheDir, name);
  const stateFile = path.join(root, 'release.json');
  const state = readState(stateFile);
  const cached = state?.repo === repo ? cachedSource(root, name, state) : null;
  if (cached && !force && Date.now() - (state.checkedAt || 0) < maxAge) return cached;

  try {
    const release = await (await request(fetchFn, `${api}/repos/${repo}/releases/latest`, 'application/vnd.github+json')).json();
    const tag = String(release.tag_name || '');
    if (!SAFE_NAME.test(tag)) throw new Error(t('Unexpected release name: {tag}', { tag: tag.slice(0, 40) }));
    const zips = (release.assets || []).filter((a) => typeof a.name === 'string' && a.name.toLowerCase().endsWith('.zip'));
    const asset = zips.find((a) => a.name === `${name}-${tag}.zip`) || zips[0];
    if (!asset || !Number.isInteger(asset.id)) throw new Error(t('Release {tag} has no zip', { tag }));
    if (asset.size > MAX_ZIP_BYTES) throw new Error(t('The zip of release {tag} is too large', { tag }));

    const dir = path.join(root, tag);
    if (!fs.existsSync(path.join(dir, `${name}.toc`))) {
      // The asset is addressed by its id on the API host, never by a URL taken from the response.
      const res = await request(fetchFn, `${api}/repos/${repo}/releases/assets/${asset.id}`, 'application/octet-stream');
      const zip = Buffer.from(await res.arrayBuffer());
      if (zip.length > MAX_ZIP_BYTES) throw new Error(t('The zip of release {tag} is too large', { tag }));
      const tmp = path.join(root, `.${tag}.${process.pid}.tmp`);
      fs.rmSync(tmp, { recursive: true, force: true });
      try {
        extractAddon(zip, name, tmp);
        if (!fs.existsSync(path.join(tmp, `${name}.toc`))) throw new Error(t('The zip of release {tag} contains no {toc}', { tag, toc: `${name}.toc` }));
        fs.rmSync(dir, { recursive: true, force: true });
        fs.renameSync(tmp, dir);
      } finally {
        fs.rmSync(tmp, { recursive: true, force: true });
      }
    }
    const next = { repo, tag, url: typeof release.html_url === 'string' ? release.html_url : null,
      publishedAt: release.published_at || null, checkedAt: Date.now() };
    fs.writeFileSync(stateFile, JSON.stringify(next, null, 2));
    // Older downloads are no longer needed.
    for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
      if (entry.isDirectory() && entry.name !== tag) fs.rmSync(path.join(root, entry.name), { recursive: true, force: true });
    }
    return cachedSource(root, name, next);
  } catch (err) {
    const reason = err.notFound ? t('{repo} has no release yet.', { repo }) : err.message;
    if (cached) return { ...cached, offline: true, error: reason };
    throw new Error(err.notFound ? reason : t('Release of {repo} not loaded: {reason}', { repo, reason }));
  }
}

module.exports = { latestRelease, readZip, extractAddon, crc32, tocVersion, compareVersions };
