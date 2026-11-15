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
| `scripts/upgrades.py` | Ausrüstungs-Upgrades aus dem AH (Gewichte für Gebrechen-Hexer) gegen die angelegte Ausrüstung, inkl. Forever-Items (ID > 200000). Liest den Item-Export des Addons (`WoWMCP_Items` in der Account-SavedVariables `WoWMCP.lua`). | Im AH scannen und AH schließen (das Addon exportiert neue Items dann selbst) oder `/wowmcp items`, danach `/reload`. Dann `uv run scripts\upgrades.py --char Malgruk` (Optionen: `--max-preis 1`, `--vorschau 0`, `--top 5`, `--json`) |
| `scripts/app_bridge.py` | JSON-Schnittstelle der Desktop-App zu den Skripten (`status`, `character`, `writs`, `upgrades`) | `uv run scripts\app_bridge.py status` |
| `app/` | Electron-App: Verbindung zum Addon (installieren/aktualisieren, Aufträge, Live-Abgleich der SavedVariables), Charakter, Verfügungen, AH-Upgrades. Rechnet über `app_bridge.py`. | `cd app`, `npm install`, `npm start`. Screenshots aller Seiten: `npm run capture` (→ `app/captures`), Tests: `npm test` |
| `server/wow_mcp.py` | MCP-Server `wow-forever` (uv-Skript, Abhängigkeiten im Header) | wird von Claude gestartet: `uv run server\wow_mcp.py` |
| `addon/WoWMCP/` | Quelle des Export-Addons (1.1.0). Installiert liegt es unter `…\_forever_\Interface\AddOns\WoWMCP`; die App installiert und aktualisiert es. | In-game `/wowmcp sync`, `/wowmcp items [full]`, `/wowmcp auto on/off`, `/wowmcp status` |
| `notes/wow_forever_settings.txt` | Versteckte Grafik-CVars (Schärfe, Gras, Sichtweite, LOD) inkl. Reset-Befehle | im Chat `/console …` |

Daten-Aktualität: WoW schreibt SavedVariables nur bei `/reload` oder Logout. Für `verfuegungen.py` vorher einen vollen AH-Scan mit Auctionator machen und dann `/reload`. AH-Preise sind Mindest-Sofortkauf beim letzten Scan, keine Live-Angebote.

## Hinweise

- App ↔ Addon: Addons haben kein Datei- oder Netzwerk-I/O. Die App schreibt Aufträge nach `Interface\AddOns\WoWMCP\AppRequest.lua`. Die Datei wird bei `/reload` neu gelesen, jeder Auftrag läuft einmal pro `id`. Ergebnisse kommen über die SavedVariables zurück (`WoWMCP_Account.lastRequest`). Neue Dateien in der `.toc` lädt WoW erst nach einem Spiel-Neustart, `/reload` reicht dafür nicht.
- Item-Export (Addon 1.1): Die Kandidaten kommen direkt aus `Auctionator.Database.db` (alle ausrüstbaren Rüstungen und Waffen der letzten 4 Scan-Tage). Gespeichert wird Account-weit und inkrementell pro Client-Build.
- Verfügungsliste in `verfuegungen.py` stammt von wow.gg (Nov 2026); neue Verfügungen dort in `WRITS` ergänzen.
- Änderungen am Addon zuerst in `addon/WoWMCP` machen, mit `wow_lua_lint` / `wow_toc_validate` prüfen und dann nach `Interface\AddOns` kopieren.
- Forever-Spieldaten (DB2) gibt es bei wago.tools als CSV, Produkt `wowf`, z. B. `https://wago.tools/db2/ItemSparse/csv?build=1.60.1.70938&locale=deDE`. `ItemSparse` ist dort unvollständig (rund ein Drittel der AH-Ausrüstung fehlt, vermutlich verschlüsselt). Item-Werte darum aus dem Spiel holen (Item-Export des Addons). `Item`, `SpellEffect`, `SpellName` und `ItemXItemEffect` sind vollständig.
- Zauberstein und Feuerstein sind in Forever laut Zaubertext Waffen-Verzauberungen („Erfüllt Eure Waffe …“, Zauberstein 36: +14 Schatten/+1 % Tempo, 48: +17/+2 %, 60: +21/+2 %). Sie belegen also vermutlich denselben Platz wie Zauberöl. Im Spiel noch nicht getestet.
- Die Konsole zeigt Umlaute aus Python ggf. kaputt an (cp1252); die Daten selbst sind korrekt.
