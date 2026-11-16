# WoW-Umgebung (WoW: Forever)

Arbeitsordner für alle Fragen rund um World of Warcraft: Forever – Charaktere, Auktionshaus, Berufe/Verfügungen, Addons, Grafik-Einstellungen. Antworten auf Deutsch.

## Installation & Pfade

| Was | Pfad |
| --- | --- |
| WoW-Basis (Battle.net) | `C:\Program Files (x86)\World of Warcraft` |
| **WoW Forever** (Client 1.60.1, Flavor `wowf`) | `C:\Program Files (x86)\World of Warcraft\_forever_` |
| Addons | `…\_forever_\Interface\AddOns` |
| Account-SavedVariables (z. B. `Auctionator.lua`) | `…\_forever_\WTF\Account\<Account>\SavedVariables` |
| Charakter-SavedVariables (`WoWMCP.lua`) | `…\_forever_\WTF\Account\<Account>\93\<Name-Name>\SavedVariables` |
| Logs / Screenshots / Fehler | `…\_forever_\Logs`, `\Screenshots`, `\Errors` |
| Combat-Logs (alt) | `%USERPROFILE%\OneDrive\Dokumente\Spiele\World of Warcraft` |

Andere Clients im selben Ordner: `_retail_`, `_classic_` (MoP Classic), `_classic_era_` – für Forever-Fragen immer `_forever_` verwenden.

Charaktere (Ruleset/Realm-Ordner `93`): Malgruk Höllenbrand (Orc-Hexenmeister), Siegfried Schniefmaul (Untoter Priester), Günther Gammelmaul (noch kein WoWMCP-Export).

Installierte Addons u. a.: Auctionator, AtlasLoot (Classic + Continued), Details, EllesmereUI, RXPGuides, TomTom, WhatsTraining, WIM, NodeSight, ForeverDungeonJournal, BetterFishing, WoWMCP.

## MCP-Server

| Server | Wofür | Wo konfiguriert |
| --- | --- | --- |
| `wow-forever` | Eigene Charakterdaten: Werte, Ausrüstung, Taschen/Bank, Quests, Ruf, Berufe, Talente. Liest die `WoWMCP.lua`-Snapshots. | `.mcp.json` in diesem Ordner (aktiviert über `.claude/settings.local.json`) **und** Claude Desktop (`%APPDATA%\Claude\claude_desktop_config.json`) |
| `everloot` | Spielwissen Forever: Items, NPCs, Quests, Händler, Karten, AH-Preise/Marktwerte, Talente, Berufswege, Gilden. Quelle everloot.app (Spieler-Uploads) – Anzahl/Datum der Beobachtungen mit angeben. | User-Scope `~/.claude.json` (HTTP `https://everloot.app/mcp/public`) |
| `wow` (`hated-wow-mcp`) | Addon-Entwicklung: Lua-API, Blizzard-UI-Quellcode, CVars, Icons/Atlas, Lua-Lint, TOC/XML-Validierung. `WOW_DEFAULT_FLAVOR=forever`. | User-Scope `~/.claude.json` (`npx -y hated-wow-mcp`) |

Faustregeln:
- Fragen zu *meinem* Charakter → `wow-forever` (immer `snapshot_age` beachten; ggf. im Spiel `/wowmcp sync` bzw. `/reload`).
- Fragen zum *Spiel* (Wo droppt X, was kostet Y, welcher Händler) → `everloot`, Einstieg mit `answer_sources`.
- Addon-Code / CVars → `wow`.

## Skripte

