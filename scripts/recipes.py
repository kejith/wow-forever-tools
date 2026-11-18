# /// script
# requires-python = ">=3.11"
# dependencies = ["lupa>=2.0"]
# ///
"""Profession recipes: what does every learned recipe earn in the auction house right now, and which recipes
would earn more?

    uv run scripts/recipes.py --char Malgruk [--profession Alchemy] [--top 15] [--json] [--lang en|de]

Sources:
- Recipe catalog from Forever's DB2 tables (wago.tools, product wowf): SkillLineAbility (recipes per
  profession, colour thresholds), SpellReagents, SpellEffect (crafted item, amount, the learn effect of recipe
  items), ItemEffect + ItemXItemEffect (which item teaches which recipe), ItemSparse (names, vendor prices,
  required skill, binding), SpellCooldowns (transmutes). Cached as JSON per build and client locale.
- Learned recipes from the addon (1.4+): WoWMCP_Char.recipes, read when the profession window opens.
  Without that export "learned" is unknown.
- Prices from Auctionator (lowest buyout at the last scan), vendor prices from WoWMCP_Account.vendor
  (read when you visit a vendor) or from ItemSparse for reagents every trade vendor sells.

Profit per craft = AH price x amount x (1 - 5% AH cut) - reagents. Reagents at the cheaper price of AH and
vendor; your own reagents count at the market price (what you would get for selling them).
"""

from __future__ import annotations

import argparse
import collections
import datetime
import glob
import json
import os
import statistics
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import i18n  # noqa: E402
import icons  # noqa: E402  (DB2 download and build)
import upgrades  # noqa: E402  (reads SavedVariables)
import writs  # noqa: E402  (Auctionator)
from i18n import tr  # noqa: E402

AH_CUT = 0.05
CATALOG_VERSION = 2
HISTORY_DAYS = 8  # the profit history shows this many full scan days
FULL_SCAN_SHARE = 0.3  # a day counts as a full scan when it holds this share of the biggest scan day
GATHERING = {182, 186, 393, 356}  # Herbalism, Mining, Skinning, Fishing: nothing to craft
NEW_ID = 200000

# Reagents every trade or general goods vendor sells. Price = ItemSparse.BuyPrice / VendorStackCount, until
# the addon reads the real price at a vendor.
VENDOR_ITEMS = {
    3371, 3372, 8925, 18256,  # vials
    2678, 2692, 3713, 159, 1179, 2596, 4289, 8150,  # cooking: spices, water, milk, beer, salt
    2320, 2321, 4291, 8343, 14341,  # threads
    2324, 2604, 6260, 4340, 4341, 4342, 2325, 10290,  # bleach and dyes
    3857, 2880, 3466, 18567,  # coal, flux
    6217, 4470, 4399, 4400, 10647, 10648,  # copper rod, wood, kegs, gelatin, parchment
    6530,  # nightcrawlers
}


def _int(x) -> int:
    try:
        return int(float(x or 0))
    except ValueError:
        return 0


# ---------- catalog ----------

