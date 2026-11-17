# /// script
# requires-python = ">=3.11"
# dependencies = ["mcp>=2.3,<3", "lupa>=2.0"]
# ///
"""MCP server for WoW Forever character data exported by the WoWMCP addon.

The addon writes one SavedVariables file per character, which the client flushes to disk on /reload
or logout. This server parses those files in a sandboxed Lua runtime and serves them as MCP tools.

Set WOW_FOREVER_DIR if WoW Forever is not installed in the default location.
"""

from __future__ import annotations

import os
import re
import threading
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

from lupa.lua54 import LuaError, LuaRuntime, lua_type
from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

# Under WSL the Windows install is reached through /mnt/c.
DEFAULT_ROOT = ("/mnt/c/Program Files (x86)/World of Warcraft/_forever_" if os.name == "posix"
                else r"C:\Program Files (x86)\World of Warcraft\_forever_")
SV_FILE = "WoWMCP.lua"
SV_GLOBAL = "WoWMCP_Char"

QUALITY = {0: "Poor", 1: "Common", 2: "Uncommon", 3: "Rare", 4: "Epic", 5: "Legendary", 6: "Artifact", 7: "Heirloom"}
SLOTS = {
    0: "Ammo", 1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt", 5: "Chest", 6: "Waist", 7: "Legs", 8: "Feet",
    9: "Wrist", 10: "Hands", 11: "Finger 1", 12: "Finger 2", 13: "Trinket 1", 14: "Trinket 2", 15: "Back",
    16: "Main Hand", 17: "Off Hand", 18: "Ranged", 19: "Tabard",
}

# Runs the SavedVariables chunk in an empty environment: no stdlib, text chunks only.
_SANDBOX_LOADER = b"""
return function(src, global)
  local env = {}
  local chunk, err = load(src, "=SavedVariables", "t", env)
  if not chunk then error(err, 0) end
  chunk()
  return env[global]
end
"""

_ENCHANT_RE = re.compile(r"item:\d+:(\d+)")


def _to_python(value: Any) -> Any:
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    if lua_type(value) != "table":
        return value
    items = [(_to_python(k), _to_python(v)) for k, v in value.items()]
    keys = [k for k, _ in items]
    if keys and all(type(k) is int for k in keys) and sorted(keys) == list(range(1, len(keys) + 1)):
        return [v for _, v in sorted(items, key=lambda kv: kv[0])]
    return {str(k): v for k, v in items}


class SavedVariablesReader:
    def __init__(self) -> None:
        self._lua = LuaRuntime(encoding=None, register_eval=False, register_builtins=False)
        self._load = self._lua.execute(_SANDBOX_LOADER)
        self._lock = threading.Lock()  # a Lua state must not be entered concurrently

    def read(self, path: Path) -> Any:
        src = path.read_bytes()
        with self._lock:
            return _to_python(self._load(src, SV_GLOBAL.encode()))


@dataclass(frozen=True)
class Character:
    path: Path
    mtime: float
    data: dict[str, Any]

    @property
    def info(self) -> dict[str, Any]:
        info = self.data.get("character")
        return info if isinstance(info, dict) else {}

    @property
    def first_name(self) -> str:
        return str(self.info.get("name") or self.path.parents[1].name)

    @property
    def display_name(self) -> str:
        surname = self.info.get("surname")
        return f"{self.first_name} {surname}" if surname else self.first_name

    @property
    def ruleset(self) -> str:
        # WTF/Account/<account>/<ruleset>/<character>/SavedVariables/WoWMCP.lua
        return self.path.parents[2].name

    @property
    def updated(self) -> int:
        return int(self.data.get("updated") or self.mtime)


