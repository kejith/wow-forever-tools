# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Price history of every item in Auctionator's price database, for the desktop app.

    uv run scripts/pricehistory.py [--item 2447] [--json]

Auctionator keeps per item and day the lowest (l) and the highest (h) minimum buyout it saw, the most
offers (a) and the last minimum price (m); days count from 2020-01-01 and it keeps 21 of them by default.
Every item it has is listed, also those only seen in a single search.

Names come from the DB2 table ItemSparse in the client's language, then from the addon's item export
(WoWMCP_Items), then from the bags and banks of the exported characters. ItemSparse on wago.tools lacks
many Forever items (presumably encrypted); the item export has most of the equipment among them.
"""

from __future__ import annotations

import argparse
import collections
import datetime
import glob
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import i18n  # noqa: E402
import icons  # noqa: E402  (DB2 download and build)
import upgrades  # noqa: E402  (reads SavedVariables)
import writs  # noqa: E402  (Auctionator)
from i18n import tr  # noqa: E402

DAY0 = datetime.date(2020, 1, 1)  # Auctionator.Constants.SCAN_DAY_0
NAMES_VERSION = 1
FULL_SCAN_SHARE = 0.3  # a day with at least this share of the items of the biggest day was a full scan


def day_date(day: int) -> datetime.date:
    return DAY0 + datetime.timedelta(day)


def load_names(build: str | None, locale: str) -> dict[str, list]:
    """Item ID (as text) -> [name or None, quality, class ID] from ItemSparse and Item, cached as
    scripts/cache/itemnames-<build>-<locale>.json. Without a build the newest cached copy is used."""
    path = os.path.join(icons.CACHE, f"itemnames-{build}-{locale}.json") if build else None
    if path and os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if data.get("version") == NAMES_VERSION:
            return data["items"]
    if not build:
        for pattern in (f"itemnames-*-{locale}.json", "itemnames-*.json"):
            cached = sorted(glob.glob(os.path.join(icons.CACHE, pattern)), key=os.path.getmtime)
            if cached:
                with open(cached[-1], encoding="utf-8") as f:
                    return json.load(f).get("items") or {}
    items: dict[str, list] = {}
    for r in icons.table("Item", build):
        items[r["ID"]] = [None, None, int(r["ClassID"] or 0)]
    for r in icons.table("ItemSparse", build, locale):
        entry = items.setdefault(r["ID"], [None, None, None])
        entry[0] = r.get("Display_lang") or None
        entry[1] = int(r["OverallQualityID"]) if (r.get("OverallQualityID") or "").isdigit() else None
    if path and items:
        os.makedirs(icons.CACHE, exist_ok=True)
        part = f"{path}.{os.getpid()}.part"
        with open(part, "w", encoding="utf-8") as f:
            json.dump({"version": NAMES_VERSION, "items": items}, f, ensure_ascii=False)
        os.replace(part, path)
    return items


def known_items(wow: str, locale: str) -> dict[int, dict]:
    """Name and quality the addon saw in the game: the item export, then bags and banks."""
    out: dict[int, dict] = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        store = upgrades.load_sv(path, "WoWMCP_Items") or {}
        if store.get("locale") and store["locale"] != locale:
            continue  # names of another client language
        for iid, rec in (store.get("items") or {}).items():
            if isinstance(rec, dict) and rec.get("name"):
                out[int(iid)] = {"name": rec["name"], "q": rec.get("quality"), "cls": rec.get("classID")}
    for path in writs.find_chars(wow):
        try:
            c = upgrades.load_char(path)
        except Exception:  # a half-written file while WoW is saving
            continue
        if ((c.get("client") or {}).get("locale") or locale) != locale:
            continue
        bags = list(c.get("bags") or []) + list((c.get("bank") or {}).get("containers") or [])
        for bag in bags:
            for it in (bag or {}).get("items") or []:
                if it.get("id") and it.get("name") and it["id"] not in out:
                    out[it["id"]] = {"name": it["name"], "q": it.get("quality"), "cls": None}
    return out


def days_of(entry: dict) -> list[list]:
    """[[day, lowest, highest minimum price, offers or None], ...], oldest first."""
    highs = entry.get("h") if isinstance(entry.get("h"), dict) else {}
    lows = entry.get("l") if isinstance(entry.get("l"), dict) else {}
    offers = entry.get("a") if isinstance(entry.get("a"), dict) else {}
    out = []
    for key, high in highs.items():
        if not str(key).isdigit() or not isinstance(high, (int, float)) or high <= 0:
            continue
        low = lows.get(key)
        low = low if isinstance(low, (int, float)) and 0 < low <= high else high
        listed = offers.get(key)
        out.append([int(key), round(low), round(high), int(listed) if isinstance(listed, (int, float)) else None])
    return sorted(out)


def compute(wow: str = writs.DEFAULT_WOW, prices: dict | None = None) -> dict:
    path = None
    if prices is None:
        path = writs.find_auctionator(wow)
        prices = writs.load_prices(path)
    locale = i18n.client_locale(wow) or i18n.DEFAULT_LOCALE
    build = icons.client_build(wow)
    names = load_names(build, locale)
    seen = known_items(wow, locale)
    items, last_day, unnamed = [], None, 0
    for key, entry in prices.items():
        if not key.isdigit() or not isinstance(entry, dict):
            continue  # "version", and keys of other item kinds (battle pets "p:", suffixes "gr:")
        days = days_of(entry)
        if not days:
            continue
        iid = int(key)
        name, q, cls = (names.get(key) or [None, None, None])[:3]
        own = seen.get(iid) or {}
        name = name or own.get("name")
        q = q if q is not None else own.get("q")
        cls = cls if cls is not None else own.get("cls")
        if not name:
            unnamed += 1
        last_day = max(last_day or 0, days[-1][0])
        m = entry.get("m")
        items.append({"id": iid, "name": name, "q": q, "cls": cls,
                      "m": round(m) if isinstance(m, (int, float)) and m > 0 else days[-1][1], "days": days})
    if not items:
        raise LookupError(tr("No price data found in Auctionator.lua."))
    # Auctionator also notes the items of single searches; "the last scan" is the last day that has a good
    # share of the items of the biggest day (as recipes.py counts full scan days).
    per_day = collections.Counter(p[0] for it in items for p in it["days"])
    biggest = max(per_day.values())
    last_scan = max(day for day, n in per_day.items() if n >= FULL_SCAN_SHARE * biggest)
    items.sort(key=lambda it: (not it["name"], (it["name"] or "").lower(), it["id"]))
    return {"day0": DAY0.isoformat(), "lastDay": last_day, "lastDate": day_date(last_day).isoformat(),
            "firstDay": min(it["days"][0][0] for it in items), "lastScanDay": last_scan,
            "lastScanDate": day_date(last_scan).isoformat(), "items": items, "count": len(items),
            "unnamed": unnamed, "locale": locale, "modified": os.path.getmtime(path) if path else None}


def main() -> None:
    ap = argparse.ArgumentParser(description="Price history of the items in Auctionator's database.")
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--item", type=int, default=0, help="only this item ID")
    ap.add_argument("--json", action="store_true")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    r = compute(a.wow)
    if a.item:
        r["items"] = [it for it in r["items"] if it["id"] == a.item]
    if a.json:
        sys.stdout.reconfigure(encoding="utf-8")
        print(json.dumps(r, ensure_ascii=False))
        return
    print(tr("{n} items, {unnamed} without a name, last scan day {date}", n=r["count"], unnamed=r["unnamed"],
             date=r["lastScanDate"]))
    for it in r["items"][:50] if not a.item else r["items"]:
        print(f"{it['id']:>7}  {it['name'] or '?'}")
        if a.item:
            for day, low, high, listed in it["days"]:
                print(f"         {day_date(day).isoformat()}  {low:>9}  {high:>9}  {listed if listed is not None else ''}")


if __name__ == "__main__":
    main()