def build_catalog(build: str | None, locale: str | None = None) -> dict:
    """{skillLine: {spellId: recipe}} plus item info, from the DB2 tables. Names are in `locale`."""
    t = lambda name: icons.table(name, build, locale)  # noqa: E731
    abilities = [r for r in t("SkillLineAbility") if _int(r["SkillLine"]) and _int(r["Spell"])]
    spells = {_int(r["Spell"]) for r in abilities}
    names = {_int(r["ID"]): r["Name_lang"] for r in t("SpellName") if _int(r["ID"]) in spells}
    reagents = {}
    for r in t("SpellReagents"):
        sid = _int(r["SpellID"])
        if sid in spells:
            reagents[sid] = [[_int(r[f"Reagent_{i}"]), _int(r[f"ReagentCount_{i}"])]
                             for i in range(8) if _int(r[f"Reagent_{i}"]) > 0 and _int(r[f"ReagentCount_{i}"]) > 0]
    creates, teaches = {}, {}
    for r in t("SpellEffect"):
        sid, effect = _int(r["SpellID"]), _int(r["Effect"])
        if effect == 24 and sid in spells and _int(r["EffectItemType"]):  # CREATE_ITEM
            creates[sid] = [_int(r["EffectItemType"]), max(1, _int(r.get("EffectBasePointsF")))]
        elif effect == 36 and _int(r["EffectTriggerSpell"]) in spells:  # LEARN_SPELL (recipe item)
            teaches[sid] = _int(r["EffectTriggerSpell"])
    effect_spell = {_int(r["ID"]): _int(r["SpellID"]) for r in t("ItemEffect")}
    recipe_items: dict[int, list[int]] = {}
    for r in t("ItemXItemEffect"):
        sp = effect_spell.get(_int(r["ItemEffectID"]))
        # Recipe items teach through a learn effect or carry the recipe spell itself (as in Classic).
        target = teaches.get(sp) or (sp if sp in spells else None)
        if target:
            recipe_items.setdefault(target, []).append(_int(r["ItemID"]))
    cooldown = {}
    for r in t("SpellCooldowns"):
        sid = _int(r["SpellID"])
        if sid in spells:
            ms = max(_int(r["CategoryRecoveryTime"]), _int(r["RecoveryTime"]))
            if ms >= 600000:
                cooldown[sid] = ms // 1000

    lines: dict[str, dict] = {}
    for r in abilities:
        sid, line = _int(r["Spell"]), _int(r["SkillLine"])
        if sid not in creates or sid not in reagents or not names.get(sid):
            continue
        lines.setdefault(str(line), {})[str(sid)] = {
            "name": names[sid], "output": creates[sid][0], "qty": creates[sid][1], "reagents": reagents[sid],
            "yellow": _int(r["TrivialSkillLineRankLow"]), "grey": _int(r["TrivialSkillLineRankHigh"]),
            "acquire": _int(r["AcquireMethod"]), "recipeItems": sorted(set(recipe_items.get(sid, []))),
            "cooldown": cooldown.get(sid),
        }
    wanted = set()
    for recipes in lines.values():
        for rec in recipes.values():
            wanted.update([rec["output"], *rec["recipeItems"], *(i for i, _ in rec["reagents"])])
    items = {}
    for r in t("ItemSparse"):
        iid = _int(r["ID"])
        if iid in wanted:
            items[str(iid)] = {"name": r["Display_lang"] or None, "quality": _int(r["OverallQualityID"]),
                               "buy": _int(r["BuyPrice"]), "buyStack": max(1, _int(r["VendorStackCount"])),
                               "sell": _int(r["SellPrice"]), "bind": _int(r["Bonding"]),
                               "reqSkill": _int(r["RequiredSkillRank"]), "reqSkillLine": _int(r["RequiredSkill"])}
    return {"version": CATALOG_VERSION, "build": build, "locale": locale, "lines": lines, "items": items}


def load_catalog(build: str | None, locale: str | None = None) -> dict:
    """Catalog for a build in the client's locale, from the cache or built from the DB2 tables. Without a
    build the newest cached catalog is used (the same locale first)."""
    locale = locale or i18n.DEFAULT_LOCALE
    path = os.path.join(icons.CACHE, f"recipes-{build}-{locale}.json") if build else None
    if path and os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        if data.get("version") == CATALOG_VERSION:
            return data
    if not build:
        for pattern in (f"recipes-*-{locale}.json", "recipes-*.json"):
            cached = sorted(glob.glob(os.path.join(icons.CACHE, pattern)), key=os.path.getmtime)
            if cached:
                with open(cached[-1], encoding="utf-8") as f:
                    return json.load(f)
    data = build_catalog(build, locale)
    if path and data["lines"]:
        os.makedirs(icons.CACHE, exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False)
    return data


# ---------- prices ----------

