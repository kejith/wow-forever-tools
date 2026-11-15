# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Ausrüstungs-Upgrades aus dem Auktionshaus für einen Charakter (Standard: Gebrechen-Hexer).

Liest
  * den Auctionator-Scan (Mindest-Sofortkauf, wie scripts/verfuegungen.py),
  * aus WoWMCP.lua die angelegte Ausrüstung (pro Charakter) und den Item-Export des Addons
    (Account-weit, WoWMCP_Items: Werte und Tooltips aller ausrüstbaren Items aus den Auctionator-Scans).

Ablauf: im AH scannen, AH schließen (das Addon liest neue Items automatisch) oder "/wowmcp items",
auf die Meldung warten, dann /reload. Danach:
  uv run scripts\\upgrades.py --char Malgruk
"""

from __future__ import annotations

import argparse
import datetime
import glob
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import verfuegungen as v  # noqa: E402  (load_prices, DEFAULT_WOW)

# Gewichte pro Punkt: Schattenschaden/Zaubermacht = 1. Heilung, andere Schulen und Nahkampfwerte zählen nicht.
WEIGHTS = {
    "spell_power": 1.0, "spell_damage_done": 1.0, "shadow_damage_done": 1.0,
    "stamina": 0.5, "intellect": 0.35, "spirit": 0.25,
    "hit_spell_rating": 10.0, "spell_hit": 10.0, "crit_spell_rating": 5.0, "spell_crit": 5.0,
    "armor": 0.01,
}
WAND_DPS_WEIGHT = 0.6

SLOT_OF = {  # equipLoc -> Inventarplätze
    "INVTYPE_HEAD": [1], "INVTYPE_NECK": [2], "INVTYPE_SHOULDER": [3], "INVTYPE_CHEST": [5], "INVTYPE_ROBE": [5],
    "INVTYPE_WAIST": [6], "INVTYPE_LEGS": [7], "INVTYPE_FEET": [8], "INVTYPE_WRIST": [9], "INVTYPE_HAND": [10],
    "INVTYPE_FINGER": [11, 12], "INVTYPE_TRINKET": [13, 14], "INVTYPE_CLOAK": [15],
    "INVTYPE_WEAPON": [16], "INVTYPE_WEAPONMAINHAND": [16], "INVTYPE_HOLDABLE": [17], "INVTYPE_WEAPONOFFHAND": [17],
    "INVTYPE_RANGED": [18], "INVTYPE_RANGEDRIGHT": [18], "INVTYPE_2HWEAPON": [16, 17],
}
SLOT_NAME = {1: "Kopf", 2: "Hals", 3: "Schulter", 5: "Brust", 6: "Taille", 7: "Beine", 8: "Füße", 9: "Handgelenk",
             10: "Hände", 11: "Finger", 12: "Finger", 13: "Schmuck", 14: "Schmuck", 15: "Rücken",
             16: "Waffenhand", 17: "Schildhand", 18: "Zauberstab"}
USABLE_WEAPONS = {7, 10, 15, 19}  # Einhandschwert, Stab, Dolch, Zauberstab
NEW_ID = 200000  # Item-IDs darüber gibt es nur in Forever
STANDARD_EFFECTS = ("verursachten Schaden und Heilung um bis zu", "von Schattenzaubern und -effekten verursachten",
                    "Heilung um bis zu", "durch Feuerzauber", "durch Frostzauber", "durch Arkanzauber",
                    "durch Naturzauber", "durch Heiligzauber", "von magischen Zaubern und Effekten verursachten Schaden",
                    "von Arkanzaubern", "von Feuerzaubern", "von Frostzaubern", "von Naturzaubern", "von Heiligzaubern")


def stat_key(k: str) -> str:
    return "armor" if k == "RESISTANCE0_NAME" else k.removeprefix("ITEM_MOD_").removesuffix("_SHORT").lower()


def lua_to_py(x):
    from lupa.lua54 import lua_type
    if lua_type(x) != "table":
        return x
    keys = list(x.keys())
    if keys and all(isinstance(k, int) for k in keys) and sorted(keys) == list(range(1, len(keys) + 1)):
        return [lua_to_py(x[k]) for k in sorted(keys)]
    return {k: lua_to_py(x[k]) for k in keys}


def load_sv(path: str, name: str):
    """Runs a SavedVariables file in a sandboxed Lua and returns one global as Python data."""
    from lupa.lua54 import LuaRuntime
    lua = LuaRuntime(register_eval=False, register_builtins=False)
    for g in ("os", "io", "package", "require", "dofile", "loadfile", "debug"):
        lua.execute(f"{g} = nil")
    lua.execute(open(path, encoding="utf-8", errors="replace").read())
    return lua_to_py(lua.globals()[name])


def load_char(path: str) -> dict:
    return load_sv(path, "WoWMCP_Char") or {}


def score(stats: dict | None, wand: bool = False) -> float:
    s = 0.0
    for k, val in (stats or {}).items():
        k = stat_key(k)
        if k == "damage_per_second":
            s += WAND_DPS_WEIGHT * val if wand else 0
        else:
            s += WEIGHTS.get(k, 0.0) * val
    return s


def short(stats: dict | None) -> str:
    names = {"spell_power": "ZM", "spell_damage_done": "ZS", "shadow_damage_done": "Schatten", "stamina": "Ausd",
             "intellect": "Int", "spirit": "Wil", "hit_spell_rating": "Treffer%", "spell_hit": "Treffer%",
             "crit_spell_rating": "Krit%", "spell_crit": "Krit%", "damage_per_second": "DPS"}
    parts = []
    for k, val in (stats or {}).items():
        n = names.get(stat_key(k))
        if n:
            parts.append(f"{n} {val:g}" if isinstance(val, (int, float)) else n)
    return ", ".join(parts)


SKILL_REQ = re.compile(r"^Benötigt (.+?) \((\d+)\)$")


def meets_skill_requirements(tooltip: list[str], skills: dict[str, int]) -> bool:
    """False for tooltip lines like "Benötigt Ingenieurskunst (215)" the character does not meet."""
    for t in tooltip:
        m = SKILL_REQ.match(t)
        if m and skills.get(m.group(1), 0) < int(m.group(2)):
            return False
    return True


STAT_LABEL = {"stamina": "Ausdauer", "intellect": "Intelligenz", "spirit": "Willenskraft", "strength": "Stärke",
              "agility": "Beweglichkeit"}
SPELL_LABEL = {
    "spell_power": "Anlegen: Erhöht durch magische Zauber und Effekte verursachten Schaden und Heilung um bis zu {}.",
    "spell_damage_done": "Anlegen: Erhöht den von magischen Zaubern und Effekten verursachten Schaden um bis zu {}.",
    "shadow_damage_done": "Anlegen: Erhöht den von Schattenzaubern und -effekten verursachten Schaden um bis zu {}.",
    "spell_healing_done": "Anlegen: Erhöht durch Zauber und Effekte verursachte Heilung um bis zu {}.",
}


def tooltip(rec: dict, slot_name: str | None = None) -> dict:
    """Tooltip for the app: the game's own lines when the addon exported them (1.2+: with colours), otherwise
    rebuilt from the stats so older snapshots still show something useful."""
    out = {"name": rec.get("name"), "quality": rec.get("quality"), "ilvl": rec.get("ilvl")}
    if rec.get("tooltip"):
        out["lines"] = rec["tooltip"]
        out["colors"] = rec.get("tipColors")
        return out
    lines, colors, equip = [], [], []
    if slot_name or rec.get("subType"):
        lines.append(f"{slot_name or ''} | {rec.get('subType') or ''}".strip(" |"))
        colors.append("ffffff")
    stats = {stat_key(k): val for k, val in (rec.get("stats") or {}).items()}
    if stats.get("armor"):
        lines.append(f"{stats['armor']:g} Rüstung")
        colors.append("ffffff")
    if stats.get("damage_per_second"):
        lines.append(f"({stats['damage_per_second']:.1f} Schaden pro Sekunde)")
        colors.append("ffffff")
    for k, label in STAT_LABEL.items():
        if stats.get(k):
            lines.append(f"+{stats[k]:g} {label}")
            colors.append("ffffff")
    dur = rec.get("durability")
    if isinstance(dur, list) and len(dur) == 2:
        lines.append(f"Haltbarkeit {dur[0]} / {dur[1]}")
        colors.append("ffffff")
    if rec.get("reqLevel"):
        lines.append(f"Benötigt Stufe {rec['reqLevel']}")
        colors.append("ffffff")
    for k, text in SPELL_LABEL.items():
        if stats.get(k):
            equip.append(text.format(f"{stats[k]:g}"))
    lines += equip
    colors += ["1eff00"] * len(equip)
    out.update(lines=lines, colors=colors, rebuilt=True)
    return out


def money(c: int) -> str:
    return f"{c // 10000}g {c // 100 % 100:02d}s" if c >= 10000 else f"{c // 100}s {c % 100:02d}c"


def load_item_export(wow: str, char: dict) -> dict:
    """Item export of "/wowmcp items": account-wide WoWMCP_Items (addon 1.1+), else the older per-character copy."""
    best: dict = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        data = load_sv(path, "WoWMCP_Items") or {}
        if (data.get("updated") or 0) > (best.get("updated") or 0):
            best = data
    if not best.get("items"):
        best = char.get("itemInfo") or {}
    return best


def compute(wow: str = v.DEFAULT_WOW, char: str = "", level: int = 0, vorschau: int = 4, max_preis: float = 0,
            top: int = 3, prices: dict | None = None) -> dict:
    """Upgrade candidates per slot as plain data (copper prices) for the CLI and the app."""
    prices = prices if prices is not None else v.load_prices(v.find_auctionator(wow))
    last = max(int(k) for e in prices.values() if isinstance(e, dict) for k in (e.get("a") or {}))
    files = v.find_chars(wow, char)
    if not files:
        raise FileNotFoundError("Keine WoWMCP.lua für diesen Charakter gefunden.")
    c = load_char(files[0])
    info = load_item_export(wow, c)
    items = {int(k): x for k, x in (info.get("items") or {}).items()}
    if not items:
        raise LookupError("Kein Item-Export gefunden: im Spiel /wowmcp items, auf die Meldung warten, dann /reload.")
    level = level or (c.get("character") or {}).get("level") or 60
    gold = (c.get("character") or {}).get("money") or 0
    budget = int(max_preis * 10000) if max_preis else gold

    equipped = {e["slot"]: e for e in (c.get("equipment") or [])}
    skills = {s["name"]: s.get("rank") or 0 for s in ((c.get("skills") or {}).get("skills") or [])
              if isinstance(s, dict) and s.get("name")}

    def cur_score(slot: int) -> float:
        e = equipped.get(slot)
        return score(e.get("stats"), wand=slot == 18) if e else 0.0

    def item_out(iid: int, it: dict, price: int, extra: list[str], gain: float | None = None) -> dict:
        return dict(id=iid, name=it.get("name"), quality=it.get("quality"), reqLevel=it.get("reqLevel") or 0,
                    price=price, gain=round(gain, 1) if gain is not None else None, stats=short(it.get("stats")),
                    isNew=iid >= NEW_ID, soon=(it.get("reqLevel") or 0) > level, effects=extra,
                    tip=tooltip(it))

    rows, review = [], []
    for iid, it in items.items():
        slots = SLOT_OF.get(it.get("equipLoc") or "")
        req = it.get("reqLevel") or 0
        if not slots or req > level + vorschau or (it.get("quality") or 0) < 2:
            continue
        if it.get("classID") == 2 and it.get("subclassID") not in USABLE_WEAPONS:
            continue
        if it.get("classID") == 4 and it.get("subclassID") not in (0, 1):
            continue
        tip = it.get("tooltip") or []
        if any(t.startswith("Klassen:") and "Hexenmeister" not in t for t in tip):
            continue
        if not meets_skill_requirements(tip, skills):
            continue
        e = prices.get(str(iid))
        if not isinstance(e, dict) or not e.get("m"):
            continue
        if not any(int(d) >= last - 3 for d in (e.get("a") or {})):
            continue
        price = e["m"]
        if price > budget:
            continue
        new = score(it.get("stats"), wand=slots == [18])
        if slots == [16, 17]:
            old, slot = cur_score(16) + cur_score(17), 16
        elif len(slots) == 2:
            slot = min(slots, key=cur_score)
            old = cur_score(slot)
        else:
            slot, old = slots[0], cur_score(slots[0])
        # Forever writes spell power as "Anlegen:" lines too; those are already in stats. Keep the rest.
        extra = [t for t in tip if t.startswith(("Anlegen:", "Benutzen:", "Trefferchance:"))
                 and not any(p in t for p in STANDARD_EFFECTS)]
        if slot in (13, 14) or (extra and new - old < 5):
            if extra and req >= level - 15:
                review.append(dict(slot=SLOT_NAME[slot], **item_out(iid, it, price, extra)))
            continue
        gain = new - old
        if gain > 0.5:
            rows.append((slot, gain, price, iid, it, extra))

    slots_out = []
    for slot in sorted({r[0] for r in rows}):
        cur = equipped.get(slot)
        best = sorted((r for r in rows if r[0] == slot), key=lambda r: (-r[1], r[2]))[:top]
        slots_out.append(dict(
            slot=slot, slotName=SLOT_NAME[slot],
            current=dict(name=cur.get("name"), id=cur.get("id"), quality=cur.get("quality"),
                         stats=short(cur.get("stats")), tip=tooltip(cur, SLOT_NAME[slot])) if cur else None,
            candidates=[item_out(iid, it, price, extra, gain) for _, gain, price, iid, it, extra in best]))
    review.sort(key=lambda r: r["price"])
    return dict(
        scanDate=(datetime.date(2020, 1, 1) + datetime.timedelta(last)).isoformat(),
        exportUpdated=info.get("updated"), exportCount=len(items), exportMissing=len(info.get("missing") or []),
        level=level, budget=budget, character=os.path.basename(os.path.dirname(os.path.dirname(files[0]))),
        weights="Schatten/Zaubermacht 1, Ausdauer 0.5, Int 0.35, Willenskraft 0.25, Treffer% 10, Krit% 5",
        slots=slots_out, review=review[:15])


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--wow", default=v.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--level", type=int, default=0, help="Stufe überschreiben (Standard: aus dem Export)")
    ap.add_argument("--vorschau", type=int, default=4, help="auch Items bis Stufe + N zeigen")
    ap.add_argument("--max-preis", type=float, default=0, help="Gold-Obergrenze (Standard: eigenes Gold)")
    ap.add_argument("--top", type=int, default=3, help="Vorschläge pro Platz")
    ap.add_argument("--json", action="store_true", help="Ergebnis als JSON ausgeben (für die App)")
    a = ap.parse_args()

    try:
        r = compute(a.wow, a.char, a.level, a.vorschau, a.max_preis, a.top)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    if a.json:
        import json
        print(json.dumps(r, ensure_ascii=False))
        return

    when = datetime.datetime.fromtimestamp(r["exportUpdated"] or 0)
    print(f"AH-Scan: {r['scanDate']} | Item-Export: {when:%Y-%m-%d %H:%M} | Stufe {r['level']} | "
          f"Budget {money(r['budget'])} | {r['exportCount']} Items, {r['exportMissing']} nicht geladen")
    print(f"Gewichte: {r['weights']}; Preise = Mindest-Sofortkauf beim Scan.\n")
    for s in r["slots"]:
        cur = s["current"]
        print(f"== {s['slotName']}: jetzt {cur['name'] if cur else '-'} ({cur['stats'] if cur else ''})")
        for c in s["candidates"]:
            flag = " [NEU in Forever]" if c["isNew"] else ""
            soon = f" (ab Stufe {c['reqLevel']})" if c["soon"] else ""
            print(f"   +{c['gain']:5.1f}  {money(c['price']):>9}  {c['name']}{flag}{soon}  [{c['stats']}]  id {c['id']}")
            for t in c["effects"]:
                print(f"            {t}")
    if r["review"]:
        print("\n== Mit Spezialeffekt, bitte selbst ansehen")
        for x in r["review"]:
            print(f"   {x['slot']:10} {money(x['price']):>9}  {x['name']}: {' / '.join(x['effects'])[:160]}")


if __name__ == "__main__":
    main()
