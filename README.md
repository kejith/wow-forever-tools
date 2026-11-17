# wow-forever-tools

Werkzeuge für World of Warcraft: Forever: eine Desktop-App, ein MCP-Server und Skripte, die Charakterdaten, Auktionshaus-Scans und Berufe auswerten.

Die Daten kommen aus dem Spiel über zwei Addons mit eigenen Repositories:

| Addon | Repository | Wofür |
| --- | --- | --- |
| WoWMCP | [kejith/WoWMCP](https://github.com/kejith/WoWMCP) | exportiert Charakter, Ausrüstung, Taschen, Bank, Quests, Ruf, Berufe, Talente und AH-Items in seine SavedVariables |
| GoldPerHour | [kejith/GoldPerHour](https://github.com/kejith/GoldPerHour) | Gold pro Stunde, getrennt nach Kräutern und Loot |

WoW schreibt SavedVariables erst bei `/reload` oder Logout auf die Platte. App, Skripte und MCP-Server lesen sie von dort.

## Installation

### 1. Addons

Am einfachsten über die Desktop-App (siehe unten): Seite „Verbindung“ → „Addon installieren“. Die App lädt das neueste Release des Addons von GitHub, legt es in ihrem Datenordner ab und kopiert es nach `_forever_\Interface\AddOns`. „Nach Updates suchen“ fragt GitHub sofort, sonst höchstens alle 15 Minuten. Ohne Netz installiert die App das zuletzt geladene Release.

Von Hand: das Zip aus dem neuesten Release ([WoWMCP](https://github.com/kejith/WoWMCP/releases/latest), [GoldPerHour](https://github.com/kejith/GoldPerHour/releases/latest)) nach `C:\Program Files (x86)\World of Warcraft\_forever_\Interface\AddOns` entpacken. Einmal einloggen, Bank einmal öffnen.

Für die Entwicklung an einem Addon installiert die App statt des Release einen lokalen Checkout: `addonSources` in `%APPDATA%\wowmcp-app\settings.json` (`{ "addonSources": { "goldperhour": "<Ordner>" } }`) oder die Umgebungsvariablen `WOWMCP_ADDON_DIR` und `GOLDPERHOUR_ADDON_DIR`.

In-game:

- `/wowmcp sync`: Snapshot erstellen und `/reload`, damit Server und App aktuelle Daten sehen
- `/wowmcp items [full]`: Werte und Tooltips aller ausrüstbaren Items aus den Auctionator-Scans der letzten 4 Tage lesen (nur neue, mit `full` alle), danach `/reload`
- `/wowmcp auto on|off`: Item-Export automatisch beim Schließen des Auktionshauses (Standard: an)
- `/wowmcp status`: letzter Snapshot, Bank-Scan, Item-Export, App-Aufträge, Exportfehler

## Desktop-App

Electron-App in `app/` mit sechs Seiten:

- **Verbindung:** Läuft WoW Forever? Sind die Addons WoWMCP und GoldPerHour installiert und aktuell (Installieren/Aktualisieren per Klick)? Wie alt sind AH-Scan, Item-Export und die Charakter-Snapshots? Dazu Aufträge an das Addon: Item-Export, alle Items neu lesen, Charakter-Snapshot.
- **Charakter:** Werte, Ausrüstung mit Punkten, Berufe, Talente
- **Verfügungen:** die günstigste Handwerksverfügung pro Stufe netto nach Münzbeutel, dazu die Handelskisten nach Silber pro Händlergunst (`scripts/verfuegungen.py`)
- **AH-Upgrades:** Upgrades pro Ausrüstungsplatz mit Budget, Vorschau und Markierung neuer Forever-Items (`scripts/upgrades.py`)
- **Berufe:** Gewinn pro Herstellung der gelernten Rezepte und noch fehlende Rezepte, die mehr bringen, mit Quelle und Preis des Rezepts (`scripts/recipes.py`)
- **Gold pro Stunde:** die laufende Sitzung des Addons GoldPerHour (Kräuter und Loot, pro Stunde, Items, Zonen, Goldkurve), Start/Stop/Zurücksetzen, die Einstellungen des Addons und der Verlauf abgeschlossener Sitzungen (`scripts/goldperhour.py`)

```
cd app
npm install
npm start
```

Benötigt Node.js und uv. Die App beobachtet `WTF\Account` und lädt neu, sobald WoW SavedVariables schreibt (`/reload`, Logout). Berechnet wird in Python über `scripts/app_bridge.py`.

So kommen App und Addon zusammen: Addons können keine Dateien schreiben oder ins Netz. Die App legt einen Auftrag in `Interface\AddOns\WoWMCP\AppRequest.lua` (bzw. `…\GoldPerHour\AppRequest.lua`) ab. Das Spiel lädt die Datei beim nächsten `/reload`, das Addon führt den Auftrag einmal aus und meldet das Ergebnis über seine SavedVariables. Auch Addon-Updates brauchen nur ein `/reload`, selbst wenn neue Dateien oder SavedVariables dazukommen.

`npm test` prüft Installation, Release-Download und Aufträge gegen einen Testordner und einen nachgebauten GitHub-Server, ohne Netz. `npm run capture` speichert Screenshots aller Seiten nach `app/captures`.

### 2. MCP-Server

Benötigt [uv](https://docs.astral.sh/uv/) (`winget install astral-sh.uv`). Die Abhängigkeiten (`mcp`, `lupa`) stehen im Skript-Header und werden von `uv run` automatisch installiert.

Der Server liegt in diesem Repo unter `server/wow_mcp.py`. Die Beispiele nehmen einen Clone unter `C:\Coding\wow` an. `.mcp.json` trägt ihn für Claude Code unter Windows mit diesem Pfad ein.

**Claude Code:**

```
claude mcp add --scope user wow-forever -- uv run C:\Coding\wow\server\wow_mcp.py
```

**Claude Desktop** (`%APPDATA%\Claude\claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "wow-forever": {
      "command": "uv",
      "args": ["run", "C:\\Coding\\wow\\server\\wow_mcp.py"]
    }
  }
}
```

Liegt WoW Forever woanders, Umgebungsvariable `WOW_FOREVER_DIR` auf den `_forever_`-Ordner setzen (in Claude Desktop über `"env": {"WOW_FOREVER_DIR": "..."}`).

## Tools

| Tool | Inhalt |
| --- | --- |
| `list_characters` | Alle exportierten Charaktere |
| `get_character` | Level, XP, Gold, Zone/Koordinaten, Gilde, PvP-Rang, Kampfwerte |
| `get_equipment` | Ausrüstung mit Itemlevel, Stats, Enchant-ID, Haltbarkeit |
| `get_inventory` | Taschen und Bank, summiert pro Item oder pro Slot (`detailed`) |
| `find_item` | Itemsuche nach Name oder ID über alle Charaktere |
| `get_quests` | Questlog mit Zielen, Fortschritt, Questtext, Belohnungen |
| `check_completed_quests` | Ob bestimmte Quest-IDs abgegeben sind |
| `get_reputation` | Ruf mit Fortschritt im aktuellen Rang |
| `get_skills` | Berufe und alle Skill-Linien (Waffen, Sprachen) |
| `get_talents` | Punkte pro Talentbaum, gelernte Talente, Legacy-Bäume |

## Einschränkungen

- Daten sind so aktuell wie das letzte `/reload` bzw. der letzte Logout. Jede Antwort enthält `snapshot_age`.
- Bankinhalt ist nur bei geöffneter Bank lesbar und bleibt bis zum nächsten Besuch auf diesem Stand.
- Im Kampf werden keine Snapshots erstellt (Werte sind dann teils "secret"); der Snapshot folgt nach Kampfende.
- Das Addon klappt beim Snapshot alle Ruf-Kategorien auf, sonst sind eingeklappte Fraktionen nicht lesbar.
- Getestet gegen einen nachgebauten Forever-Client, nicht im echten Spiel. Die API-Aufrufe folgen der dokumentierten Forever-API (1.60.1, Mainline-Familie); unverifiziert sind vor allem die Bank-Container-IDs und `C_SkillInfo`. Fehler pro Bereich zeigt `/wowmcp status` bzw. `export_errors` in `get_character`.