| Datei | Zweck | Aufruf |
| --- | --- | --- |
| `scripts/verfuegungen.py` | Beste Handwerksverfügung pro Stufe (Geselle 75 / Experte 125 / Fachmann 200 Ruf) nach Silber pro Ruf für „Versorgung und Logistik Durotar“. Liest Auctionator-Scan + Inventar aus WoWMCP. Items im Besitz zählen als 0. | `python scripts\verfuegungen.py --char Malgruk --top 3` (Optionen: `--besitz-zum-ah-preis`, `--wow <pfad>`) |
| `scripts/upgrades.py` | Ausrüstungs-Upgrades aus dem AH für den Gebrechen-Hexer gegen die angelegte Ausrüstung, inkl. Forever-Items (ID > 200000): Vorschläge pro Platz und das beste Paket fürs Budget. Bewertung siehe „Upgrade-Bewertung“ unten. Liest den Item-Export des Addons (`WoWMCP_Items` in der Account-SavedVariables `WoWMCP.lua`). | Im AH scannen und AH schließen (das Addon exportiert neue Items dann selbst) oder `/wowmcp items`, danach `/reload`. Dann `uv run scripts\upgrades.py --char Malgruk` (Optionen: `--max-preis 1` = Budget in Gold, `--profil leveln\|dungeon`, `--sort gewinn\|preis`, `--vorschau 0`, `--top 5`, `--json`) |
| `scripts/test_upgrades.py` | Tests der Bewertung ohne Spiel (Stat-Schlüssel, Treffer-Cap, Paket, Stab gegen Waffenhand + Schildhand) | `uv run scripts\test_upgrades.py` |
| `scripts/app_bridge.py` | JSON-Schnittstelle der Desktop-App zu den Skripten (`status`, `character`, `writs`, `upgrades`) | `uv run scripts\app_bridge.py status` |
| `app/` | Electron-App: Verbindung zum Addon (installieren/aktualisieren, Aufträge, Live-Abgleich der SavedVariables), Charakter, Verfügungen, AH-Upgrades. Rechnet über `app_bridge.py`. | `cd app`, `npm install`, `npm start`. Screenshots aller Seiten: `npm run capture` (→ `app/captures`), Tests: `npm test` |
| `server/wow_mcp.py` | MCP-Server `wow-forever` (uv-Skript, Abhängigkeiten im Header) | wird von Claude gestartet: `uv run server\wow_mcp.py` |
| `addon/WoWMCP/` | Quelle des Export-Addons (1.2.0). Installiert liegt es unter `…\_forever_\Interface\AddOns\WoWMCP`; die App installiert und aktualisiert es. | In-game `/wowmcp sync`, `/wowmcp items [full]`, `/wowmcp auto on/off`, `/wowmcp status` |
| `notes/wow_forever_settings.txt` | Versteckte Grafik-CVars (Schärfe, Gras, Sichtweite, LOD) inkl. Reset-Befehle | im Chat `/console …` |

Daten-Aktualität: WoW schreibt SavedVariables nur bei `/reload` oder Logout. Für `verfuegungen.py` vorher einen vollen AH-Scan mit Auctionator machen und dann `/reload`. AH-Preise sind Mindest-Sofortkauf beim letzten Scan, keine Live-Angebote.

## Desktop-App (`app/`)

Electron-App (Electron 45, ohne Build-Schritt, Vanilla-JS), die Addon, Skripte und SavedVariables zusammenführt. Sie läuft lokal und hat keinen Server und keinen Netzwerkzugriff.

**Starten:** `cd C:\Coding\wow\app` und dann `npm start` (`npm install` nur nach frischem Clone). Voraussetzungen sind Node.js und uv. Liegt WoW woanders, `WOW_FOREVER_DIR` setzen. Feste Einstellungen liest die App aus `%APPDATA%\wowmcp-app\settings.json` (`wowDir`, `uvPath`). Die Datei gibt es erst, wenn man sie anlegt; in der Oberfläche gibt es dafür noch keinen Dialog.

**Seiten**

| Seite | Inhalt | Datenquelle |
| --- | --- | --- |
| Verbindung | WoW läuft?, Addon installiert/aktuell (Button „installieren/aktualisieren“), Auctionator-Scan, Item-Export, Charakter-Snapshots mit Alter und Exportfehlern, Aufträge an das Addon | `app_bridge.py status`, `addon.js`, PowerShell `Get-Process Wow` |
| Charakter | Kopfzeile (Stufe, Gold, XP), Kampfwerte, Ausrüstung mit Punkten und Tooltips, Berufe, Talente | `app_bridge.py character` |
| Verfügungen | Ruf-Fortschritt, beste Verfügung pro Stufe, Kosten pro Tag; Optionen Top-N und „eigene Items zum AH-Preis“ | `app_bridge.py writs` → `verfuegungen.compute()` |
| AH-Upgrades | Skala mit Begründung pro Gewicht (aufklappbar), Einkaufsliste „bestes Paket fürs Budget“ in Kaufreihenfolge (Punkte pro Gold, Summe bis hier), Karten pro Ausrüstungsplatz mit Gewinn, „zum Item“ in %, Preis, Punkte/Gold, Markierung „NEU in Forever“, „ab Stufe“ und „Zweihand“, Hover-Tooltips mit Vergleich „Derzeit angelegt“; Optionen Budget, Skala (automatisch/Leveln/Dungeon), Sortierung, Vorschau, Top-N | `app_bridge.py upgrades` → `upgrades.compute()` |

