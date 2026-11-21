# WoW Forever Tools

[![CI](https://github.com/kejith/wow-forever-tools/actions/workflows/ci.yml/badge.svg)](https://github.com/kejith/wow-forever-tools/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kejith/wow-forever-tools)](https://github.com/kejith/wow-forever-tools/releases/latest)

Tools for World of Warcraft: Forever: a desktop app, an MCP server and scripts that evaluate character data, auction house scans and professions.

The data comes out of the game through two addons, each in its own repository:

| Addon | Repository | What it does |
| --- | --- | --- |
| WoWMCP | [kejith/WoWMCP](https://github.com/kejith/WoWMCP) | exports character, equipment, bags, bank, quests, reputation, professions, talents and auction house items into its SavedVariables; shows the app's lists in the game |
| GoldPerHour | [kejith/GoldPerHour](https://github.com/kejith/GoldPerHour) | gold per hour, split into herbs and loot |

The game writes SavedVariables to disk only on `/reload` or logout. The app, the scripts and the MCP server read them from there.

App, scripts and addons work with an English and a German game client and use the client's language. Names that come from the game (items, zones, professions) are always in the language of the client.

## Desktop app

### Install

Download `WoW-Forever-Tools-Setup-<version>.exe` from the [latest release](https://github.com/kejith/wow-forever-tools/releases/latest) and run it. The installer is not code signed, so Windows SmartScreen asks once ("More info", then "Run anyway").

The app brings everything it needs. On the first start it downloads Python and two libraries through its bundled [uv](https://docs.astral.sh/uv/), which needs a network connection. It finds the game under `C:\Program Files (x86)\World of Warcraft\_forever_`; for another place set `wowDir` in the settings file (see "Settings").

### Pages

- **Connection:** Is WoW Forever running? Are WoWMCP and GoldPerHour installed and current (install and update with one click)? How old are the auction house scan, the item export and the character snapshots? Which lists did the app send into the game, and has the game loaded them? Requests to the addon: item export, read all items again, character snapshot.
- **Character:** the character window as in the game: stats, equipment with points, professions, talents.
- **Writs:** the cheapest crafting writ per tier, net of the coin pouch, and the trade crates by silver per Merchant's Favor (`scripts/writs.py`).
- **AH upgrades:** upgrades per equipment slot with a budget, a preview of higher levels and a mark for items new in Forever (`scripts/upgrades.py`).
- **Professions:** profit per craft of the recipes you know, and missing recipes that pay more, with the source and price of the recipe (`scripts/recipes.py`).
- **Consumables:** what flasks, elixirs, buff food, weapon oils, scrolls, potions and runes add to your DPS or HPS in a fight, which of them exclude each other, and the best set of those you can get now; below that the strongest potions, food, drinks, bandages and buffs for the level (`scripts/consumables.py`). The Character page shows the same set, and the pages "AH upgrades" and "Rotations" can rate with it ("with consumables").
- **Gold per hour:** the running session of the GoldPerHour addon (herbs and loot, per hour, items, zones, gold curve), start/stop/reset, the addon's settings and the history of finished sessions (`scripts/goldperhour.py`).

The app watches `WTF\Account` and reloads as soon as the game writes SavedVariables.

### Addons

On the "Connection" page, "Install addon" downloads the newest GitHub release of the addon, keeps it in the app's data folder and copies it to `_forever_\Interface\AddOns`. "Check for updates" asks GitHub right away, otherwise the app asks every 15 minutes at most. Without a network it installs the release it downloaded last.

By hand: unpack the zip of the latest release ([WoWMCP](https://github.com/kejith/WoWMCP/releases/latest), [GoldPerHour](https://github.com/kejith/GoldPerHour/releases/latest)) into `_forever_\Interface\AddOns`. Log in once and open the bank once.

How app and addon talk: addons cannot write files or use the network. The app puts a request into `Interface\AddOns\WoWMCP\AppRequest.lua` (or `…\GoldPerHour\AppRequest.lua`). The game loads the file on the next `/reload`, the addon runs the request once and reports the result through its SavedVariables. An addon update also needs only a `/reload`, even with new files or new SavedVariables.

### Lists in the game

What the pages find is also available in the game, through the WoWMCP addon (1.7 or newer):

- **A window** (`/wowmcp show`, or it opens with the auction house) with one tab per list: AH upgrades, writs, trade crates, recipes, consumables. A click on a row searches the auction house for the item; a shift-click puts its link into the chat or the search box.
- **Auctionator shopping lists** "WoWMCP: …", one per list, with the rows the app suggests buying.
- **Tooltip lines** on the items of the lists, for example the gain of an upgrade or the profit of a recipe.
- **A chat line at vendors** that sell a recipe worth buying.

The app writes the lists for every character into `Interface\AddOns\WoWMCP\AppData.lua` whenever the game has saved (`scripts/ingame.py`); the game loads the file on the next `/reload`. After a new auction house scan that takes two: the first `/reload` saves the scan for the app, the second loads the new lists. The "Connection" page shows what was sent and whether the game has loaded it, and has "Send now" and the switch for sending automatically.

### Language

The language follows the game client (`SET textLocale` in `WTF\Config.wtf`): German for a German client, English for every other one. If the game is not found, the language of Windows decides. The select box at the bottom left overrides it (Automatic, English, Deutsch).

### Settings

`%APPDATA%\wowmcp-app\settings.json` (create it if you need it):

| Key | Meaning |
| --- | --- |
| `wowDir` | the `_forever_` folder, if the game is not in the default place. The environment variable `WOW_FOREVER_DIR` does the same. |
| `language` | `auto` (default), `en` or `de` |
| `uvPath` | another `uv` than the bundled one |
| `sendToGame` | `false`: do not write the lists for the game by itself (the switch on the "Connection" page) |
| `addonSources` | install an addon from a local checkout instead of the release: `{ "goldperhour": "<folder>", "wowmcp": "<folder>" }`. The environment variables `GOLDPERHOUR_ADDON_DIR` and `WOWMCP_ADDON_DIR` do the same. |

## Scripts

The scripts need [uv](https://docs.astral.sh/uv/) (`winget install astral-sh.uv`); their dependencies are listed in the script header and `uv run` installs them.

| Script | Purpose |
| --- | --- |
| `uv run scripts/writs.py --char <name>` | best writ per tier and the trade crates (`--top 3`, `--owned-at-ah-price`) |
| `uv run scripts/upgrades.py --char <name>` | equipment upgrades from the auction house (`--max-price <gold>`, `--profile leveling\|dungeon`, `--sort gain\|price`, `--preview 4`, `--top 3`) |
| `uv run scripts/recipes.py --char <name>` | profit per recipe (`--profession Alchemy`, `--top 15`) |
| `uv run scripts/consumables.py --char <name>` | the strongest potions, food, bandages and buffs for the level, and the best set of consumables for a fight (`--top 5`, `--all-buffs`, `--fight-seconds 60`) |
| `uv run scripts/ingame.py` | what the WoWMCP addon shows in the game, per character; `--write` puts it into the addon folder |
| `uv run scripts/goldperhour.py` | GoldPerHour sessions |
| `uv run scripts/app_bridge.py status` | what the app sees, as JSON |

Every script takes `--wow <folder>`. `writs.py`, `upgrades.py`, `recipes.py`, `consumables.py` and `ingame.py` print a table, or JSON with `--json`, and take `--lang auto|en|de` (default: the language of the game client; `WOW_TOOLS_LANG` sets it for all scripts; `ingame.py` always writes the client's language into the game). `goldperhour.py` and `app_bridge.py` always print JSON.

Before running them: scan the auction house with Auctionator, open every profession window once, then `/reload`. Prices are the lowest buyout of the last scan, not live offers.

## MCP server

`server/wow_mcp.py` serves the character data to Claude and other MCP clients. It needs uv; `mcp` and `lupa` are installed by `uv run`.

**Claude Code:**

```
claude mcp add --scope user wow-forever -- uv run C:\path\to\wow-forever-tools\server\wow_mcp.py
```

**Claude Desktop** (`%APPDATA%\Claude\claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "wow-forever": {
      "command": "uv",
      "args": ["run", "C:\\path\\to\\wow-forever-tools\\server\\wow_mcp.py"]
    }
  }
}
```

If WoW Forever is installed somewhere else, set `WOW_FOREVER_DIR` to the `_forever_` folder (in Claude Desktop with `"env": {"WOW_FOREVER_DIR": "..."}`).

| Tool | Content |
| --- | --- |
| `list_characters` | all exported characters |
| `get_character` | level, XP, gold, zone and coordinates, guild, PvP rank, combat stats |
| `get_equipment` | equipment with item level, stats, enchant ID, durability |
| `get_inventory` | bags and bank, totalled per item or per slot (`detailed`) |
| `find_item` | item search by name or ID over all characters |
| `get_quests` | quest log with objectives, progress, quest text, rewards |
| `check_completed_quests` | whether given quest IDs are turned in |
| `get_reputation` | reputation with progress within the current standing |
| `get_skills` | professions and all skill lines (weapons, languages) |
| `get_talents` | points per talent tree, learned talents, Legacy trees |

Limits:

- The data is as fresh as the last `/reload` or logout. Every answer carries `snapshot_age`.
- The bank can only be read while it is open and keeps that state until the next visit.
- No snapshot is taken in combat; it follows when the fight ends.

## Development

```
cd app
npm install
npm start
```

This needs Node.js 22 and uv. The app has no build step: plain JavaScript in `app/` (main process) and `app/renderer/` (window, ES modules). All numbers are computed in Python through `scripts/app_bridge.py`.

| Check | Command |
| --- | --- |
| App: addon install, release download, requests, translations | `cd app && npm test` |
| Scripts | `uv run scripts/test_writs.py` (also `test_upgrades.py`, `test_recipes.py`, `test_consumables.py`, `test_ingame.py`, `test_goldperhour.py`, `test_i18n.py`) |
| MCP server | `uv run server/test_wow_mcp.py` |
| Screenshots of every page | `cd app && npm run capture` (into `app/captures`; add `-- --lang=de` for German) |
| The app as it will be installed, unpacked | `cd app && npm run pack` (into `app/dist`) |
| The Windows installer | `cd app && npm run dist` (on Windows) |

No test needs the game or a network connection.

### Translations

English is the language of the code. A text is written in English where it is used and translated in a catalog; a missing entry falls back to English.

| Part | Mark a text | German catalog | Test |
| --- | --- | --- | --- |
| Scripts | `tr("…")`, `mark("…")` from `scripts/i18n.py` | `scripts/locale_de.py` | `scripts/test_i18n.py` |
| App, main process | `t('…')` from `app/i18n.js` | `app/locales/de.js` | `app/i18n.test.js` |
| App, window | `t('…')`, `mark('…')` from `app/renderer/i18n.js`; `data-i18n` in `index.html` | `app/renderer/locales/de.js` | `app/i18n.test.js` |

The tests fail when a text has no German entry, when an entry is no longer used or when the placeholders differ. For another language add a catalog and its code to `LANGUAGES` in the three `i18n` files.

### Releases

Releases are automatic. Raise `version` in `app/package.json` and push to `main`:

1. CI runs the tests on Linux and Windows, builds the Windows installer and starts the built app once against test data.
2. If the version has no tag yet, CI creates the tag `v<version>` and starts the release workflow.
3. The release workflow builds the installer again from the tag and publishes it as a GitHub release, with a checksum and the commits since the last release.

A tag pushed by hand (`git tag v<version> && git push origin v<version>`) starts the same release workflow. The tag must match the version in `app/package.json`.

The addons are released the same way from their own repositories, and from there also to CurseForge and Wago Addons once the project ids and API tokens are set (see the README of each addon). CurseForge and Wago host addons only, so the desktop app is published here on GitHub.
