# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0", "pillow>=10"]
# ///
"""JSON bridge between the desktop app (app/) and the scripts in this folder.

    uv run scripts/app_bridge.py status
    uv run scripts/app_bridge.py character --char Malgruk
    uv run scripts/app_bridge.py writs --char Malgruk [--top 3] [--owned-at-ah-price]
    uv run scripts/app_bridge.py upgrades --char Malgruk [--max-price 1] [--preview 4] [--top 3]
                                          [--profile leveling|dungeon] [--sort gain|price]
    uv run scripts/app_bridge.py recipes --char Malgruk [--profession Alchemy]
    uv run scripts/app_bridge.py consumables --char Malgruk [--top 5]
    uv run scripts/app_bridge.py icons --ids 9492,287004
    uv run scripts/app_bridge.py goldperhour
    uv run scripts/app_bridge.py pricehistory
    uv run scripts/app_bridge.py ingame [--write]

Every command takes --lang en|de for the texts the scripts produce (notes, labels, error messages); the
default is the language of the game client. Prints exactly one JSON object on stdout; failures come back
as {"error": "..."} with exit code 0, so the app can show the message.
"""

from __future__ import annotations

import argparse
import datetime
import glob
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import consumables  # noqa: E402
import goldperhour  # noqa: E402
import i18n  # noqa: E402
import ingame  # noqa: E402
import pricehistory  # noqa: E402
import recipes  # noqa: E402
import upgrades  # noqa: E402
import writs  # noqa: E402
from i18n import tr  # noqa: E402


def char_key(path: str) -> str:
    return os.path.basename(os.path.dirname(os.path.dirname(path)))


def status(wow: str) -> dict:
    out: dict = {"wowDir": wow, "wowFound": os.path.isdir(wow), "characters": [], "auctionator": None,
                 "itemExport": None, "lastRequest": None, "clientLocale": i18n.client_locale(wow),
                 "language": i18n.language()}
    if not out["wowFound"]:
        return out
    try:
        path = writs.find_auctionator(wow)
        prices = writs.load_prices(path)
        sd = writs.scan_date(prices)
        out["auctionator"] = {"file": path, "modified": os.path.getmtime(path), "items": len(prices),
                              "scanDate": sd.isoformat() if sd else None}
    except (FileNotFoundError, LookupError, ValueError) as e:
        out["auctionator"] = {"error": str(e)}
    for path in writs.find_chars(wow):
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
        for path in writs.find_chars(wow):
            info = (upgrades.load_char(path).get("itemInfo") or {})
            if info.get("items"):
                out["itemExport"] = {"updated": info.get("updated"), "count": len(info["items"]),
                                     "missing": len(info.get("missing") or []), "legacy": True}
                break
    return out


def character(wow: str, name: str) -> dict:
    files = writs.find_chars(wow, name)
    if not files:
        raise FileNotFoundError(tr("No WoWMCP export found for \"{char}\".", char=name))
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
        slot = e.get("slot")
        label = upgrades.slot_name(slot) if slot in upgrades.SLOT_NAME else None
        equipment.append({"slot": slot, "slotName": label or str(slot),
                          "id": e.get("id"), "name": e.get("name"), "quality": e.get("quality"), "ilvl": e.get("ilvl"),
                          "stats": upgrades.short(e.get("stats")), "durability": e.get("durability"),
                          "score": round(full - upgrades.set_value(rest, scale), 1),
                          "tip": upgrades.tooltip(e, label)})
    return {"key": char_key(files[0]), "updated": c.get("updated"), "character": c.get("character") or {},
            "equipment": sorted(equipment, key=lambda e: e["slot"] or 0),
            "weights": scale["text"], "profileLabel": scale["label"], "setValue": round(full, 1),
            "professions": skills.get("professions") or [], "talents": talents.get("class") or [],
            "legacy": talents.get("legacy") or [], "errors": c.get("errors") or {}}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("command", choices=["status", "character", "writs", "upgrades", "recipes", "consumables", "icons",
                                           "goldperhour", "ingame", "pricehistory"])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--top", type=int, default=3)
    ap.add_argument("--owned-at-ah-price", action="store_true")
    ap.add_argument("--max-price", type=float, default=0)
    ap.add_argument("--preview", type=int, default=4)
    ap.add_argument("--profile", choices=["", *sorted(upgrades.PROFILES)], default="")
    ap.add_argument("--sort", choices=["gain", "price"], default="gain")
    ap.add_argument("--engine", choices=["auto", "on", "off"], default="auto",
                    help="upgrades: combat engine (auto falls back to the weight scale)")
    ap.add_argument("--fight-seconds", type=float, default=60)
    ap.add_argument("--target-delta", type=int, default=-1)
    ap.add_argument("--kills-per-minute", type=float, default=-1,
                    help="upgrades: killing blows per minute for the combat model (default: automatic)")
    ap.add_argument("--metric", choices=["", "dps", "hps", "ttoom"], default="",
                    help="upgrades: metric of the combat model (default: per class and spec)")
    ap.add_argument("--method", choices=["", "expectation", "montecarlo"], default="")
    ap.add_argument("--iterations", type=int, default=0)
    ap.add_argument("--profession", default="", help="recipes: part of the profession name or its skill line ID")
    ap.add_argument("--ids", default="", help="icons: comma-separated item IDs")
    ap.add_argument("--write", action="store_true", help="ingame: write the data file into the addon folder")
    i18n.add_argument(ap)
    a = ap.parse_args()
    language = i18n.set_language(a.lang, a.wow)
    try:
        if a.command == "status":
            r = status(a.wow)
        elif a.command == "character":
            r = character(a.wow, a.char)
        elif a.command == "icons":
            import icons  # pillow is only needed here

            r = icons.resolve(a.wow, [int(i) for i in a.ids.split(",") if i.strip().isdigit()])
        elif a.command == "goldperhour":
            r = goldperhour.compute(a.wow)
        elif a.command == "pricehistory":
            # Every item of Auctionator's database with its days; the window searches and draws them.
            r = pricehistory.compute(a.wow)
        elif a.command == "ingame":
            # What the addon shows in the game; the list titles are in the client's language.
            r = ingame.sync(a.wow, a.char, a.write)
        elif a.command == "recipes":
            r = recipes.compute(a.wow, a.char, a.profession)
        elif a.command == "consumables":
            # Every buff group; the window hides the ones that do not help the class.
            r = consumables.compute(a.wow, a.char, a.top, all_buffs=True)
        elif a.command == "writs":
            r = writs.compute(a.wow, a.char, a.top, a.owned_at_ah_price)
        else:
            r = upgrades.compute(a.wow, a.char, 0, a.preview, a.max_price, a.top, profile=a.profile, sort=a.sort,
                                 engine=a.engine, fight_seconds=a.fight_seconds, target_delta=a.target_delta,
                                 kills_per_minute=a.kills_per_minute, metric=a.metric, method=a.method, iterations=a.iterations)
        r["generated"] = datetime.datetime.now().timestamp()
        r["language"] = language
    except (FileNotFoundError, LookupError, ValueError, RuntimeError) as e:
        r = {"error": str(e)}
    except SystemExit as e:  # helpers that still call sys.exit(message)
        r = {"error": str(e.code)}
    sys.stdout.reconfigure(encoding="utf-8")
    print(json.dumps(r, ensure_ascii=False))


if __name__ == "__main__":
    main()
