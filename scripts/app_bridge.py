# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0", "pillow>=10"]
# ///
"""JSON bridge between the desktop app (app/) and the scripts in this folder.

    uv run scripts/app_bridge.py status
    uv run scripts/app_bridge.py character --char Malgruk
    uv run scripts/app_bridge.py writs --char Malgruk [--top 3] [--besitz-zum-ah-preis]
    uv run scripts/app_bridge.py upgrades --char Malgruk [--max-preis 1] [--vorschau 4] [--top 3]
                                          [--profil leveln|dungeon] [--sort gewinn|preis]
    uv run scripts/app_bridge.py recipes --char Malgruk [--beruf Alchimie]
    uv run scripts/app_bridge.py icons --ids 9492,287004

Prints exactly one JSON object on stdout; failures come back as {"error": "..."} with exit code 0,
so the app can show the message.
"""

from __future__ import annotations

import argparse
import datetime
import glob
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import recipes  # noqa: E402
import upgrades  # noqa: E402
import verfuegungen as v  # noqa: E402


def char_key(path: str) -> str:
    return os.path.basename(os.path.dirname(os.path.dirname(path)))


def status(wow: str) -> dict:
    out: dict = {"wowDir": wow, "wowFound": os.path.isdir(wow), "characters": [], "auctionator": None,
                 "itemExport": None, "lastRequest": None}
    if not out["wowFound"]:
        return out
    try:
        path = v.find_auctionator(wow)
        prices = v.load_prices(path)
        sd = v.scan_date(prices)
        out["auctionator"] = {"file": path, "modified": os.path.getmtime(path), "items": len(prices),
                              "scanDate": sd.isoformat() if sd else None}
    except (FileNotFoundError, SystemExit, ValueError) as e:
        out["auctionator"] = {"error": str(e)}
    for path in v.find_chars(wow):
        try:
            c = upgrades.load_char(path)
        except Exception as e:  # a half-written file while WoW is saving
            out["characters"].append({"key": char_key(path), "error": str(e), "modified": os.path.getmtime(path)})
            continue
        ch = c.get("character") or {}
        out["characters"].append({
            "key": char_key(path), "name": ch.get("name"), "surname": ch.get("surname"), "level": ch.get("level"),
            "class": ch.get("class"), "classFile": ch.get("classFile"), "race": ch.get("race"),
            "money": ch.get("money"), "zone": ch.get("zone"), "updated": c.get("updated"),
            "modified": os.path.getmtime(path), "addonVersion": (c.get("addon") or {}).get("version"),
            "client": c.get("client"), "errors": c.get("errors") or {},
            "bankScanned": (c.get("bank") or {}).get("updated"),
        })
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        items = upgrades.load_sv(path, "WoWMCP_Items") or {}
        account = upgrades.load_sv(path, "WoWMCP_Account") or {}
        out["itemExport"] = {"updated": items.get("updated"), "count": len(items.get("items") or {}),
                             "missing": len(items.get("missing") or []), "build": items.get("build"),
                             "lastRun": items.get("lastRun"), "auto": account.get("autoItems")}
        out["lastRequest"] = account.get("lastRequest")
    if not out["itemExport"]:
        # Addon 1.0 kept the export per character.
        for path in v.find_chars(wow):
            info = (upgrades.load_char(path).get("itemInfo") or {})
            if info.get("items"):
                out["itemExport"] = {"updated": info.get("updated"), "count": len(info["items"]),
                                     "missing": len(info.get("missing") or []), "legacy": True}
                break
    return out


def character(wow: str, name: str) -> dict:
    files = v.find_chars(wow, name)
    if not files:
        raise FileNotFoundError(f"Kein WoWMCP-Export für „{name}“ gefunden.")
    c = upgrades.load_char(files[0])
    skills = c.get("skills") or {}
    talents = c.get("talents") or {}
    # Points per item: what the set loses without it, on the same scale as the AH upgrades.
    eq, totals = upgrades.equipped_stats(c)
    scale = upgrades.build_scale(c, gear_hit=totals.get("hit", 0), gear_crit=totals.get("crit", 0))
    full = upgrades.set_value(totals, scale)
    equipment = []
    for e in c.get("equipment") or []:
        rest = dict(totals)
        upgrades.add_stats(rest, eq.get(e.get("slot"), {}), -1)
        equipment.append({"slot": e.get("slot"), "slotName": upgrades.SLOT_NAME.get(e.get("slot"), str(e.get("slot"))),
                          "id": e.get("id"), "name": e.get("name"), "quality": e.get("quality"), "ilvl": e.get("ilvl"),
                          "stats": upgrades.short(e.get("stats")), "durability": e.get("durability"),
                          "score": round(full - upgrades.set_value(rest, scale), 1),
                          "tip": upgrades.tooltip(e, upgrades.SLOT_NAME.get(e.get("slot")))})
    return {"key": char_key(files[0]), "updated": c.get("updated"), "character": c.get("character") or {},
            "equipment": sorted(equipment, key=lambda e: e["slot"] or 0),
            "weights": scale["text"], "profileLabel": scale["label"], "setValue": round(full, 1),
            "professions": skills.get("professions") or [], "talents": talents.get("class") or [],
            "legacy": talents.get("legacy") or [], "errors": c.get("errors") or {}}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("command", choices=["status", "character", "writs", "upgrades", "recipes", "icons"])
    ap.add_argument("--wow", default=os.environ.get("WOW_FOREVER_DIR") or v.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--top", type=int, default=3)
    ap.add_argument("--besitz-zum-ah-preis", action="store_true")
    ap.add_argument("--max-preis", type=float, default=0)
    ap.add_argument("--vorschau", type=int, default=4)
    ap.add_argument("--profil", choices=["", *sorted(upgrades.PROFILES)], default="")
    ap.add_argument("--sort", choices=["gewinn", "preis"], default="gewinn")
    ap.add_argument("--beruf", default="", help="recipes: part of the profession name")
    ap.add_argument("--ids", default="", help="icons: comma-separated item IDs")
    a = ap.parse_args()
    try:
        if a.command == "status":
            r = status(a.wow)
        elif a.command == "character":
            r = character(a.wow, a.char)
        elif a.command == "icons":
            import icons  # pillow is only needed here

            r = icons.resolve(a.wow, [int(i) for i in a.ids.split(",") if i.strip().isdigit()])
        elif a.command == "recipes":
            r = recipes.compute(a.wow, a.char, a.beruf)
        elif a.command == "writs":
            r = v.compute(a.wow, a.char, a.top, a.besitz_zum_ah_preis)
        else:
            r = upgrades.compute(a.wow, a.char, 0, a.vorschau, a.max_preis, a.top, profil=a.profil, sort=a.sort)
        r["generated"] = datetime.datetime.now().timestamp()
    except (FileNotFoundError, LookupError, ValueError) as e:
        r = {"error": str(e)}
    except SystemExit as e:  # helpers that still call sys.exit(message)
        r = {"error": str(e.code)}
    sys.stdout.reconfigure(encoding="utf-8")
    print(json.dumps(r, ensure_ascii=False))


if __name__ == "__main__":
    main()