**Aufbau**

| Datei | Aufgabe |
| --- | --- |
| `app/main.js` | Hauptprozess: Fenster, IPC, Aufruf der Bridge (`uv run --quiet scripts/app_bridge.py …`, sucht uv im PATH, unter WinGet und in `~/.local/bin`), Dateiwächter auf `WTF\Account` (bei Änderungen an `WoWMCP.lua`/`Auctionator.lua` nach 1,5 s Ruhe → Event `data-changed`), Screenshot-Modus `--capture=<ordner>` |
| `app/preload.js` | Einzige Brücke zum Fenster (`window.wow`): `bridge(cmd, opts)`, `addonInfo()`, `installAddon()`, `createRequest(action, {full})`, `wowRunning()`, `openPath()`, `getSettings()`, `setSettings()`, `onDataChanged(fn)`. contextIsolation und sandbox sind an, nodeIntegration ist aus. |
| `app/addon.js` | Installieren und Aktualisieren des Addons (kopiert alle Dateien aus der `.toc`, behält eine offene `AppRequest.lua`, löscht das veraltete `ItemQueue.lua`), Versionsvergleich Quelle ↔ Installation, Schreiben und Lesen von Aufträgen. Hat keine Electron-Abhängigkeit. |
| `app/addon.test.js` | Tests für `addon.js` gegen einen temporären WoW-Ordner (`npm test`) |
| `app/renderer/index.html`, `app.js`, `style.css` | Oberfläche ohne Framework. Seiten-Registry `PAGES`, Cache pro Abfrage (wird bei `data-changed` und „Aktualisieren“ geleert), Tooltip-Registry (`tipAttr(tip, compare)` → `data-tip`). CSP: nur `'self'`, Inline-Styles erlaubt. |
| `scripts/app_bridge.py` | JSON-Schnittstelle der App: `status`, `character --char`, `writs --char [--top] [--besitz-zum-ah-preis]`, `upgrades --char [--max-preis] [--vorschau] [--top] [--profil] [--sort]`. Gibt immer genau ein JSON-Objekt aus, Fehler als `{"error": "…"}` mit Exit-Code 0. |

**App ↔ Addon:** Addons haben kein Datei- oder Netzwerk-I/O. Die App schreibt Aufträge nach `Interface\AddOns\WoWMCP\AppRequest.lua` (`WoWMCP_AppRequest = { id = <unix-zeit>, action = "items"|"sync", full = true? }`). Das Spiel liest die Datei bei `/reload`, das Addon führt jeden Auftrag einmal pro `id` aus (3 s nach dem Einloggen) und schreibt das Ergebnis nach `WoWMCP_Account.lastRequest`. Die App erkennt das am nächsten `/reload`. Forever liest bei `/reload` auch die `.toc` neu: Neue Dateien und neue SavedVariables laden ohne Spiel-Neustart (im Spiel getestet am 15.11.2026). Achtung: `/reload` speichert erst mit dem alten Code und lädt danach den neuen. Nach einem Addon-Update braucht es deshalb ein zweites `/reload`, bis Daten im neuen Format auf der Platte liegen.

**SavedVariables des Addons**

| Variable | Datei | Inhalt |
| --- | --- | --- |
| `WoWMCP_Char` | pro Charakter `…\<Name-Name>\SavedVariables\WoWMCP.lua` | `character`, `equipment` (ab 1.2 mit `tooltip` + `tipColors`), `bags`, `bank`, `quests`, `completedQuests`, `reputation`, `skills`, `talents`, `errors`, `client`, `addon.version`, `updated` |
| `WoWMCP_Items` | Account `…\SavedVariables\WoWMCP.lua` | Item-Export: `build`, `updated`, `count`, `missing`, `lastRun`, `items[id] = {name, quality, ilvl, reqLevel, equipLoc, classID, subclassID, bind, stats, tooltip, tipColors}` |
| `WoWMCP_Account` | Account `…\SavedVariables\WoWMCP.lua` | `autoItems` (Export beim Schließen des AH, Standard an), `lastRequest` |

