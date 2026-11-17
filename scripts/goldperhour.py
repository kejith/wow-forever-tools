# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""GoldPerHour sessions for the desktop app: reads GoldPerHourDB from the account SavedVariables.

    uv run scripts/goldperhour.py [--wow <pfad>]

The addon (addon/GoldPerHour, from 0.3) keeps the running session in GoldPerHourDB.gold/coin/items/elapsed,
its metadata (start, zones, characters, gold curve) in .session and finished sessions in .history. WoW
writes the file only on /reload or logout, so everything here is the state at that moment.
"""

from __future__ import annotations

import glob
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import upgrades  # noqa: E402  (load_sv)
import verfuegungen as v  # noqa: E402  (DEFAULT_WOW)

CATEGORIES = ("herb", "loot")
# Same defaults as DEFAULTS in GoldPerHour.lua.
DEFAULTS = {"scale": 1, "tooltipItems": 6, "priceSource": "auctionator", "resumeAfterReload": True,
            "autoStart": False}
HERB_GATHERING = 2366
LINK = re.compile(r"\|Hitem:(\d+)[^|]*\|h\[(.*?)\]\|h")
COLOR = re.compile(r"\|c[fF]{2}([0-9a-fA-F]{6})")
QUALITY_BY_COLOR = {"9d9d9d": 0, "ffffff": 1, "1eff00": 2, "0070dd": 3, "a335ee": 4, "ff8000": 5}


def values(x) -> list:
    """Lua tables come back as a list (keys 1..n) or a dict (sparse keys, or empty)."""
    if isinstance(x, list):
        return x
    if isinstance(x, dict):
        return list(x.values())
    return []


def keys_true(x) -> list:
    if isinstance(x, list):
        return [i + 1 for i, val in enumerate(x) if val]
    if isinstance(x, dict):
        return [k for k, val in x.items() if val]
    return []


def per_hour(copper: float, elapsed: float) -> float | None:
    return copper / elapsed * 3600 if elapsed and elapsed >= 60 else None


def item_entry(e: dict) -> dict:
    link = e.get("link") or ""
    m = LINK.search(link)
    color = COLOR.search(link)
    q = e.get("quality")
    if q is None and color:
        q = QUALITY_BY_COLOR.get(color.group(1).lower())
    return {"id": e.get("id") or (int(m.group(1)) if m else None), "name": e.get("name") or (m.group(2) if m else "?"),
            "quality": q, "count": e.get("count") or 0, "value": round(e.get("value") or 0)}


def zone_list(zones) -> list:
    if not isinstance(zones, dict):
        return []
    return sorted(({"zone": z, "value": round(c)} for z, c in zones.items()), key=lambda z: -z["value"])


def samples(raw) -> list:
    out = []
    for s in values(raw):
        s = values(s)
        if len(s) >= 3:
            out.append({"t": s[0], "herb": s[1], "loot": s[2]})
    return out


def session_summary(gold: dict, coin: dict, items: dict, elapsed: float, meta: dict) -> dict:
    total = sum(gold.get(c, 0) for c in CATEGORIES)
    cats = {}
    for c in CATEGORIES:
        lst = sorted((item_entry(e) for e in values(items.get(c))), key=lambda e: (-e["value"], e["id"] or 0))
        cats[c] = {"gold": round(gold.get(c, 0)), "coin": round(coin.get(c, 0)), "perHour": per_hour(gold.get(c, 0), elapsed),
                   "items": lst, "itemCount": sum(e["count"] for e in lst)}
    return {"elapsed": round(elapsed), "total": round(total), "perHour": per_hour(total, elapsed), "categories": cats,
            "started": meta.get("started"), "ended": meta.get("ended") or meta.get("lastActive"),
            "zones": zone_list(meta.get("zones")), "chars": sorted(keys_true(meta.get("chars"))),
            "samples": samples(meta.get("samples"))}


def settings_of(db: dict) -> dict:
    s = dict(DEFAULTS)
    for k, val in (db.get("settings") or {}).items():
        if k in DEFAULTS and val is not None:
            s[k] = val
    s["shown"] = not db.get("hidden")
    s["herbSpells"] = sorted(int(k) for k in keys_true(db.get("herbSpells")))
    return s


def find_file(wow: str) -> str | None:
    files = glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "GoldPerHour.lua"))
    return max(files, key=os.path.getmtime) if files else None


def from_db(db: dict) -> dict:
    meta = db.get("session") or {}
    current = session_summary(db.get("gold") or {}, db.get("coin") or {}, db.get("items") or {},
                              db.get("elapsed") or 0, meta)
    current["runningAtSave"] = bool(db.get("resume"))
    history = []
    for h in values(db.get("history")):
        s = session_summary(h.get("gold") or {}, h.get("coin") or {}, h.get("items") or {}, h.get("elapsed") or 0, h)
        s["archived"] = h.get("archived")
        history.append(s)
    history.sort(key=lambda s: -(s["started"] or s["archived"] or 0))
    rated = [s for s in history if s["perHour"] is not None]
    total_time = sum(s["elapsed"] for s in history)
    total_gold = sum(s["total"] for s in history)
    return {
        "found": True, "version": db.get("version"), "updated": db.get("updated"),
        "current": current, "history": history, "settings": settings_of(db),
        "lastRequest": db.get("lastRequest"), "legacy": "settings" not in db,
        "summary": {"sessions": len(history), "elapsed": total_time, "total": total_gold,
                    "perHour": per_hour(total_gold, total_time),
                    "best": max(rated, key=lambda s: s["perHour"])["started"] if rated else None},
        "herbGathering": HERB_GATHERING,
    }


def compute(wow: str = v.DEFAULT_WOW) -> dict:
    path = find_file(wow)
    if not path:
        return {"found": False, "settings": settings_of({}), "herbGathering": HERB_GATHERING}
    db = upgrades.load_sv(path, "GoldPerHourDB") or {}
    out = from_db(db)
    out["file"] = path
    out["modified"] = os.path.getmtime(path)
    return out


if __name__ == "__main__":
    import argparse

    ap = argparse.ArgumentParser()
    ap.add_argument("--wow", default=os.environ.get("WOW_FOREVER_DIR") or v.DEFAULT_WOW)
    a = ap.parse_args()
    sys.stdout.reconfigure(encoding="utf-8")
    print(json.dumps(compute(a.wow), ensure_ascii=False, indent=2))