class Market:
    def __init__(self, prices: dict, vendor: dict, items: dict):
        self.prices, self.vendor, self.items = prices, vendor, items
        days = collections.Counter(int(k) for e in prices.values() if isinstance(e, dict) for k in (e.get("a") or {}))
        self.last_day = max(days) if days else 0
        # Auctionator also writes day prices for a single search. Only full scans count for the history.
        biggest = max(days.values(), default=0)
        self.scan_days = sorted(d for d, n in days.items() if n >= FULL_SCAN_SHARE * biggest)[-HISTORY_DAYS:]

    def ah(self, iid: int) -> dict | None:
        """Lowest price at the last scan, amount listed, median of the day prices of the last 7 scan days."""
        e = self.prices.get(str(iid))
        if not isinstance(e, dict) or not e.get("m"):
            return None
        avail = e.get("a") or {}
        day = max((int(d) for d in avail), default=0)
        recent = [p for d, p in (e.get("h") or {}).items() if int(d) > self.last_day - 7 and p]
        return {"price": e["m"], "listed": avail.get(str(day), 0), "age": self.last_day - day if day else None,
                "median": int(statistics.median(recent)) if recent else e["m"]}

    def day_price(self, iid: int, day: int) -> float | None:
        """AH price on a scan day: the middle of the day's highest and lowest minimum price (Auctionator
        stores only these two), on the last scan day the current minimum price as in ah()."""
        e = self.prices.get(str(iid))
        if not isinstance(e, dict):
            return None
        high = (e.get("h") or {}).get(str(day))
        if not high:
            return None
        if day == self.last_day and e.get("m"):
            return e["m"]
        low = (e.get("l") or {}).get(str(day))
        return (high + low) / 2 if low else high

    def day_listed(self, iid: int, day: int) -> int:
        e = self.prices.get(str(iid))
        return ((e.get("a") or {}).get(str(day)) or 0) if isinstance(e, dict) else 0

    def vendor_price(self, iid: int) -> tuple[float | None, str | None]:
        """(price per piece, vendor NPC or None) when a vendor sells the item without a stock limit."""
        seen = self.vendor.get(iid) or self.vendor.get(str(iid))
        if isinstance(seen, dict) and seen.get("price") and not seen.get("limited"):
            return seen["price"], seen.get("npc")
        it = self.items.get(str(iid)) or {}
        if iid in VENDOR_ITEMS and it.get("buy"):
            return it["buy"] / it.get("buyStack", 1), None
        return None, None

    def reagent(self, iid: int) -> dict:
        """Cheapest way to buy: "ah" or "vendor" (with the vendor's name when the addon saw one)."""
        ah = self.ah(iid)
        vp, npc = self.vendor_price(iid)
        options = []
        if ah:
            options.append((ah["price"], "ah", None))
        if vp:
            options.append((vp, "vendor", npc))
        if not options:
            return {"unit": None, "source": None, "vendor": None, "listed": 0}
        unit, source, npc = min(options, key=lambda o: o[0])
        return {"unit": unit, "source": source, "vendor": npc, "listed": ah["listed"] if ah else 0}


# ---------- character ----------

def owned_counts(c: dict) -> dict[int, int]:
    out: dict[int, int] = {}
    containers = list(c.get("bags") or []) + list((c.get("bank") or {}).get("containers") or [])
    for bag in containers:
        for it in (bag or {}).get("items") or []:
            if it.get("id"):
                out[it["id"]] = out.get(it["id"], 0) + (it.get("count") or 1)
    return out


def color_of(rank: int, yellow: int, grey: int) -> int | None:
    """0 orange, 1 yellow, 2 green, 3 grey, like TradeskillRelativeDifficulty in the game."""
    if not grey:
        return None
    if rank >= grey:
        return 3
    if rank >= (yellow + grey) // 2:
        return 2
    if rank >= yellow:
        return 1
    return 0


def fmt_cd(seconds: int | None) -> str | None:
    if not seconds:
        return None
    hours = seconds / 3600
    return f"{hours:.0f} h" if hours >= 1 else f"{seconds // 60} min"


# ---------- calculation ----------

def profit_history(market: Market, output: int, qty: float, reagents: list[tuple[int, int]]) -> list[dict]:
    """Profit per craft on every full scan day, calculated like the current profit. Days on which the
    result or a reagent has no price are left out."""
    out = []
    for day in market.scan_days:
        price = market.day_price(output, day)
        if not price:
            continue
        cost = 0.0
        for iid, count in reagents:
            options = [p for p in (market.day_price(iid, day), market.vendor_price(iid)[0]) if p]
            if not options:
                break
            cost += min(options) * count
        else:
            out.append({"date": (datetime.date(2020, 1, 1) + datetime.timedelta(day)).isoformat(),
                        "profit": round(price * qty * (1 - AH_CUT) - cost), "price": round(price),
                        "listed": market.day_listed(output, day)})
    return out


def trend_of(history: list[dict], cost: float = 0) -> str | None:
    """"falling", "rising" or "stable": change of the profit over the last three full scan days. Less than
    20% of the largest amount in that window counts as stable (at least 5% of the reagent cost or 1 silver,
    so copper amounts around zero are no trend). Without three scan days there is no statement."""
    if len(history) < 3:
        return None
    window = [h["profit"] for h in history[-3:]]
    change = window[-1] - window[0]
    scale = max(*(abs(p) for p in window), 0.05 * cost, 100)
    if abs(change) < 0.2 * scale:
        return "stable"
    return "falling" if change < 0 else "rising"