class Store:
    def __init__(self, root: Path) -> None:
        self.account_dir = root / "WTF" / "Account"
        self._reader = SavedVariablesReader()
        self._cache: dict[Path, Character] = {}

    def characters(self) -> list[Character]:
        if not self.account_dir.is_dir():
            raise ToolError(f"WoW account folder not found: {self.account_dir}. Set WOW_FOREVER_DIR to the _forever_ folder.")
        found: list[Character] = []
        for path in self.account_dir.glob(f"**/SavedVariables/{SV_FILE}"):
            try:
                mtime = path.stat().st_mtime
            except OSError:
                continue
            cached = self._cache.get(path)
            if cached is None or cached.mtime != mtime:
                try:
                    data = self._reader.read(path)
                except (OSError, LuaError):
                    # Typically a file caught mid-write; serve the previous parse if there is one.
                    if cached is not None:
                        found.append(cached)
                    continue
                if not isinstance(data, dict):
                    continue
                cached = Character(path, mtime, data)
                self._cache[path] = cached
            found.append(cached)
        return found

    def resolve(self, name: str | None) -> Character:
        chars = self.characters()
        if not chars:
            raise ToolError("No WoWMCP data yet. Enable the addon, log in, then /reload or log out so WoW writes it to disk.")
        if not name or not name.strip():
            return max(chars, key=lambda c: c.updated)
        needle = name.strip().casefold()
        exact = [c for c in chars if needle in (c.display_name.casefold(), c.first_name.casefold())]
        matches = exact or [c for c in chars if needle in c.display_name.casefold()]
        if len(matches) == 1:
            return matches[0]
        if not matches:
            raise ToolError(f"No character matches {name!r}. Known: {', '.join(c.display_name for c in chars)}")
        raise ToolError("Ambiguous name, matches: " + ", ".join(f"{c.display_name} ({c.ruleset})" for c in matches))


store = Store(Path(os.environ.get("WOW_FOREVER_DIR", DEFAULT_ROOT)))

mcp = MCPServer(
    "wow-forever",
    instructions=(
        "Character data from World of Warcraft: Forever, exported by the WoWMCP addon. Every result is a snapshot "
        "from the player's last /reload or logout; check snapshot_age and, if it matters for the answer, ask the "
        "player to type /wowmcp sync in-game. Bank contents are as of the last bank visit (bank_scanned). "
        "The `character` argument takes a full or partial name; omit it for the most recently played character."
    ),
)


def _compact(d: dict[str, Any]) -> dict[str, Any]:
    return {k: v for k, v in d.items() if v is not None and v != [] and v != {}}


def _list(value: Any) -> list:
    return value if isinstance(value, list) else []  # empty Lua tables arrive as {}


def _money(copper: Any) -> str | None:
    if not isinstance(copper, (int, float)):
        return None
    gold, rest = divmod(int(copper), 10000)
    silver, copper = divmod(rest, 100)
    return f"{gold}g {silver}s {copper}c"


def _when(ts: int) -> dict[str, str]:
    seconds = max(0, int(time.time() - ts))
    age = f"{seconds}s ago"
    for unit, size in (("d", 86400), ("h", 3600), ("min", 60)):
        if seconds >= size:
            age = f"{seconds // size}{unit} ago"
            break
    return {"time": datetime.fromtimestamp(ts).isoformat(timespec="seconds"), "age": age}


def _meta(c: Character) -> dict[str, Any]:
    when = _when(c.updated)
    return {"character": c.display_name, "ruleset": c.ruleset, "snapshot": when["time"], "snapshot_age": when["age"]}


def _section(c: Character, key: str) -> Any:
    if key in c.data:
        return c.data[key]
    error = (c.data.get("errors") or {}).get(key)
    raise ToolError(f"{c.display_name} has no {key} data" + (f": addon error: {error}" if error else "."))


def _stat_name(key: str) -> str:
    if key == "RESISTANCE0_NAME":
        return "armor"
    return key.removeprefix("ITEM_MOD_").removesuffix("_SHORT").lower()


def _item(raw: dict[str, Any], *, with_stats: bool = False) -> dict[str, Any]:
    out = {
        "id": raw.get("id"),
        "name": raw.get("name"),
        "count": raw.get("count", 1),
        "quality": QUALITY.get(raw.get("quality")),
        "ilvl": raw.get("ilvl"),
        "reqLevel": raw.get("reqLevel"),
        "type": raw.get("type"),
        "subType": raw.get("subType"),
        "bound": raw.get("bound"),
    }
    match = _ENCHANT_RE.search(raw.get("link") or "")
    if match and match.group(1) != "0":
        out["enchantId"] = int(match.group(1))
    if with_stats and isinstance(raw.get("stats"), dict):
        out["stats"] = {_stat_name(k): v for k, v in raw["stats"].items()}
    return _compact(out)


