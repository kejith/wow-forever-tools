// German texts of the main process. The key is the English source text; placeholders must match.
// `npm test` (i18n.test.js) checks that every text handed to t() has an entry here and none is unused.
module.exports = {
  // main.js
  'no output': 'keine Ausgabe',
  'Invalid answer from the bridge: {text}': 'Ungültige Antwort der Bridge: {text}',
  'uv was not found. Install it with: winget install astral-sh.uv': 'uv wurde nicht gefunden. Installieren: winget install astral-sh.uv',
  'The bridge failed': 'Bridge fehlgeschlagen',

  // addon.js
  'Unknown addon: {key}': 'Unbekanntes Addon: {key}',
  'Local source without {toc}: {dir}': 'Lokale Quelle ohne {toc}: {dir}',
  'Path not allowed in {toc}: {path}': 'Unerlaubter Pfad in {toc}: {path}',
  'The source lacks files listed in the .toc: {files}': 'In der Quelle fehlen Dateien aus der .toc: {files}',
  '{name} is not installed.': '{name} ist nicht installiert.',
  'Unknown action: {action}': 'Unbekannte Aktion: {action}',
  'Nothing to send.': 'Nichts zu senden.',

  // releases.js
  'not a zip file': 'keine Zip-Datei',
  'zip directory is damaged': 'Zip-Verzeichnis beschädigt',
  '{name}: encrypted': '{name}: verschlüsselt',
  'zip is too large when unpacked': 'Zip entpackt zu groß',
  '{name}: entry is damaged': '{name}: Eintrag beschädigt',
  '{name}: unknown compression {method}': '{name}: unbekannte Kompression {method}',
  '{name}: checksum does not match': '{name}: Prüfsumme stimmt nicht',
  'zip is empty': 'Zip ist leer',
  'Unexpected entry in the zip: {name}': 'Unerwarteter Eintrag im Zip: {name}',
  'GitHub cannot be reached ({reason})': 'GitHub nicht erreichbar ({reason})',
  'no release found': 'kein Release gefunden',
  'GitHub rate limit reached, try again later': 'GitHub-Limit erreicht, später erneut versuchen',
  'GitHub answered with {status}': 'GitHub antwortet mit {status}',
  'No cache folder for addon releases.': 'Kein Cache-Ordner für Addon-Releases.',
  'Unexpected release name: {tag}': 'Unerwarteter Release-Name: {tag}',
  'Release {tag} has no zip': 'Release {tag} hat kein Zip',
  'The zip of release {tag} is too large': 'Zip von Release {tag} ist zu groß',
  'The zip of release {tag} contains no {toc}': 'Zip von Release {tag} enthält keine {toc}',
  '{repo} has no release yet.': '{repo} hat noch kein Release.',
  'Release of {repo} not loaded: {reason}': 'Release von {repo} nicht geladen: {reason}',
};