def evaluate(sid: int, rec: dict, scan: dict | None, rank: int, market: Market, owned: dict[int, int],
             names: dict[int, str]) -> dict:
    item = lambda i: market.items.get(str(i)) or {}  # noqa: E731
    name_of = lambda i: names.get(i) or item(i).get("name") or f"Item {i}"  # noqa: E731

    reagents = [(r["id"], r["count"]) for r in (scan or {}).get("reagents") or [] if not r.get("optional")] \
        or [tuple(r) for r in rec.get("reagents") or []]
    output = (scan or {}).get("output") or rec.get("output")
    qty = rec.get("qty") or 1
    if scan and scan.get("qtyMin"):
        qty = (scan["qtyMin"] + (scan.get("qtyMax") or scan["qtyMin"])) / 2

    rows, cost, missing_price, craftable = [], 0.0, [], None
    for iid, count in reagents:
        p = market.reagent(iid)
        line = None if p["unit"] is None else p["unit"] * count
        if line is None:
            missing_price.append(name_of(iid))
        else:
            cost += line
        have = owned.get(iid, 0)
        craftable = have // count if craftable is None else min(craftable, have // count)
        rows.append({"id": iid, "name": name_of(iid), "quality": item(iid).get("quality", 1), "count": count,
                     "unit": None if p["unit"] is None else round(p["unit"]), "cost": None if line is None else round(line),
                     "source": p["source"], "vendor": p["vendor"], "owned": have})

    out_item = item(output)
    ah = market.ah(output) if output else None
    bound = out_item.get("bind") == 1
    sell, sell_source = None, None
    if ah and not bound:
        sell, sell_source = ah["price"] * qty * (1 - AH_CUT), "ah"
    elif out_item.get("sell"):
        sell, sell_source = out_item["sell"] * qty, "vendor"
    profit = None if sell is None or missing_price else sell - cost
    history = profit_history(market, output, qty, reagents) if sell_source == "ah" else []
    trend = trend_of(history, cost)
    difficulty = (scan or {}).get("difficulty")
    if difficulty is None:
        difficulty = color_of(rank, rec.get("yellow", 0), rec.get("grey", 0))
    notes = []
    if bound:
        notes.append(tr("binds when picked up, cannot be sold"))
    if ah and ah["age"]:
        notes.append(tr("price is 1 day old") if ah["age"] == 1 else tr("price is {days} days old", days=ah["age"]))
    if trend == "falling":
        # A price below the weekly average is then no outlier but the end of a decline.
        was = history[-3]
        notes.append(tr("profit is falling (on {date} still {gold}g)", date=i18n.short_date(was["date"]),
                        gold=f"{was['profit'] / 10000:+.2f}"))
    elif ah and ah["price"] < 0.6 * ah["median"]:
        notes.append(tr("cheap right now (7-day average {gold}g)", gold=f"{ah['median'] / 10000:.2f}"))
    if missing_price:
        notes.append(tr("no price for {items}", items=", ".join(missing_price)))
    return {
        "id": sid, "name": (scan or {}).get("name") or rec.get("name") or tr("Recipe {id}", id=sid),
        "output": output, "outputName": name_of(output) if output else None, "quality": out_item.get("quality", 1),
        "qty": qty, "reagents": rows, "cost": round(cost) if not missing_price else None,
        "sell": None if sell is None else round(sell), "sellSource": sell_source,
        "unitPrice": ah["price"] if ah else None, "median": ah["median"] if ah else None,
        "listed": ah["listed"] if ah else 0,
        "profit": None if profit is None else round(profit),
        "margin": None if profit is None or not cost else round(profit / cost * 100),
        "craftable": craftable or 0, "difficulty": difficulty, "yellow": rec.get("yellow"), "grey": rec.get("grey"),
        "skillUps": (scan or {}).get("skillUps"), "cooldown": fmt_cd(rec.get("cooldown")),
        "isNew": bool(output and output > NEW_ID), "notes": notes, "history": history, "trend": trend,
    }