def _containers(c: Character, include_bank: bool) -> list[tuple[str, dict[str, Any]]]:
    out = [("bags", b) for b in _list(c.data.get("bags"))]
    if include_bank and isinstance(c.data.get("bank"), dict):
        out += [("bank", b) for b in _list(c.data["bank"].get("containers"))]
    return out


@mcp.tool()
def list_characters() -> list[dict[str, Any]]:
    """List every character with exported data, most recent snapshot first."""
    chars = sorted(store.characters(), key=lambda c: c.updated, reverse=True)
    return [
        _compact({**_meta(c), "level": c.info.get("level"), "class": c.info.get("class"),
                  "race": c.info.get("race"), "zone": c.info.get("zone")})
        for c in chars
    ]


@mcp.tool()
def get_character(character: str | None = None) -> dict[str, Any]:
    """Level, XP, gold, location (zone and map coordinates in %), guild, PvP rank, and combat stats."""
    c = store.resolve(character)
    info = dict(_section(c, "character"))
    xp, xp_max, rested = info.pop("xp", None), info.pop("xpMax", None), info.pop("rested", None)
    if xp is not None and xp_max:
        info["xp"] = _compact({"current": xp, "max": xp_max, "percent": round(100 * xp / xp_max, 1), "rested": rested})
    info["money"] = _money(info.get("money"))
    info.pop("name", None)
    info.pop("surname", None)
    return _compact({**_meta(c), **info, "export_errors": c.data.get("errors")})


@mcp.tool()
def get_equipment(character: str | None = None) -> dict[str, Any]:
    """Equipped items per slot with item level, stats, enchant ID and durability."""
    c = store.resolve(character)
    items = []
    for raw in _list(_section(c, "equipment")):
        entry = {"slot": SLOTS.get(raw.get("slot"), raw.get("slot")), **_item(raw, with_stats=True)}
        durability = raw.get("durability")
        if isinstance(durability, list) and len(durability) == 2:
            entry["durability"] = f"{durability[0]}/{durability[1]}"
        items.append(entry)
    return {**_meta(c), "equipment": items}


@mcp.tool()
def get_inventory(character: str | None = None, include_bank: bool = True, detailed: bool = False) -> dict[str, Any]:
    """Bag (and bank) contents. By default items are totalled by item with per-location counts;
    `detailed` returns every container and slot instead."""
    c = store.resolve(character)
    bags = _list(_section(c, "bags"))
    result: dict[str, Any] = {
        **_meta(c),
        "bag_slots": {"total": sum(b.get("size") or 0 for b in bags), "free": sum(b.get("free") or 0 for b in bags)},
    }
    if include_bank:
        bank = c.data.get("bank")
        if isinstance(bank, dict):
            result["bank_scanned"] = _when(int(bank.get("updated") or 0))
        else:
            result["bank"] = (c.data.get("errors") or {}).get("bank") or "Never scanned: open the bank in-game once, then /reload."

    containers = _containers(c, include_bank)
    if detailed:
        result["containers"] = [
            _compact({"location": location, "bag": b.get("name") or b.get("id"), "size": b.get("size"), "free": b.get("free"),
                      "items": [{"slot": i.get("slot"), **_item(i)} for i in _list(b.get("items"))]})
            for location, b in containers
        ]
        return result

    totals: dict[Any, dict[str, Any]] = {}
    for location, b in containers:
        for raw in _list(b.get("items")):
            key = raw.get("id") or raw.get("name")
            entry = totals.setdefault(key, {**_item(raw), "count": 0})
            count = raw.get("count") or 1
            entry["count"] += count
            entry[location] = entry.get(location, 0) + count
    result["items"] = sorted(totals.values(), key=lambda e: str(e.get("name") or ""))
    return result


@mcp.tool()
def find_item(query: str, character: str | None = None) -> dict[str, Any]:
    """Find items by name (case-insensitive substring) or numeric item ID in equipment, bags and bank.
    Searches all characters unless `character` is given."""
    needle = query.strip().casefold()
    if not needle:
        raise ToolError("query must not be empty")
    item_id = int(needle) if needle.isdigit() else None
    chars = [store.resolve(character)] if character else store.characters()
    matches = []
    for c in chars:
        sources = [("equipped", _list(c.data.get("equipment")))]
        sources += [(location, _list(b.get("items"))) for location, b in _containers(c, include_bank=True)]
        for location, items in sources:
            for raw in items:
                hit = raw.get("id") == item_id if item_id is not None else needle in str(raw.get("name") or "").casefold()
                if hit:
                    matches.append({"character": c.display_name, "location": location, **_item(raw)})
    return {"query": query, "matches": matches}