**Tooltips:** Ab Addon 1.2 kommt der Original-Tooltip-Text (`C_TooltipInfo`, Zeile 1 = Name wird weggelassen, rechte Spalte als `"links | rechts"`) mit der Farbe pro Zeile (`tipColors`, Hex ohne `#`). Ältere Exporte haben keine Farben, dann rät die App sie aus dem Text (`Anlegen:`/`Benutzen:` grün, Zitat gelb). Angelegte Items ohne Tooltip baut `upgrades.tooltip()` aus den Werten nach (`rebuilt: true`, im Tooltip vermerkt). Icons gibt es keine, das Spiel liefert nur FileDataIDs.

**Prüfen nach Änderungen**
- `cd app` und `npm test`: Installieren, Update von 1.0, Aufträge, Stub ohne Phantom-Auftrag.
- `uv run scripts\test_upgrades.py`: Bewertung der Upgrades (nach Änderungen an `upgrades.py`).
- `npm run capture`: rendert alle Seiten mit echten Daten und speichert `app/captures/{connection,character,writs,upgrades,upgrades-slots,tooltip}.png` (`upgrades-slots` ist die Upgrade-Seite, zu den Karten pro Platz gescrollt). Danach die PNGs ansehen; das ist der Weg, die Oberfläche ohne Klicken zu prüfen. `captures/` ist in `.gitignore`.
- Bridge direkt: `uv run scripts\app_bridge.py upgrades --char Malgruk --max-preis 1`.
- Eine neue Seite braucht einen Eintrag in `PAGES` (`app.js`), einen Nav-Button in `index.html` und bei neuen Daten einen Befehl in `app_bridge.py`; zusätzlich die Seite in `runCapture` (`main.js`) eintragen.

## Upgrade-Bewertung (`scripts/upgrades.py`)

Prinzip wie Pawn: Gewicht pro Wert, Einheit ist 1 Punkt Schatten-Zaubermacht. Zwei Dinge gehen über eine feste Pawn-Skala hinaus: Die Gewichte für Treffer und Krit werden aus dem Charakter abgeleitet (`build_scale`), und bewertet wird immer das ganze Set (`set_value`, Gewinn = Wert nach dem Tausch minus Wert davor). So zählt Treffer nur bis zum Cap.

- **Stat-Schlüssel in Forever:** Treffer und Krit gibt es nur als gemeinsame Wertung für Nahkampf und Zauber (`ITEM_MOD_HIT_RATING_SHORT`, `ITEM_MOD_CRIT_RATING_SHORT`; Tooltip „Verbessert Eure Trefferchance um 1.0%“). Die Prozente stehen im Tooltip, sonst wird umgerechnet (10 bzw. 14 Wertung pro 1 %, `learn_ratings` misst das aus dem Export nach). `norm_stats` übersetzt alles in `sp`, `stamina`, `intellect`, `spirit`, `mp5`, `hp5`, `armor`, `wand_dps`, `hit`, `crit`. Heilung, andere Schulen, Zauberdurchschlag und Nahkampfwerte zählen 0.
- **Profile** (`PROFILES`): `leveln` (Gegner bis +2 Stufen; Ausdauer 0,5, Int 0,35, Wil 0,25, Mana/5s 0,7, Zauberstab-DPS 0,6) und `dungeon` (Bosse +3; 0,25/0,25/0,05/0,3/0,1). Automatisch: unter Stufe 60 `leveln`. Diese Gewichte sind Erfahrungswerte, nicht berechnet.
- **Treffer:** Fehlschlag gegen +0/+1/+2/+3 Stufen = 4/5/6/17 % (aus Forevers Charakteranzeige, `Camelot/PaperDollFrameStats.lua`). Cap = Fehlschlag − 1 % − `spellHit` des Charakters (`GetSpellHitModifier`, ohne Ausrüstung; Malgruk hat 5 %). Beim Leveln ist das Cap damit erreicht, Treffer zählt 0. Im Profil `dungeon` zählt jedes Prozent bis zum Cap wie 1 % Schaden, für Gebrechen-Zauber (75 % des Schadens) abzüglich Unterdrückung.
- **Krit:** 1 % Schaden = (13 × Stufe + Schatten-Zaubermacht) / 100 Punkte; 1 % Krit = das × 0,5 (Krit-Bonus) × Anteil des Schadens, der kritisch treffen kann (25 %, mit Pandemie 3/3 85 %).
- **Annahmen, im Spiel noch nicht geprüft:** Unterdrückung gibt 2 % Treffer pro Rang und 1 % Fehlschlag bleibt immer (Classic-Werte); Pandemie lässt DoTs kritisch treffen (wie die SoD-Rune mit derselben Zauber-ID 427712). Die Talent-Texte exportiert das Addon nicht.
- **Vergleich:** Ringe und Schmuck gegen den schwächeren der beiden Plätze, Zweihand gegen Waffenhand + Schildhand, Schildhand-Item bei angelegtem Stab gegen den Stab. Spezialeffekte (`special_effects`: „Benutzen:“ und „Anlegen:“-Zeilen, die kein Wert aus `stats` sind) kann die Skala nicht bewerten; solche Items mit weniger als 5 Punkten Gewinn stehen unter „selbst ansehen“.
- **Paket** (`best_package`): Rucksackproblem mit höchstens einer Option pro Platzgruppe (Ring-Paare, Schmuck-Paare, Waffenhand + Schildhand als Kombination), ohne Items über der eigenen Stufe. Budget = `--max-preis` oder das eigene Gold. Die Liste ist nach Punkten pro Gold sortiert; ein kleineres Budget hat sein eigenes Optimum, das nicht immer der Anfang dieser Liste ist.