def acquisition(sid: int, rec: dict, scan: dict | None, rank: int, market: Market) -> dict:
    """Where a recipe that is not learned yet comes from and what it costs."""
    options = []
    req = None
    for iid in rec.get("recipeItems") or []:
        it = market.items.get(str(iid)) or {}
        req = it.get("reqSkill") or req
        ah = market.ah(iid)
        seen = market.vendor.get(iid) or market.vendor.get(str(iid))
        name = it.get("name") or tr("Recipe: {name}", name=rec.get("name"))
        if ah:
            options.append({"kind": "ah", "id": iid, "name": name, "price": ah["price"], "listed": ah["listed"],
                            "age": ah["age"], "quality": it.get("quality", 1)})
        if isinstance(seen, dict) and seen.get("price"):
            options.append({"kind": "vendor", "id": iid, "name": name, "price": round(seen["price"]),
                            "npc": seen.get("npc"), "limited": bool(seen.get("limited")), "quality": it.get("quality", 1)})
        if not ah and not seen:
            options.append({"kind": "item", "id": iid, "name": name, "price": None, "quality": it.get("quality", 1)})
    trainer = not rec.get("recipeItems") and rec.get("acquire") != 3
    if trainer:
        options.append({"kind": "trainer", "name": tr("Trainer"), "price": None})
    source_text = (scan or {}).get("source")
    priced = [o for o in options if o.get("price")]
    best = min(priced, key=lambda o: o["price"]) if priced else None
    if req:
        learnable = rank >= req
    elif trainer and rec.get("yellow"):
        learnable = True if rank >= rec["yellow"] else None  # the skill to learn it is never above "yellow"
    else:
        learnable = None
    return {"options": options, "best": best, "reqSkill": req, "learnable": learnable, "sourceText": source_text,
            "trainer": trainer}


def profession_rows(line: int, catalog: dict, scan: dict | None, rank: int, market: Market,
                    owned: dict[int, int]) -> tuple[list, list]:
    recipes = catalog["lines"].get(str(line), {})
    scanned = (scan or {}).get("recipes") or {}
    names = {int(k): n for k, n in ((scan or {}).get("names") or {}).items() if n}
    ids = {int(k) for k in recipes} | {int(k) for k in scanned}
    learned, missing = [], []
    for sid in sorted(ids):
        rec = recipes.get(str(sid)) or {}
        s = scanned.get(sid) or scanned.get(str(sid))
        if not rec and not (s and s.get("output")):
            continue
        is_learned = bool(s and s.get("learned")) if scan else None
        if not scan and rec.get("acquire") == 3 and not rec.get("recipeItems"):
            continue
        row = evaluate(sid, rec, s, rank, market, owned, names)
        row["learned"] = is_learned
        if is_learned:
            learned.append(row)
        else:
            if rec.get("acquire") == 3 and not rec.get("recipeItems") and not (s and s.get("source")):
                continue  # no longer available
            row["acquire"] = acquisition(sid, rec, s, rank, market)
            missing.append(row)
    return learned, missing


def sort_key(row: dict) -> tuple:
    return (row["profit"] is None, -(row["profit"] or 0))


def compute(wow: str = writs.DEFAULT_WOW, char: str = "", profession: str = "", top: int = 0,
            prices: dict | None = None) -> dict:
    files = writs.find_chars(wow, char)
    if not files:
        raise FileNotFoundError(tr("No WoWMCP export found for \"{char}\".", char=char))
    c = upgrades.load_char(files[0])
    prices = prices if prices is not None else writs.load_prices(writs.find_auctionator(wow))
    vendor: dict = {}
    for path in glob.glob(os.path.join(wow, "WTF", "Account", "*", "SavedVariables", "WoWMCP.lua")):
        vendor.update((upgrades.load_sv(path, "WoWMCP_Account") or {}).get("vendor") or {})
    build = None
    cl = c.get("client") or {}
    if cl.get("version") and cl.get("build"):
        build = f"{cl['version']}.{int(cl['build'])}"
    # Names in the catalog are in the client's language, like everything else that comes from the game.
    locale = cl.get("locale") or i18n.client_locale(wow) or i18n.DEFAULT_LOCALE
    catalog = load_catalog(build or icons.client_build(wow), locale)
    market = Market(prices, vendor, catalog["items"])
    owned = owned_counts(c)
    scans = {int(k): s for k, s in (c.get("recipes") or {}).items()}

    out = []
    for p in (c.get("skills") or {}).get("professions") or []:
        line = p.get("skillLine")
        if not line or line in GATHERING or str(line) not in catalog["lines"]:
            continue
        if profession and profession != str(line) and profession.lower() not in (p.get("name") or "").lower():
            continue
        scan = scans.get(line)
        rank = (scan or {}).get("rank") or p.get("rank") or 0
        learned, missing = profession_rows(line, catalog, scan, rank, market, owned)
        learned.sort(key=sort_key)
        good = [r["profit"] for r in learned if r["profit"] is not None and r["listed"] >= 3]
        best_learned = max(good) if good else 0
        for r in missing:
            r["better"] = bool(scan) and r["profit"] is not None and r["profit"] > best_learned
            cost = (r["acquire"]["best"] or {}).get("price")
            r["payback"] = (round(cost / r["profit"], 1) if cost and r["profit"] and r["profit"] > 0 else None)
        # First what you can learn now, then by profit.
        missing.sort(key=lambda r: (r["acquire"]["learnable"] is not True, *sort_key(r)))
        if top:
            learned_rows, missing_rows = learned[:top], missing[:top]
        else:
            learned_rows, missing_rows = learned, missing
        out.append({
            "skillLine": line, "name": p.get("name"), "rank": rank, "max": (scan or {}).get("max") or p.get("max"),
            "scanned": (scan or {}).get("updated"), "learnedCount": len(learned), "missingCount": len(missing),
            "bestLearned": best_learned, "learned": learned_rows, "missing": missing_rows,
            "profitableCount": sum(1 for r in learned if (r["profit"] or 0) > 0),
        })
    sd = writs.scan_date(prices)
    return {"character": os.path.basename(os.path.dirname(os.path.dirname(files[0]))),
            "level": (c.get("character") or {}).get("level"), "scanDate": sd.isoformat() if sd else None,
            "build": catalog.get("build"), "locale": catalog.get("locale") or locale, "vendorPrices": len(vendor), "addonVersion": (c.get("addon") or {}).get("version"),
            "professions": out, "ahCut": AH_CUT}