@mcp.tool()
def get_quests(character: str | None = None) -> dict[str, Any]:
    """Active quest log: objectives with progress, quest text, level, zone category and rewards."""
    c = store.resolve(character)
    quests = []
    for q in _list(_section(c, "quests")):
        objectives = [
            _compact({"text": o.get("text"), "done": bool(o.get("finished")),
                      "progress": f"{o.get('have', 0)}/{o['need']}" if o.get("need") else None})
            for o in _list(q.get("objectives"))
        ]
        quests.append(_compact({
            "id": q.get("id"), "title": q.get("title"), "level": q.get("level"), "category": q.get("category"),
            "suggestedGroup": q.get("suggestedGroup"), "complete": q.get("complete"), "failed": q.get("failed"),
            "objectives": objectives, "objectiveText": q.get("objectiveText"), "description": q.get("description"),
            "rewardXP": q.get("rewardXP"), "rewardMoney": _money(q.get("rewardMoney")) if q.get("rewardMoney") else None,
        }))
    return {**_meta(c), "count": len(quests), "quests": quests}


@mcp.tool()
def check_completed_quests(quest_ids: list[int], character: str | None = None) -> dict[str, Any]:
    """Whether the given quest IDs have been turned in. Quests currently in the log count as not completed."""
    c = store.resolve(character)
    done = set(_list(_section(c, "completedQuests")))
    return {**_meta(c), "total_completed": len(done), "results": {str(q): q in done for q in quest_ids}}


@mcp.tool()
def get_reputation(character: str | None = None) -> dict[str, Any]:
    """Faction standings with progress within the current standing."""
    c = store.resolve(character)
    factions = []
    for f in _list(_section(c, "reputation")):
        value, low, high = f.get("value"), f.get("min"), f.get("max")
        progress = f"{value - low}/{high - low}" if None not in (value, low, high) and high > low else None
        factions.append(_compact({"name": f.get("name"), "category": f.get("category"), "standing": f.get("standing"),
                                  "progress": progress, "atWar": f.get("atWar"), "id": f.get("id")}))
    return {**_meta(c), "factions": factions}


@mcp.tool()
def get_skills(character: str | None = None) -> dict[str, Any]:
    """Professions plus every skill line (weapon skills, languages, secondary skills) with rank and max."""
    c = store.resolve(character)
    skills = _section(c, "skills")
    # Forever lists each profession under two skill lines (e.g. Alchemy 171 and 2937); keep the first.
    lines, seen = [], set()
    for s in _list(skills.get("skills")):
        line = _compact({"name": s.get("name") or s.get("skillName"), "category": s.get("category"), "rank": s.get("rank"),
                         "max": s.get("maxRank"), "modifier": s.get("modifier") or None, "skillID": s.get("skillID")})
        key = (line.get("name"), line.get("category"), line.get("rank"), line.get("max"))
        if key not in seen:
            seen.add(key)
            lines.append(line)
    return {**_meta(c), "professions": _list(skills.get("professions")), "skills": lines}


@mcp.tool()
def get_talents(character: str | None = None) -> dict[str, Any]:
    """Learned class talents (points per tab and each talent's rank) and Legacy trees."""
    c = store.resolve(character)
    talents = _section(c, "talents")

    def tree(t: dict[str, Any]) -> dict[str, Any]:
        return _compact({
            "label": t.get("label"), "spent": t.get("spent"), "unspent": t.get("unspent"),
            "tabs": {g.get("name") or str(g.get("id")): g.get("spent", 0) for g in _list(t.get("groups"))},
            "talents": [_compact({"name": x.get("name"), "tab": x.get("group"), "rank": f"{x.get('rank')}/{x.get('maxRank')}",
                                  "spellID": x.get("spellID")}) for x in _list(t.get("talents"))],
        })

    return {**_meta(c), "class": [tree(t) for t in _list(talents.get("class"))],
            "legacy": [tree(t) for t in _list(talents.get("legacy"))]}


if __name__ == "__main__":
    mcp.run()