## Hinweise

- Item-Export (Addon ab 1.1): Die Kandidaten kommen direkt aus `Auctionator.Database.db` (alle ausrüstbaren Rüstungen und Waffen der letzten 4 Scan-Tage). Gespeichert wird Account-weit und inkrementell pro Client-Build. `/wowmcp items full` liest alles neu, etwa um Tooltip-Farben nachzuholen.
- Verfügungsliste in `verfuegungen.py` stammt von wow.gg (Nov 2026); neue Verfügungen dort in `WRITS` ergänzen.
- Änderungen am Addon zuerst in `addon/WoWMCP` machen, Version in der `.toc` hochzählen, prüfen und dann über die App („Addon aktualisieren“) ins Spiel bringen, danach `/reload`. Prüfen:
  - Syntax: `uv run --with lupa python -c "…load(src)…"` (Lua 5.4, reicht für Syntaxfehler).
  - `wow_lua_lint` mit `flavor: forever` akzeptiert nur Pfade unter `_retail_\Interface\AddOns`. Datei dafür kurz nach `_retail_\Interface\AddOns\_wowmcp_lint_tmp\` kopieren (ohne `.toc`, wird nicht geladen) und danach wieder löschen. `knownGlobals`: `WoWMCP_Char`, `WoWMCP_Items`, `WoWMCP_Account`, `WoWMCP_AppRequest`, `Auctionator`, `ReloadUI`.
- Git: Repo in `C:\Coding\wow`, Branch `main`, kein Remote. Nicht versioniert sind `node_modules/`, `scripts/cache/` (wago-Downloads), `app/captures/` und `.claude/settings.local.json`.
- Forever-Spieldaten (DB2) gibt es bei wago.tools als CSV, Produkt `wowf`, z. B. `https://wago.tools/db2/ItemSparse/csv?build=1.60.1.70938&locale=deDE`. `ItemSparse` ist dort unvollständig (rund ein Drittel der AH-Ausrüstung fehlt, vermutlich verschlüsselt). Item-Werte darum aus dem Spiel holen (Item-Export des Addons). `Item`, `SpellEffect`, `SpellName` und `ItemXItemEffect` sind vollständig.
- Zauberstein und Feuerstein sind in Forever laut Zaubertext Waffen-Verzauberungen („Erfüllt Eure Waffe …“, Zauberstein 36: +14 Schatten/+1 % Tempo, 48: +17/+2 %, 60: +21/+2 %). Sie belegen also vermutlich denselben Platz wie Zauberöl. Im Spiel noch nicht getestet.
- Die Konsole zeigt Umlaute aus Python ggf. kaputt an (cp1252); die Daten selbst sind korrekt.