# ---------- CLI ----------

def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--wow", default=writs.DEFAULT_WOW)
    ap.add_argument("--char", default="")
    ap.add_argument("--profession", default="",
                    help="part of the profession name as the client shows it (Alchemy, Alchimie) or its skill line ID")
    ap.add_argument("--top", type=int, default=15)
    ap.add_argument("--json", action="store_true")
    i18n.add_argument(ap)
    a = ap.parse_args()
    i18n.set_language(a.lang, a.wow)
    try:
        r = compute(a.wow, a.char, a.profession, a.top)
    except (FileNotFoundError, LookupError) as e:
        sys.exit(str(e))
    sys.stdout.reconfigure(encoding="utf-8")
    if a.json:
        print(json.dumps(r, ensure_ascii=False))
        return
    g = lambda c: "–" if c is None else writs.fmt(c)  # noqa: E731
    print(tr("{char}  |  AH scan {date}  |  build {build}  |  vendor prices {vendors}", char=r["character"],
             date=r["scanDate"], build=r["build"], vendors=r["vendorPrices"]))
    kinds = {"ah": tr("AH"), "vendor": tr("vendor"), "item": tr("recipe item"), "trainer": tr("trainer")}
    learnable = {True: tr("yes"), False: tr("no"), None: "?"}
    for p in r["professions"]:
        state = (tr("profession window read {when}", when=f"{datetime.datetime.fromtimestamp(p['scanned']):%Y-%m-%d %H:%M}")
                 if p["scanned"] else tr("learned recipes unknown (open the profession window)"))
        print(f"\n== {p['name']} {p['rank']}/{p['max']} ({state}) ==")
        if p["scanned"]:
            print("  " + tr("Learned:"))
            for x in p["learned"]:
                print(f"   {x['name'][:40]:<40} "
                      + tr("profit {profit}  cost {cost}  AH {listed}x", profit=f"{g(x['profit']):>10}",
                           cost=f"{g(x['cost']):>9}", listed=f"{x['listed']:>5}")
                      + f"  {'; '.join(x['notes'])}")
        print("  " + (tr("Not learned yet:") if p["scanned"] else tr("All recipes:")))
        for x in p["missing"]:
            acq = x["acquire"]
            best = acq["best"]
            src = (f"{kinds[best['kind']]} {g(best['price'])}" if best else
                   ", ".join(kinds[o["kind"]] for o in acq["options"]) or "?")
            flag = "*" if x.get("better") else " "
            print(f"  {flag}{x['name'][:40]:<40} "
                  + tr("profit {profit}  learnable {learnable} from {skill} {source}", profit=f"{g(x['profit']):>10}",
                       learnable=f"{learnable[acq['learnable']]:<5}", skill=f"{acq['reqSkill'] or '?':<4}", source=src))


if __name__ == "__main__":
    main()
